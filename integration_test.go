package main

import (
	"context"
	"encoding/json"
	"testing"

	"talent-forge/extractor"
	"talent-forge/merger"
	"talent-forge/projector"
)

const integrationConfigJSON = `{
  "fields": [
    {"path": "candidate_id", "type": "string", "required": true},
    {"path": "full_name", "from": "full_name.value", "type": "string", "required": true},
    {"path": "primary_email", "from": "emails.value[0]", "type": "string", "required": true},
    {"path": "emails", "from": "emails.value", "type": "string[]"},
    {"path": "phones", "from": "phones.value", "type": "string[]", "normalize": "E164"},
    {"path": "location", "from": "location.value", "type": "object"},
    {"path": "links", "from": "links.value", "type": "object"},
    {"path": "headline", "from": "headline.value", "type": "string"},
    {"path": "years_experience", "from": "years_experience.value", "type": "number"},
    {"path": "skills", "from": "skills.value", "type": "string[]", "normalize": "canonical"},
    {"path": "experience", "from": "experience.value", "type": "object[]"},
    {"path": "education", "from": "education.value", "type": "object[]"},
    {"path": "provenance", "type": "object[]"},
    {"path": "overall_confidence", "type": "number"}
  ],
  "include_confidence": false,
  "on_missing": "null"
}`

const integrationConfidenceConfigJSON = `{
  "fields": [
    {"path": "full_name", "from": "full_name.value", "type": "string", "required": true},
    {"path": "primary_email", "from": "emails.value[0]", "type": "string", "required": true},
    {"path": "missing_optional", "from": "does_not_exist.value", "type": "string"}
  ],
  "include_confidence": true,
  "on_missing": "null"
}`

func runIntegrationPipeline(t *testing.T, rawConfig string) map[string]interface{} {
	t.Helper()

	extractors := []extractor.Extractor{
		&extractor.ATSExtractor{CandidateID: "cand-123", FailureRate: 0},
		&extractor.GitHubExtractor{Username: "janedoe", FailureRate: 0},
		&extractor.NotesExtractor{FailureRate: 0},
	}

	normalized := extractor.FetchAll(context.Background(), extractors)
	canonical := merger.Merge(merger.NewCorrelationLogger("integration-test"), normalized)

	cfg, err := projector.ParseConfig([]byte(rawConfig))
	if err != nil {
		t.Fatalf("ParseConfig() failed: %v", err)
	}

	out, err := projector.Project(canonical, cfg)
	if err != nil {
		t.Fatalf("Project() failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("projected output is not valid JSON: %v\n%s", err, out)
	}
	return result
}

func TestPipelineProjectsSpecStyleOutput(t *testing.T) {
	result := runIntegrationPipeline(t, integrationConfigJSON)

	if result["candidate_id"] != "cand-123" {
		t.Fatalf("candidate_id = %v, want cand-123", result["candidate_id"])
	}
	if result["full_name"] != "Jane Doe" {
		t.Fatalf("full_name = %v, want Jane Doe", result["full_name"])
	}
	if result["primary_email"] != "jane.doe@example.com" {
		t.Fatalf("primary_email = %v, want jane.doe@example.com", result["primary_email"])
	}

	emails := asStringSlice(t, result["emails"])
	if !contains(emails, "jane.doe@example.com") || !contains(emails, "jane@janedoe.dev") {
		t.Fatalf("emails = %v, want GitHub and notes emails", emails)
	}
	if contains(emails, "jane.doe@badformat") {
		t.Fatalf("emails should not include malformed ATS email: %v", emails)
	}

	phones := asStringSlice(t, result["phones"])
	if !contains(phones, "+15551234567") || !contains(phones, "+14155559988") {
		t.Fatalf("phones = %v, want ATS and notes E.164 phones", phones)
	}

	location, ok := result["location"].(map[string]interface{})
	if !ok {
		t.Fatalf("location = %T, want object", result["location"])
	}
	if location["country"] != "US" {
		t.Fatalf("location.country = %v, want US", location["country"])
	}

	links, ok := result["links"].(map[string]interface{})
	if !ok {
		t.Fatalf("links = %T, want object", result["links"])
	}
	if links["github"] != "https://github.com/janedoe" {
		t.Fatalf("links.github = %v, want https://github.com/janedoe", links["github"])
	}
	if links["linkedin"] != "https://linkedin.com/in/janedoe" {
		t.Fatalf("links.linkedin = %v, want https://linkedin.com/in/janedoe", links["linkedin"])
	}

	if result["years_experience"] != float64(8) {
		t.Fatalf("years_experience = %v, want 8", result["years_experience"])
	}

	skills := asStringSlice(t, result["skills"])
	for _, want := range []string{"go", "sql", "kubernetes", "postgresql", "python", "typescript", "distributed systems"} {
		if !contains(skills, want) {
			t.Fatalf("skills = %v, missing %q", skills, want)
		}
	}

	if len(asObjectSlice(t, result["experience"])) == 0 {
		t.Fatal("expected experience entries")
	}
	if len(asObjectSlice(t, result["education"])) == 0 {
		t.Fatal("expected education entries")
	}
	if len(asObjectSlice(t, result["provenance"])) == 0 {
		t.Fatal("expected provenance entries")
	}
	if confidence, ok := result["overall_confidence"].(float64); !ok || confidence <= 0 || confidence > 1 {
		t.Fatalf("overall_confidence = %v, want value in (0,1]", result["overall_confidence"])
	}
}

func TestPipelineProjectionCanIncludeConfidence(t *testing.T) {
	result := runIntegrationPipeline(t, integrationConfidenceConfigJSON)

	fullName, ok := result["full_name"].(map[string]interface{})
	if !ok {
		t.Fatalf("full_name = %T, want object with value/confidence", result["full_name"])
	}
	if fullName["value"] != "Jane Doe" {
		t.Fatalf("full_name.value = %v, want Jane Doe", fullName["value"])
	}
	if confidence, ok := fullName["confidence"].(float64); !ok || confidence <= 0 {
		t.Fatalf("full_name.confidence = %v, want positive number", fullName["confidence"])
	}

	missing, ok := result["missing_optional"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing_optional = %T, want object with null value/confidence", result["missing_optional"])
	}
	if missing["value"] != nil {
		t.Fatalf("missing_optional.value = %v, want nil", missing["value"])
	}
	if missing["confidence"] != float64(0) {
		t.Fatalf("missing_optional.confidence = %v, want 0", missing["confidence"])
	}
}

func TestPipelineProjectionIsDeterministic(t *testing.T) {
	first := runIntegrationPipeline(t, integrationConfigJSON)
	second := runIntegrationPipeline(t, integrationConfigJSON)

	for _, key := range []string{
		"candidate_id",
		"full_name",
		"primary_email",
		"emails",
		"phones",
		"location",
		"links",
		"headline",
		"years_experience",
		"skills",
		"experience",
		"education",
		"overall_confidence",
	} {
		firstValue, err := json.Marshal(first[key])
		if err != nil {
			t.Fatalf("marshal first[%q]: %v", key, err)
		}
		secondValue, err := json.Marshal(second[key])
		if err != nil {
			t.Fatalf("marshal second[%q]: %v", key, err)
		}
		if string(firstValue) != string(secondValue) {
			t.Fatalf("non-deterministic output for %q: %s vs %s", key, firstValue, secondValue)
		}
	}
}

func asStringSlice(t *testing.T, value interface{}) []string {
	t.Helper()

	raw, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value = %T, want []interface{}", value)
	}
	out := make([]string, 0, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("item %d = %T, want string", i, item)
		}
		out = append(out, s)
	}
	return out
}

func asObjectSlice(t *testing.T, value interface{}) []map[string]interface{} {
	t.Helper()

	raw, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value = %T, want []interface{}", value)
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("item %d = %T, want object", i, item)
		}
		out = append(out, obj)
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
