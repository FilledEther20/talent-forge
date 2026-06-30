package types

import "time"

type Source string

const (
	SourceATS    Source = "ats"
	SourceGitHub Source = "github"
	SourceNotes  Source = "notes"
)

type Location struct {
	City    string `json:"city,omitempty"`
	Region  string `json:"region,omitempty"`
	Country string `json:"country,omitempty"`
}

type Links struct {
	LinkedIn  string   `json:"linkedin,omitempty"`
	GitHub    string   `json:"github,omitempty"`
	Portfolio string   `json:"portfolio,omitempty"`
	Other     []string `json:"other,omitempty"`
}

type ExperienceEntry struct {
	Company string `json:"company"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type EducationEntry struct {
	Institution string `json:"institution"`
	Degree      string `json:"degree,omitempty"`
	Field       string `json:"field,omitempty"`
	EndYear     int    `json:"end_year,omitempty"`
}

// Generic wrapper for canonical fields.
type FieldValue[T any] struct {
	Value      T       `json:"value"`
	Source     Source  `json:"source"`
	Confidence float64 `json:"confidence"`
}

type Skill struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Sources    []Source `json:"sources"`
}

type ProvenanceEntry struct {
	Field  string `json:"field"`
	Source Source `json:"source"`
	Method string `json:"method"`
}

type NormalizedProfile struct {
	Source          Source
	CandidateID     string
	FullName        string
	Emails          []string
	Phones          []string
	Location        *Location
	Links           *Links
	Headline        string
	YearsExperience *float64
	Skills          []string
	Experience      []ExperienceEntry
	Education       []EducationEntry
	LastUpdated     time.Time
}

type CanonicalProfile struct {
	CandidateID string `json:"candidate_id"`

	FullName        FieldValue[string]            `json:"full_name"`
	Emails          FieldValue[[]string]          `json:"emails"`
	Phones          FieldValue[[]string]          `json:"phones"`
	Location        FieldValue[*Location]         `json:"location"`
	Links           FieldValue[*Links]            `json:"links"`
	Headline        FieldValue[string]            `json:"headline"`
	YearsExperience FieldValue[float64]           `json:"years_experience"`
	Skills          FieldValue[[]string]          `json:"skills"`
	Experience      FieldValue[[]ExperienceEntry] `json:"experience"`
	Education       FieldValue[[]EducationEntry]  `json:"education"`

	Provenance        []ProvenanceEntry  `json:"provenance,omitempty"`
	OverallConfidence float64            `json:"overall_confidence"`
	FieldConfidence   map[string]float64 `json:"-"`
}
