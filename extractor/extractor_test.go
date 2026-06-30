package extractor

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"talent-forge/types"
)

type fakeExtractor struct {
	name    string
	profile *types.NormalizedProfile
	err     error
}

func (f fakeExtractor) Name() string { return f.name }

func (f fakeExtractor) FetchAndNormalize(ctx context.Context) (*types.NormalizedProfile, error) {
	return f.profile, f.err
}

func TestWithRetrySucceedsFirstTry(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), defaultRetry, "test", func() error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestWithRetrySucceedsAfterFailures(t *testing.T) {
	cfg := retryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
	calls := 0
	err := withRetry(context.Background(), cfg, "test", func() error {
		calls++
		if calls < 3 {
			return errors.New("boom")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success on third try, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestWithRetryExhaustsAttempts(t *testing.T) {
	cfg := retryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
	calls := 0
	sentinel := errors.New("upstream down")
	err := withRetry(context.Background(), cfg, "test", func() error {
		calls++
		return sentinel
	})

	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestWithRetryRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := withRetry(ctx, defaultRetry, "test", func() error {
		calls++
		return errors.New("boom")
	})

	if err == nil {
		t.Fatal("expected context error")
	}
	if calls != 0 {
		t.Errorf("expected 0 calls when context already cancelled, got %d", calls)
	}
}

func TestPow2(t *testing.T) {
	tests := []struct {
		in   int
		want float64
	}{
		{0, 1},
		{1, 2},
		{2, 4},
		{3, 8},
	}

	for _, tt := range tests {
		if got := pow2(tt.in); got != tt.want {
			t.Errorf("pow2(%d) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"  Jane   Doe ", "Jane Doe"},
		{"\tGo\nLang", "Go Lang"},
		{"", ""},
		{"single", "single"},
	}

	for _, tt := range tests {
		if got := cleanString(tt.in); got != tt.want {
			t.Errorf("cleanString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizePhoneE164(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"555-123-4567", "+15551234567"},
		{"+44 20 7946 0958", "+442079460958"},
		{"", ""},
		{"abc", ""},
		{"(555) 123 4567", "+15551234567"},
	}

	for _, tt := range tests {
		if got := normalizePhoneE164(tt.in); got != tt.want {
			t.Errorf("normalizePhoneE164(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeEmails(t *testing.T) {
	got := normalizeEmails([]string{" Jane@Example.COM ", "jane@example.com", "", "bob@example.com"})
	want := []string{"jane@example.com", "bob@example.com"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeEmails() = %v, want %v", got, want)
	}
}

func TestNormalizePhones(t *testing.T) {
	got := normalizePhones([]string{"555-123-4567", "(555) 123 4567", "+44 20 7946 0958", "abc"})
	want := []string{"+15551234567", "+442079460958"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizePhones() = %v, want %v", got, want)
	}
}

func TestDedupeSkills(t *testing.T) {
	got := dedupeSkills([]string{"Go", "  go ", "SQL", "", "go", "Python"})
	want := []string{"Go", "SQL", "Python"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeSkills() = %v, want %v", got, want)
	}
}

func TestToISOCountry(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"United States", "US"},
		{"india", "IN"},
		{"gb", "GB"},
		{"France", "France"},
	}

	for _, tt := range tests {
		if got := toISOCountry(tt.in); got != tt.want {
			t.Errorf("toISOCountry(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseLocationString(t *testing.T) {
	got := parseLocationString("San Francisco, CA, United States")
	if got == nil {
		t.Fatal("expected location")
	}
	if got.City != "San Francisco" || got.Region != "CA" || got.Country != "US" {
		t.Fatalf("unexpected location: %+v", got)
	}

	if empty := parseLocationString(""); empty != nil {
		t.Fatalf("expected empty one-part location, got %+v", empty)
	}
}

func TestATSExtractorNormalizesDefaultRecord(t *testing.T) {
	e := &ATSExtractor{CandidateID: "cand-1", FailureRate: 0}
	profile, err := e.FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.Source != types.SourceATS {
		t.Errorf("expected source ats, got %v", profile.Source)
	}
	if profile.CandidateID != "cand-123" {
		t.Errorf("expected candidate id cand-123, got %q", profile.CandidateID)
	}
	if profile.FullName != "Jane Doe" {
		t.Errorf("expected cleaned name Jane Doe, got %q", profile.FullName)
	}
	if !reflect.DeepEqual(profile.Emails, []string{"jane.doe@badformat"}) {
		t.Errorf("unexpected emails: %v", profile.Emails)
	}
	if !reflect.DeepEqual(profile.Phones, []string{"+15551234567"}) {
		t.Errorf("unexpected phones: %v", profile.Phones)
	}
	if profile.Location == nil || profile.Location.Country != "US" {
		t.Errorf("expected normalized US location, got %+v", profile.Location)
	}
	if profile.Headline != "Senior Software Engineer @ Acme Corp" {
		t.Errorf("unexpected headline: %q", profile.Headline)
	}
	if profile.YearsExperience == nil || *profile.YearsExperience != 8 {
		t.Errorf("unexpected years experience: %v", profile.YearsExperience)
	}
	if len(profile.Experience) != 2 {
		t.Errorf("expected 2 experience entries, got %d", len(profile.Experience))
	}
	if len(profile.Education) != 1 {
		t.Errorf("expected 1 education entry, got %d", len(profile.Education))
	}
}

func TestATSExtractorUsesStubValues(t *testing.T) {
	e := &ATSExtractor{
		FailureRate: 0,
		Stub: &ATSStub{
			Name:   "  Alex   Kim ",
			Email:  " ALEX@Example.COM ",
			Phone:  "(212) 555-7777",
			Skills: []string{"Go", " go ", "Rust"},
		},
	}

	profile, err := e.FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.FullName != "Alex Kim" {
		t.Errorf("expected stub name, got %q", profile.FullName)
	}
	if !reflect.DeepEqual(profile.Emails, []string{"alex@example.com"}) {
		t.Errorf("unexpected stub emails: %v", profile.Emails)
	}
	if !reflect.DeepEqual(profile.Phones, []string{"+12125557777"}) {
		t.Errorf("unexpected stub phones: %v", profile.Phones)
	}
	if !reflect.DeepEqual(profile.Skills, []string{"Go", "Rust"}) {
		t.Errorf("unexpected stub skills: %v", profile.Skills)
	}
}

func TestATSExtractorLoadsFile(t *testing.T) {
	path := writeTempFile(t, `{
		"candidate_id":"cand-file",
		"name":"  Priya   Shah ",
		"email_address":" PRIYA@EXAMPLE.COM ",
		"phone_number":"415 555 0000",
		"current_company":"Eightfold",
		"title":"Backend Engineer",
		"years_experience":5,
		"location":{"city":"Bengaluru","state":"KA","country":"India"},
		"skill_tags":["Go","SQL","go"],
		"experience":[{"company":"Eightfold","title":"Backend Engineer","start":"2021-01"}],
		"education":[{"institution":"IIT","degree":"B.Tech","field":"CS","end_year":2020}],
		"updated_at":"2026-06-30T10:00:00Z"
	}`)

	profile, err := (&ATSExtractor{FilePath: path}).FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.CandidateID != "cand-file" || profile.FullName != "Priya Shah" {
		t.Fatalf("unexpected loaded profile: %+v", profile)
	}
	if !reflect.DeepEqual(profile.Emails, []string{"priya@example.com"}) {
		t.Errorf("unexpected emails: %v", profile.Emails)
	}
	if !reflect.DeepEqual(profile.Phones, []string{"+14155550000"}) {
		t.Errorf("unexpected phones: %v", profile.Phones)
	}
	if profile.Location == nil || profile.Location.Country != "IN" {
		t.Errorf("expected IN location, got %+v", profile.Location)
	}
	if profile.LastUpdated.IsZero() {
		t.Error("expected parsed last updated timestamp")
	}
}

func TestGitHubExtractorNormalizesDefaultRecord(t *testing.T) {
	e := &GitHubExtractor{Username: "janedoe", FailureRate: 0}
	profile, err := e.FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.Source != types.SourceGitHub {
		t.Errorf("expected source github, got %v", profile.Source)
	}
	if profile.FullName != "Jane Doe" {
		t.Errorf("expected cleaned name, got %q", profile.FullName)
	}
	if !reflect.DeepEqual(profile.Emails, []string{"jane.doe@example.com"}) {
		t.Errorf("unexpected emails: %v", profile.Emails)
	}
	if len(profile.Phones) != 0 {
		t.Errorf("expected no phones from github, got %v", profile.Phones)
	}
	if !reflect.DeepEqual(profile.Skills, []string{"Go", "Python", "TypeScript"}) {
		t.Errorf("unexpected skills: %v", profile.Skills)
	}
	if profile.Location == nil || profile.Location.Country != "US" {
		t.Errorf("expected parsed github location, got %+v", profile.Location)
	}
	if profile.Links == nil || profile.Links.GitHub != "https://github.com/janedoe" || profile.Links.Portfolio != "https://janedoe.dev" {
		t.Errorf("unexpected links: %+v", profile.Links)
	}
}

func TestGitHubExtractorLoadsFile(t *testing.T) {
	path := writeTempFile(t, `{
		"username":"octocat",
		"name":" Octo  Cat ",
		"public_email":" OCTO@example.com ",
		"bio":"Open source maintainer",
		"location":"Berlin, BE, Germany",
		"blog":"https://octo.example",
		"top_languages":["Go","TypeScript","go"],
		"updated_at":"2026-06-30T11:00:00Z"
	}`)

	profile, err := (&GitHubExtractor{FilePath: path}).FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.FullName != "Octo Cat" {
		t.Errorf("unexpected full name: %q", profile.FullName)
	}
	if profile.Location == nil || profile.Location.Country != "DE" {
		t.Errorf("expected DE location, got %+v", profile.Location)
	}
	if profile.Links == nil || profile.Links.GitHub != "https://github.com/octocat" {
		t.Errorf("unexpected github link: %+v", profile.Links)
	}
	if profile.LastUpdated.IsZero() {
		t.Error("expected parsed last updated timestamp")
	}
}

func TestNotesExtractorParsesText(t *testing.T) {
	text := `Recruiter notes: spoke with Sam Lee.
Reach at sam@example.com and +1 415-555-9988.
Strong in Go, Kubernetes, PostgreSQL, distributed systems.
LinkedIn: https://linkedin.com/in/samlee
Portfolio: https://sam.example.`

	profile, err := (&NotesExtractor{RawText: text}).FetchAndNormalize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if profile.Source != types.SourceNotes {
		t.Errorf("expected source notes, got %v", profile.Source)
	}
	if profile.FullName != "Sam Lee" {
		t.Errorf("expected extracted name Sam Lee, got %q", profile.FullName)
	}
	if !reflect.DeepEqual(profile.Emails, []string{"sam@example.com"}) {
		t.Errorf("unexpected emails: %v", profile.Emails)
	}
	if !reflect.DeepEqual(profile.Phones, []string{"+14155559988"}) {
		t.Errorf("unexpected phones: %v", profile.Phones)
	}
	if !contains(profile.Skills, "Go") || !contains(profile.Skills, "Kubernetes") || !contains(profile.Skills, "PostgreSQL") {
		t.Errorf("expected parsed skills, got %v", profile.Skills)
	}
	if profile.Links == nil || profile.Links.LinkedIn != "https://linkedin.com/in/samlee" || len(profile.Links.Other) != 1 {
		t.Errorf("unexpected links: %+v", profile.Links)
	}
}

func TestFetchAllAllSucceed(t *testing.T) {
	extractors := []Extractor{
		fakeExtractor{name: "ats", profile: &types.NormalizedProfile{Source: types.SourceATS}},
		fakeExtractor{name: "github", profile: &types.NormalizedProfile{Source: types.SourceGitHub}},
		fakeExtractor{name: "notes", profile: &types.NormalizedProfile{Source: types.SourceNotes}},
	}

	results := FetchAll(context.Background(), extractors)

	if len(results) != 3 {
		t.Fatalf("expected 3 result slots, got %d", len(results))
	}
	if results[0] == nil || results[0].Source != types.SourceATS {
		t.Errorf("expected ats at index 0, got %v", results[0])
	}
	if results[1] == nil || results[1].Source != types.SourceGitHub {
		t.Errorf("expected github at index 1, got %v", results[1])
	}
	if results[2] == nil || results[2].Source != types.SourceNotes {
		t.Errorf("expected notes at index 2, got %v", results[2])
	}
}

func TestFetchAllDegradesGracefully(t *testing.T) {
	extractors := []Extractor{
		fakeExtractor{name: "ats", err: errors.New("down")},
		fakeExtractor{name: "github", profile: &types.NormalizedProfile{Source: types.SourceGitHub}},
	}

	results := FetchAll(context.Background(), extractors)

	if results[0] != nil {
		t.Errorf("expected nil for degraded ats source, got %v", results[0])
	}
	if results[1] == nil || results[1].Source != types.SourceGitHub {
		t.Errorf("expected github source to survive degradation, got %v", results[1])
	}
}

func TestFetchAllAllDegraded(t *testing.T) {
	extractors := []Extractor{
		fakeExtractor{name: "ats", err: errors.New("down")},
		fakeExtractor{name: "github", err: errors.New("rate limit")},
	}

	results := FetchAll(context.Background(), extractors)

	for i, r := range results {
		if r != nil {
			t.Errorf("expected nil at index %d when all sources fail, got %v", i, r)
		}
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "extractor-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
