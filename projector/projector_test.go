package projector

import (
	"encoding/json"
	"testing"

	"talent-forge/types"
)

func sampleProfile() *types.CanonicalProfile {
	return &types.CanonicalProfile{
		FullName: types.FieldValue[string]{
			Value:      "Jane Doe",
			Source:     types.SourceATS,
			Confidence: 0.9,
		},
		Emails: types.FieldValue[[]string]{
			Value:      []string{"jane@example.com"},
			Source:     types.SourceGitHub,
			Confidence: 0.8,
		},
		Phones: types.FieldValue[[]string]{
			Value:      []string{"+15551234567"},
			Source:     types.SourceATS,
			Confidence: 0.7,
		},
		Skills: types.FieldValue[[]string]{
			Value:      []string{"Go", "Python"},
			Source:     types.SourceATS,
			Confidence: 0.6,
		},
		FieldConfidence: map[string]float64{
			"full_name.value": 0.9,
			"emails.value":    0.8,
			"phones.value":    0.7,
			"skills.value":    0.6,
		},
	}
}

func projectToMap(t *testing.T, profile *types.CanonicalProfile, cfg *Config) map[string]interface{} {
	t.Helper()

	raw, err := Project(profile, cfg)
	if err != nil {
		t.Fatalf("Project() returned error: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	return out
}

func TestParseConfigValid(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
{
	"fields":[
		{
			"path":"name",
			"from":"full_name.value"
		}
	]
}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Fields) != 1 {
		t.Fatalf("expected one field")
	}

	if cfg.Fields[0].From != "full_name.value" {
		t.Errorf("unexpected from path")
	}
}

func TestParseConfigInvalidJSON(t *testing.T) {
	_, err := ParseConfig([]byte("{bad json"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectSimpleField(t *testing.T) {
	cfg := &Config{
		Fields: []FieldSpec{
			{
				Path: "name",
				From: "full_name.value",
			},
		},
	}

	out := projectToMap(t, sampleProfile(), cfg)

	if out["name"] != "Jane Doe" {
		t.Fatalf("expected Jane Doe, got %v", out["name"])
	}
}

func TestProjectIncludeConfidence(t *testing.T) {
	cfg := &Config{
		IncludeConfidence: true,
		Fields: []FieldSpec{
			{
				Path: "name",
				From: "full_name.value",
			},
		},
	}

	out := projectToMap(t, sampleProfile(), cfg)

	v := out["name"].(map[string]interface{})

	if v["value"] != "Jane Doe" {
		t.Errorf("wrong value")
	}

	if _, ok := v["confidence"]; !ok {
		t.Error("expected confidence")
	}
}

func TestProjectNormalizePhone(t *testing.T) {
	profile := sampleProfile()

	profile.Phones.Value = []string{"(555) 123-4567"}

	cfg := &Config{
		Fields: []FieldSpec{
			{
				Path:      "phone",
				From:      "phones.value[0]",
				Normalize: "E164",
			},
		},
	}

	out := projectToMap(t, profile, cfg)

	if out["phone"] != "+15551234567" {
		t.Errorf("expected normalized phone, got %v", out["phone"])
	}
}

func TestProjectNormalizeSkills(t *testing.T) {
	profile := sampleProfile()

	profile.Skills.Value = []string{"Go", " PYTHON "}

	cfg := &Config{
		Fields: []FieldSpec{
			{
				Path:      "skills",
				From:      "skills.value",
				Normalize: "canonical",
			},
		},
	}

	out := projectToMap(t, profile, cfg)

	skills := out["skills"].([]interface{})

	if skills[0] != "go" {
		t.Errorf("expected go")
	}

	if skills[1] != "python" {
		t.Errorf("expected python")
	}
}

func TestProjectMissingFieldNull(t *testing.T) {
	cfg := &Config{
		OnMissing: OnMissingNull,
		Fields: []FieldSpec{
			{
				Path: "nickname",
				From: "nickname",
			},
		},
	}

	out := projectToMap(t, sampleProfile(), cfg)

	if _, ok := out["nickname"]; !ok {
		t.Fatal("expected nickname key")
	}

	if out["nickname"] != nil {
		t.Error("expected null")
	}
}

func TestProjectMissingFieldOmit(t *testing.T) {
	cfg := &Config{
		OnMissing: OnMissingOmit,
		Fields: []FieldSpec{
			{
				Path: "nickname",
				From: "nickname",
			},
		},
	}

	out := projectToMap(t, sampleProfile(), cfg)

	if _, ok := out["nickname"]; ok {
		t.Error("nickname should have been omitted")
	}
}

func TestProjectRequiredField(t *testing.T) {
	cfg := &Config{
		Fields: []FieldSpec{
			{
				Path:     "nickname",
				From:     "nickname",
				Required: true,
			},
		},
	}

	_, err := Project(sampleProfile(), cfg)

	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestProjectTypeValidation(t *testing.T) {
	cfg := &Config{
		Fields: []FieldSpec{
			{
				Path: "name",
				From: "full_name.value",
				Type: "number",
			},
		},
	}

	_, err := Project(sampleProfile(), cfg)

	if err == nil {
		t.Fatal("expected validation error")
	}
}
