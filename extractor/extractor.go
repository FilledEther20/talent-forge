package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"talent-forge/types"
)

type Extractor interface {
	Name() string
	FetchAndNormalize(ctx context.Context) (*types.NormalizedProfile, error)
}

type retryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

var defaultRetry = retryConfig{
	MaxAttempts: 3,
	BaseDelay:   100 * time.Millisecond,
	MaxDelay:    2 * time.Second,
}

func withRetry(ctx context.Context, cfg retryConfig, name string, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		slog.Warn("extractor attempt failed",
			"source", name,
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"error", lastErr,
		)

		if attempt == cfg.MaxAttempts {
			break
		}

		delay := time.Duration(float64(cfg.BaseDelay) * pow2(attempt-1))
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		delay = time.Duration(rand.Int63n(int64(delay) + 1))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("%s: exhausted %d attempts: %w", name, cfg.MaxAttempts, lastErr)
}

func pow2(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 2
	}
	return result
}

var (
	multiSpace = regexp.MustCompile(`\s+`)
	emailRe    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	phoneRe    = regexp.MustCompile(`\+?\d[\d\s\-().]{7,}\d`)
	urlRe      = regexp.MustCompile(`https?://[^\s,]+`)
)

func cleanString(s string) string {
	return multiSpace.ReplaceAllString(strings.TrimSpace(s), " ")
}

func normalizePhoneE164(raw string) string {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(raw, "")
	if digits == "" {
		return ""
	}
	if len(digits) == 10 {
		return "+1" + digits
	}
	if strings.HasPrefix(raw, "+") {
		return "+" + digits
	}
	return "+" + digits
}

func normalizeEmails(raw []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		e = strings.ToLower(cleanString(e))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

func normalizePhones(raw []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = normalizePhoneE164(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func dedupeSkills(skills []string) []string {
	seen := make(map[string]struct{}, len(skills))
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		s = cleanString(s)
		key := strings.ToLower(s)
		if s == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

func toISOCountry(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "us", "usa", "u.s.", "u.s.a.", "united states", "united states of america":
		return "US"
	case "uk", "u.k.", "united kingdom":
		return "GB"
	case "ca", "canada":
		return "CA"
	case "de", "germany":
		return "DE"
	case "in", "india":
		return "IN"
	}
	if len(s) == 2 {
		return strings.ToUpper(s)
	}
	return strings.TrimSpace(s)
}

func parseLocationString(s string) *types.Location {
	parts := strings.Split(s, ",")
	loc := &types.Location{}
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	switch len(parts) {
	case 1:
		loc.City = parts[0]
	case 2:
		loc.City = parts[0]
		loc.Region = parts[1]
	default:
		loc.City = parts[0]
		loc.Region = parts[1]
		loc.Country = toISOCountry(parts[2])
	}
	if loc.City == "" && loc.Region == "" && loc.Country == "" {
		return nil
	}
	return loc
}

type ATSStub struct {
	Name   string
	Email  string
	Phone  string
	Skills []string
}

type atsRawRecord struct {
	CandidateID     string                  `json:"candidate_id"`
	Name            string                  `json:"name"`
	EmailAddress    string                  `json:"email_address"`
	PhoneNumber     string                  `json:"phone_number"`
	CurrentCompany  string                  `json:"current_company"`
	Title           string                  `json:"title"`
	YearsExperience *float64                `json:"years_experience"`
	Location        *atsLocationRaw         `json:"location"`
	SkillTags       []string                `json:"skill_tags"`
	Experience      []types.ExperienceEntry `json:"experience"`
	Education       []types.EducationEntry  `json:"education"`
	UpdatedAt       string                  `json:"updated_at"`
}

type atsLocationRaw struct {
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

type ATSExtractor struct {
	CandidateID string
	FailureRate float64
	FilePath    string
	Stub        *ATSStub
}

func (e *ATSExtractor) Name() string { return string(types.SourceATS) }

func (e *ATSExtractor) FetchAndNormalize(ctx context.Context) (*types.NormalizedProfile, error) {
	var raw atsRawRecord

	err := withRetry(ctx, defaultRetry, e.Name(), func() error {
		r, err := e.fetchRaw(ctx)
		if err != nil {
			return err
		}
		raw = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	profile := &types.NormalizedProfile{
		Source:          types.SourceATS,
		CandidateID:     raw.CandidateID,
		FullName:        cleanString(raw.Name),
		Emails:          normalizeEmails([]string{raw.EmailAddress}),
		Phones:          normalizePhones([]string{raw.PhoneNumber}),
		Skills:          dedupeSkills(raw.SkillTags),
		Experience:      raw.Experience,
		Education:       raw.Education,
		YearsExperience: raw.YearsExperience,
	}
	if raw.CurrentCompany != "" || raw.Title != "" {
		headline := strings.TrimSpace(strings.Join([]string{raw.Title, raw.CurrentCompany}, " @ "))
		headline = strings.TrimPrefix(headline, " @ ")
		headline = strings.TrimSuffix(headline, " @ ")
		profile.Headline = headline
	}
	if raw.Location != nil {
		profile.Location = &types.Location{
			City:    raw.Location.City,
			Region:  raw.Location.State,
			Country: toISOCountry(raw.Location.Country),
		}
	}
	if raw.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
			profile.LastUpdated = t
		}
	}

	slog.Info("ats extractor normalized profile", "candidate_id", profile.CandidateID, "emails", profile.Emails)
	return profile, nil
}

func (e *ATSExtractor) fetchRaw(ctx context.Context) (atsRawRecord, error) {
	if rand.Float64() < e.FailureRate {
		return atsRawRecord{}, errors.New("ats: simulated upstream timeout")
	}
	if e.FilePath != "" {
		return loadATSFile(e.FilePath)
	}
	if e.Stub != nil {
		raw := defaultATSRaw()
		raw.Name = e.Stub.Name
		raw.EmailAddress = e.Stub.Email
		raw.PhoneNumber = e.Stub.Phone
		raw.SkillTags = e.Stub.Skills
		return raw, nil
	}
	return defaultATSRaw(), nil
}

func loadATSFile(path string) (atsRawRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return atsRawRecord{}, fmt.Errorf("ats: read %s: %w", path, err)
	}
	var raw atsRawRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return atsRawRecord{}, fmt.Errorf("ats: parse %s: %w", path, err)
	}
	return raw, nil
}

func defaultATSRaw() atsRawRecord {
	yrs := 8.0
	return atsRawRecord{
		CandidateID:     "cand-123",
		Name:            "  Jane   Doe ",
		EmailAddress:    " JANE.DOE@badformat ",
		PhoneNumber:     "555-123-4567",
		CurrentCompany:  "Acme Corp",
		Title:           "Senior Software Engineer",
		YearsExperience: &yrs,
		Location:        &atsLocationRaw{City: "San Francisco", State: "CA", Country: "United States"},
		SkillTags:       []string{"Go", "  go ", "SQL", "Kubernetes", "PostgreSQL"},
		Experience: []types.ExperienceEntry{
			{Company: "Acme Corp", Title: "Senior Software Engineer", Start: "2022-03", Summary: "Distributed data pipelines on Kubernetes."},
			{Company: "Initech", Title: "Software Engineer", Start: "2018-06", End: "2022-02", Summary: "Backend services in Go."},
		},
		Education: []types.EducationEntry{
			{Institution: "UC Berkeley", Degree: "B.S.", Field: "Computer Science", EndYear: 2018},
		},
		UpdatedAt: time.Now().Add(-72 * time.Hour).Format(time.RFC3339),
	}
}

type GitHubStub struct {
	Name   string
	Email  string
	Skills []string
}

type githubRawRecord struct {
	Username     string    `json:"username"`
	Name         string    `json:"name"`
	PublicEmail  string    `json:"public_email"`
	Bio          string    `json:"bio"`
	Location     string    `json:"location"`
	Blog         string    `json:"blog"`
	TopLanguages []string  `json:"top_languages"`
	UpdatedAtRaw string    `json:"updated_at"`
	UpdatedAt    time.Time `json:"-"`
}

type GitHubExtractor struct {
	Username    string
	FailureRate float64
	FilePath    string
	Stub        *GitHubStub
}

func (e *GitHubExtractor) Name() string { return string(types.SourceGitHub) }

func (e *GitHubExtractor) FetchAndNormalize(ctx context.Context) (*types.NormalizedProfile, error) {
	var raw githubRawRecord

	err := withRetry(ctx, defaultRetry, e.Name(), func() error {
		r, err := e.fetchRaw(ctx)
		if err != nil {
			return err
		}
		raw = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	profile := &types.NormalizedProfile{
		Source:      types.SourceGitHub,
		FullName:    cleanString(raw.Name),
		Emails:      normalizeEmails([]string{raw.PublicEmail}),
		Skills:      dedupeSkills(raw.TopLanguages),
		Headline:    cleanString(raw.Bio),
		LastUpdated: raw.UpdatedAt,
	}
	if raw.Location != "" {
		profile.Location = parseLocationString(raw.Location)
	}
	links := &types.Links{
		GitHub:    "https://github.com/" + raw.Username,
		Portfolio: raw.Blog,
	}
	if links.GitHub != "" || links.Portfolio != "" {
		profile.Links = links
	}

	slog.Info("github extractor normalized profile", "username", raw.Username, "skills", profile.Skills)
	return profile, nil
}

func (e *GitHubExtractor) fetchRaw(ctx context.Context) (githubRawRecord, error) {
	if rand.Float64() < e.FailureRate {
		return githubRawRecord{}, errors.New("github: simulated rate limit error")
	}
	if e.FilePath != "" {
		return loadGitHubFile(e.FilePath)
	}
	if e.Stub != nil {
		raw := defaultGitHubRaw()
		raw.Name = e.Stub.Name
		raw.PublicEmail = e.Stub.Email
		raw.TopLanguages = e.Stub.Skills
		return raw, nil
	}
	return defaultGitHubRaw(), nil
}

func loadGitHubFile(path string) (githubRawRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return githubRawRecord{}, fmt.Errorf("github: read %s: %w", path, err)
	}
	var raw githubRawRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return githubRawRecord{}, fmt.Errorf("github: parse %s: %w", path, err)
	}
	if raw.UpdatedAtRaw != "" {
		if t, err := time.Parse(time.RFC3339, raw.UpdatedAtRaw); err == nil {
			raw.UpdatedAt = t
		}
	}
	return raw, nil
}

func defaultGitHubRaw() githubRawRecord {
	return githubRawRecord{
		Username:     "janedoe",
		Name:         "Jane Doe",
		PublicEmail:  "jane.doe@example.com",
		Bio:          "Distributed systems engineer. Go, Python, cloud infrastructure.",
		Location:     "San Francisco, CA, US",
		Blog:         "https://janedoe.dev",
		TopLanguages: []string{"Go", "Python", "TypeScript"},
		UpdatedAt:    time.Now().Add(-2 * time.Hour),
	}
}

var skillKeywords = []string{
	"Go", "Golang", "Python", "Java", "JavaScript", "TypeScript",
	"Rust", "C++", "Ruby", "Kotlin", "Swift",
	"Kubernetes", "Docker", "Terraform", "AWS", "GCP", "Azure",
	"PostgreSQL", "MySQL", "MongoDB", "Redis", "Kafka",
	"React", "Vue", "Angular", "Node.js",
	"SQL", "NoSQL", "GraphQL", "REST",
	"distributed systems", "microservices", "machine learning", "data engineering",
}

type NotesExtractor struct {
	FailureRate float64
	FilePath    string
	RawText     string
}

func (e *NotesExtractor) Name() string { return string(types.SourceNotes) }

func (e *NotesExtractor) FetchAndNormalize(ctx context.Context) (*types.NormalizedProfile, error) {
	var text string

	err := withRetry(ctx, defaultRetry, e.Name(), func() error {
		t, err := e.fetchRaw(ctx)
		if err != nil {
			return err
		}
		text = t
		return nil
	})
	if err != nil {
		return nil, err
	}

	profile := parseNotes(text)
	profile.Source = types.SourceNotes
	profile.LastUpdated = time.Now().Add(-24 * time.Hour)

	slog.Info("notes extractor normalized profile", "emails", profile.Emails, "phones", profile.Phones)
	return profile, nil
}

func (e *NotesExtractor) fetchRaw(ctx context.Context) (string, error) {
	if rand.Float64() < e.FailureRate {
		return "", errors.New("notes: simulated file-read failure")
	}
	if e.FilePath != "" {
		data, err := os.ReadFile(e.FilePath)
		if err != nil {
			return "", fmt.Errorf("notes: read %s: %w", e.FilePath, err)
		}
		return string(data), nil
	}
	if e.RawText != "" {
		return e.RawText, nil
	}
	return defaultNotesText(), nil
}

func defaultNotesText() string {
	return `Recruiter call notes — spoke with Jane Doe today.
Reach her at jane@janedoe.dev or +1 415-555-9988.
Currently at Acme Corp as a Senior Engineer.
Background: Go, Python, Kubernetes, PostgreSQL, distributed systems.
LinkedIn: https://linkedin.com/in/janedoe`
}

func parseNotes(text string) *types.NormalizedProfile {
	emails := normalizeEmails(emailRe.FindAllString(text, -1))
	phones := normalizePhones(phoneRe.FindAllString(text, -1))

	var skills []string
	lower := strings.ToLower(text)
	for _, kw := range skillKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			skills = append(skills, kw)
		}
	}
	skills = dedupeSkills(skills)

	name := extractNotesName(text)

	links := &types.Links{}
	for _, u := range urlRe.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,)")
		switch {
		case strings.Contains(u, "linkedin.com"):
			links.LinkedIn = u
		case strings.Contains(u, "github.com"):
			links.GitHub = u
		default:
			links.Other = append(links.Other, u)
		}
	}
	var linksOut *types.Links
	if links.LinkedIn != "" || links.GitHub != "" || links.Portfolio != "" || len(links.Other) > 0 {
		linksOut = links
	}

	return &types.NormalizedProfile{
		FullName: name,
		Emails:   emails,
		Phones:   phones,
		Skills:   skills,
		Links:    linksOut,
	}
}

var nameAfterPhrase = regexp.MustCompile(`(?i)(?:spoke with|met with|interviewed|talked to|candidate[: ]+)\s+([A-Z][A-Za-z'\-]+(?:\s+[A-Z][A-Za-z'\-]+){0,3})`)

func extractNotesName(text string) string {
	if m := nameAfterPhrase.FindStringSubmatch(text); len(m) > 1 {
		return cleanString(m[1])
	}
	return ""
}

func FetchAll(ctx context.Context, extractors []Extractor) []*types.NormalizedProfile {
	results := make([]*types.NormalizedProfile, len(extractors))

	var wg sync.WaitGroup
	for i, ex := range extractors {
		i, ex := i, ex
		wg.Add(1)
		go func() {
			defer wg.Done()
			profile, err := ex.FetchAndNormalize(ctx)
			if err != nil {
				slog.Warn("source failed after retries, degrading gracefully",
					"source", ex.Name(),
					"error", err,
				)
				return
			}
			results[i] = profile
		}()
	}
	wg.Wait()

	return results
}
