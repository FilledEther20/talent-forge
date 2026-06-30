package merger

import (
	"testing"
	"time"

	"talent-forge/types"
)

func TestMergeEmailUnionSkipsMalformedValues(t *testing.T) {
	logger := NewCorrelationLogger("test-email")
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, Emails: []string{"jane.doe@badformat"}},
		{Source: types.SourceGitHub, Emails: []string{"jane.doe@example.com"}},
	}

	got := Merge(logger, profiles)

	if len(got.Emails.Value) != 1 || got.Emails.Value[0] != "jane.doe@example.com" {
		t.Fatalf("expected only the valid github email, got %v", got.Emails.Value)
	}
	if got.Emails.Source != types.SourceGitHub {
		t.Errorf("expected github source, got %v", got.Emails.Source)
	}
	if got.Emails.Confidence != sourceWeights["emails"][types.SourceGitHub] {
		t.Errorf("expected github email confidence %v, got %v", sourceWeights["emails"][types.SourceGitHub], got.Emails.Confidence)
	}
	if got.FieldConfidence["emails.value"] != got.Emails.Confidence {
		t.Errorf("expected emails.value field confidence to be set, got %v", got.FieldConfidence["emails.value"])
	}
	if !hasProvenance(got.Provenance, "emails", types.SourceGitHub, "union") {
		t.Errorf("expected github email provenance, got %v", got.Provenance)
	}
}

func TestMergeAllMalformedEmailsReturnsEmptySet(t *testing.T) {
	logger := NewCorrelationLogger("test-bad-email")
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, Emails: []string{"not-an-email"}},
		{Source: types.SourceGitHub, Emails: []string{"also-bad"}},
	}

	got := Merge(logger, profiles)

	if len(got.Emails.Value) != 0 {
		t.Fatalf("expected no valid emails, got %v", got.Emails.Value)
	}
	if got.Emails.Confidence != 0 {
		t.Errorf("expected zero confidence, got %v", got.Emails.Confidence)
	}
	if _, ok := got.FieldConfidence["emails.value"]; ok {
		t.Errorf("did not expect emails.value field confidence when no email survived")
	}
}

func TestMergeFullNameUsesSourceWeight(t *testing.T) {
	logger := NewCorrelationLogger("test-full-name")
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, FullName: "Jane Doe"},
		{Source: types.SourceGitHub, FullName: "Jane D"},
	}

	got := Merge(logger, profiles)

	if got.FullName.Value != "Jane Doe" {
		t.Errorf("expected ATS full name, got %q", got.FullName.Value)
	}
	if got.FullName.Source != types.SourceATS {
		t.Errorf("expected ATS source, got %v", got.FullName.Source)
	}
	if got.FullName.Confidence != sourceWeights["full_name"][types.SourceATS] {
		t.Errorf("expected full name confidence %v, got %v", sourceWeights["full_name"][types.SourceATS], got.FullName.Confidence)
	}
}

func TestPickScalarWinnerUsesRecencyThenAlphabeticalTiebreak(t *testing.T) {
	older := time.Now().Add(-72 * time.Hour)
	newer := time.Now().Add(-2 * time.Hour)

	candidates := []scalarCandidate{
		{Value: "ats-value", Source: types.SourceATS, Confidence: 0.5},
		{Value: "github-value", Source: types.SourceGitHub, Confidence: 0.5},
	}
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, LastUpdated: older},
		{Source: types.SourceGitHub, LastUpdated: newer},
	}

	got := pickScalarWinner(candidates, profiles)
	if got.Source != types.SourceGitHub {
		t.Fatalf("expected newer github candidate, got %v", got.Source)
	}

	profiles[1].LastUpdated = older
	got = pickScalarWinner(candidates, profiles)
	if got.Source != types.SourceATS {
		t.Fatalf("expected alphabetical ATS tiebreak, got %v", got.Source)
	}
}

func TestMergePhonesAreUnionedAndDeduplicated(t *testing.T) {
	logger := NewCorrelationLogger("test-phones")
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, Phones: []string{"+15550000000", "+15550000000"}},
		{Source: types.SourceNotes, Phones: []string{"+15551111111"}},
	}

	got := Merge(logger, profiles)

	want := []string{"+15550000000", "+15551111111"}
	assertStringSlice(t, got.Phones.Value, want)
	if got.Phones.Source != types.Source("ats+notes") {
		t.Errorf("expected combined source ats+notes, got %v", got.Phones.Source)
	}
	wantConfidence := (sourceWeights["phones"][types.SourceATS] + sourceWeights["phones"][types.SourceNotes]) / 2
	if got.Phones.Confidence != wantConfidence {
		t.Errorf("expected phone confidence %v, got %v", wantConfidence, got.Phones.Confidence)
	}
}

func TestMergeSkillsUnionPreservesFirstSeenCasing(t *testing.T) {
	logger := NewCorrelationLogger("test-skills")
	profiles := []*types.NormalizedProfile{
		{Source: types.SourceATS, Skills: []string{"Go", "SQL", "go"}},
		{Source: types.SourceGitHub, Skills: []string{"Python", "Go"}},
	}

	got := Merge(logger, profiles)

	assertStringSlice(t, got.Skills.Value, []string{"Go", "SQL", "Python"})
	if got.Skills.Source != types.Source("ats+github") {
		t.Errorf("expected combined source ats+github, got %v", got.Skills.Source)
	}
	wantConfidence := (sourceWeights["skills"][types.SourceATS] + sourceWeights["skills"][types.SourceGitHub]) / 2
	if got.Skills.Confidence != wantConfidence {
		t.Errorf("expected skills confidence %v, got %v", wantConfidence, got.Skills.Confidence)
	}
	if !hasProvenance(got.Provenance, "skills", types.SourceATS, "union") {
		t.Errorf("expected ATS skills provenance, got %v", got.Provenance)
	}
	if !hasProvenance(got.Provenance, "skills", types.SourceGitHub, "union") {
		t.Errorf("expected GitHub skills provenance, got %v", got.Provenance)
	}
}

func TestMergeStructuredFields(t *testing.T) {
	logger := NewCorrelationLogger("test-structured")
	years := 8.0
	profiles := []*types.NormalizedProfile{
		{
			Source:          types.SourceATS,
			CandidateID:     "cand-123",
			Location:        &types.Location{City: "San Francisco", Region: "CA", Country: "US"},
			Headline:        "Senior Software Engineer @ Acme",
			YearsExperience: &years,
			Experience: []types.ExperienceEntry{
				{Company: "Acme", Title: "Senior Software Engineer", Start: "2022-03"},
			},
			Education: []types.EducationEntry{
				{Institution: "UC Berkeley", Degree: "B.S.", Field: "Computer Science", EndYear: 2018},
			},
		},
		{
			Source:   types.SourceGitHub,
			Location: &types.Location{City: "SF", Region: "CA", Country: "US"},
			Headline: "Go developer",
			Links:    &types.Links{GitHub: "https://github.com/janedoe", Portfolio: "https://janedoe.dev"},
		},
	}

	got := Merge(logger, profiles)

	if got.CandidateID != "cand-123" {
		t.Errorf("expected candidate id, got %q", got.CandidateID)
	}
	if got.Location.Source != types.SourceATS {
		t.Errorf("expected ATS location by weight, got %v", got.Location.Source)
	}
	if got.Headline.Source != types.SourceATS {
		t.Errorf("expected ATS headline by weight, got %v", got.Headline.Source)
	}
	if got.YearsExperience.Value != 8 {
		t.Errorf("expected 8 years experience, got %v", got.YearsExperience.Value)
	}
	if got.Links.Value == nil || got.Links.Value.GitHub != "https://github.com/janedoe" {
		t.Errorf("expected github link, got %+v", got.Links.Value)
	}
	if len(got.Experience.Value) != 1 {
		t.Errorf("expected one experience entry, got %v", got.Experience.Value)
	}
	if len(got.Education.Value) != 1 {
		t.Errorf("expected one education entry, got %v", got.Education.Value)
	}
	if got.OverallConfidence <= 0 || got.OverallConfidence > 1 {
		t.Errorf("expected overall confidence in (0,1], got %v", got.OverallConfidence)
	}
}

func TestMergeSkipsNilEntriesAndHandlesEmptyInput(t *testing.T) {
	logger := NewCorrelationLogger("test-nil")
	profiles := []*types.NormalizedProfile{
		nil,
		{Source: types.SourceGitHub, Emails: []string{"jane@example.com"}},
		nil,
	}

	got := Merge(logger, profiles)
	assertStringSlice(t, got.Emails.Value, []string{"jane@example.com"})

	empty := Merge(logger, []*types.NormalizedProfile{})
	if empty.OverallConfidence != 0 {
		t.Errorf("expected zero overall confidence for empty input, got %v", empty.OverallConfidence)
	}
	if len(empty.Emails.Value) != 0 || len(empty.Skills.Value) != 0 {
		t.Errorf("expected empty collection fields, got emails=%v skills=%v", empty.Emails.Value, empty.Skills.Value)
	}
}

func FuzzResolveEmail(f *testing.F) {
	seeds := []string{
		"",
		"a@b.com",
		"not-an-email",
		"@@@@",
		"a@b@c.com",
		"   spaces   @ everywhere . com   ",
		"emoji@example.com",
		"a@" + string(make([]byte, 5000)),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	logger := NewCorrelationLogger("fuzz")

	f.Fuzz(func(t *testing.T, raw string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic on input %q: %v", raw, r)
			}
		}()

		profiles := []*types.NormalizedProfile{
			{Source: types.SourceATS, Emails: []string{raw}, LastUpdated: time.Now()},
			{Source: types.SourceGitHub, Emails: []string{"control@example.com"}, LastUpdated: time.Now()},
		}

		got := Merge(logger, profiles)
		if got.Emails.Confidence < 0 || got.Emails.Confidence > 1 {
			t.Fatalf("confidence out of bounds for input %q: %v", raw, got.Emails.Confidence)
		}
	})
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d values, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func hasProvenance(entries []types.ProvenanceEntry, field string, source types.Source, method string) bool {
	for _, entry := range entries {
		if entry.Field == field && entry.Source == source && entry.Method == method {
			return true
		}
	}
	return false
}
