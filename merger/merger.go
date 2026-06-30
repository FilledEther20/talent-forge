package merger

import (
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"talent-forge/types"
)

var sourceWeights = map[string]map[types.Source]float64{
	"candidate_id":     {types.SourceATS: 0.9, types.SourceGitHub: 0.1, types.SourceNotes: 0.1},
	"full_name":        {types.SourceATS: 0.6, types.SourceGitHub: 0.5, types.SourceNotes: 0.4},
	"emails":           {types.SourceATS: 0.6, types.SourceGitHub: 0.7, types.SourceNotes: 0.5},
	"phones":           {types.SourceATS: 0.8, types.SourceGitHub: 0.1, types.SourceNotes: 0.6},
	"skills":           {types.SourceATS: 0.4, types.SourceGitHub: 0.7, types.SourceNotes: 0.4},
	"location":         {types.SourceATS: 0.7, types.SourceGitHub: 0.5, types.SourceNotes: 0.4},
	"headline":         {types.SourceATS: 0.6, types.SourceGitHub: 0.4, types.SourceNotes: 0.5},
	"years_experience": {types.SourceATS: 0.9, types.SourceGitHub: 0.0, types.SourceNotes: 0.3},
	"links":            {types.SourceATS: 0.3, types.SourceGitHub: 0.9, types.SourceNotes: 0.6},
	"experience":       {types.SourceATS: 0.9, types.SourceGitHub: 0.2, types.SourceNotes: 0.4},
	"education":        {types.SourceATS: 0.9, types.SourceGitHub: 0.1, types.SourceNotes: 0.3},
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type CorrelationLogger struct {
	id string
}

func NewCorrelationLogger(id string) *CorrelationLogger {
	return &CorrelationLogger{id: id}
}

func (l *CorrelationLogger) logDecision(field string, winnerSource types.Source, confidence float64, reason string) {
	slog.Info("conflict resolved",
		"correlation_id", l.id,
		"field", field,
		"winning_source", winnerSource,
		"confidence", confidence,
		"reason", reason,
	)
}

type validator func(string) float64

func validateNonEmpty(s string) float64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return 1
}

func validateEmail(s string) float64 {
	if s == "" {
		return 0
	}
	if !emailRe.MatchString(s) {
		return 0
	}
	return 1
}

type scalarCandidate struct {
	Value      string
	Source     types.Source
	Confidence float64
	Reason     string
}

func Merge(logger *CorrelationLogger, profiles []*types.NormalizedProfile) *types.CanonicalProfile {
	live := make([]*types.NormalizedProfile, 0, len(profiles))
	for _, p := range profiles {
		if p != nil {
			live = append(live, p)
		}
	}

	canonical := &types.CanonicalProfile{
		Emails:          types.FieldValue[[]string]{Value: []string{}},
		Phones:          types.FieldValue[[]string]{Value: []string{}},
		Location:        types.FieldValue[*types.Location]{Value: nil},
		Links:           types.FieldValue[*types.Links]{Value: nil},
		Skills:          types.FieldValue[[]string]{Value: []string{}},
		Experience:      types.FieldValue[[]types.ExperienceEntry]{Value: []types.ExperienceEntry{}},
		Education:       types.FieldValue[[]types.EducationEntry]{Value: []types.EducationEntry{}},
		Provenance:      []types.ProvenanceEntry{},
		FieldConfidence: make(map[string]float64),
	}

	candidateID, cidConf, cidSrc, cidOk := resolveScalar(logger, "candidate_id", live,
		func(p *types.NormalizedProfile) string { return p.CandidateID }, validateNonEmpty)
	if cidOk {
		canonical.CandidateID = candidateID
		canonical.FieldConfidence["candidate_id"] = cidConf
		canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
			Field: "candidate_id", Source: cidSrc, Method: "weighted_choice",
		})
	}

	fullName, fnConf, fnSrc, fnOk := resolveScalar(logger, "full_name", live,
		func(p *types.NormalizedProfile) string { return p.FullName }, validateNonEmpty)
	if fnOk {
		canonical.FullName = types.FieldValue[string]{Value: fullName, Source: fnSrc, Confidence: fnConf}
		canonical.FieldConfidence["full_name"] = fnConf
		canonical.FieldConfidence["full_name.value"] = fnConf
		canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
			Field: "full_name", Source: fnSrc, Method: "weighted_choice",
		})
	}

	headline, hConf, hSrc, hOk := resolveScalar(logger, "headline", live,
		func(p *types.NormalizedProfile) string { return p.Headline }, validateNonEmpty)
	if hOk {
		canonical.Headline = types.FieldValue[string]{Value: headline, Source: hSrc, Confidence: hConf}
		canonical.FieldConfidence["headline"] = hConf
		canonical.FieldConfidence["headline.value"] = hConf
		canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
			Field: "headline", Source: hSrc, Method: "weighted_choice",
		})
	}

	emails, emailsConf, emailsSrcs := mergeStringSet(live, "emails",
		func(p *types.NormalizedProfile) []string { return p.Emails }, validateEmail)
	canonical.Emails = types.FieldValue[[]string]{Value: emails, Source: types.Source(joinSources(emailsSrcs)), Confidence: emailsConf}
	if len(emails) > 0 {
		canonical.FieldConfidence["emails"] = emailsConf
		canonical.FieldConfidence["emails.value"] = emailsConf
		for _, s := range emailsSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "emails", Source: s, Method: "union",
			})
		}
		logger.logDecision("emails", types.Source(joinSources(emailsSrcs)), emailsConf, "union across sources")
	}

	phones, phonesConf, phonesSrcs := mergeStringSet(live, "phones",
		func(p *types.NormalizedProfile) []string { return p.Phones }, validateNonEmpty)
	canonical.Phones = types.FieldValue[[]string]{Value: phones, Source: types.Source(joinSources(phonesSrcs)), Confidence: phonesConf}
	if len(phones) > 0 {
		canonical.FieldConfidence["phones"] = phonesConf
		canonical.FieldConfidence["phones.value"] = phonesConf
		for _, s := range phonesSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "phones", Source: s, Method: "union",
			})
		}
		logger.logDecision("phones", types.Source(joinSources(phonesSrcs)), phonesConf, "union across sources")
	}

	if loc, locConf, locSrc, ok := resolveLocation(logger, live); ok {
		canonical.Location = types.FieldValue[*types.Location]{Value: loc, Source: locSrc, Confidence: locConf}
		canonical.FieldConfidence["location"] = locConf
		canonical.FieldConfidence["location.value"] = locConf
		canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
			Field: "location", Source: locSrc, Method: "weighted_choice",
		})
	}

	if links, linksConf, linksSrcs, ok := mergeLinks(live); ok {
		canonical.Links = types.FieldValue[*types.Links]{Value: links, Source: types.Source(joinSources(linksSrcs)), Confidence: linksConf}
		canonical.FieldConfidence["links"] = linksConf
		canonical.FieldConfidence["links.value"] = linksConf
		for _, s := range linksSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "links", Source: s, Method: "merge",
			})
		}
		logger.logDecision("links", types.Source(joinSources(linksSrcs)), linksConf, "per-key merge across sources")
	}

	if yrs, yrsConf, yrsSrc, ok := resolveYearsExperience(logger, live); ok {
		canonical.YearsExperience = types.FieldValue[float64]{Value: yrs, Source: yrsSrc, Confidence: yrsConf}
		canonical.FieldConfidence["years_experience"] = yrsConf
		canonical.FieldConfidence["years_experience.value"] = yrsConf
		canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
			Field: "years_experience", Source: yrsSrc, Method: "weighted_choice",
		})
	}

	skills, skillsConf, skillsSrcs := mergeSkills(live)
	canonical.Skills = types.FieldValue[[]string]{Value: skills, Source: types.Source(joinSources(skillsSrcs)), Confidence: skillsConf}
	if len(skills) > 0 {
		canonical.FieldConfidence["skills"] = skillsConf
		canonical.FieldConfidence["skills.value"] = skillsConf
		for _, s := range skillsSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "skills", Source: s, Method: "union",
			})
		}
		logger.logDecision("skills", types.Source(joinSources(skillsSrcs)), skillsConf, "union+dedupe with per-skill confidence")
	}

	exp, expConf, expSrcs := mergeExperience(live)
	canonical.Experience = types.FieldValue[[]types.ExperienceEntry]{Value: exp, Source: types.Source(joinSources(expSrcs)), Confidence: expConf}
	if len(exp) > 0 {
		canonical.FieldConfidence["experience"] = expConf
		canonical.FieldConfidence["experience.value"] = expConf
		for _, s := range expSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "experience", Source: s, Method: "union",
			})
		}
	}

	edu, eduConf, eduSrcs := mergeEducation(live)
	canonical.Education = types.FieldValue[[]types.EducationEntry]{Value: edu, Source: types.Source(joinSources(eduSrcs)), Confidence: eduConf}
	if len(edu) > 0 {
		canonical.FieldConfidence["education"] = eduConf
		canonical.FieldConfidence["education.value"] = eduConf
		for _, s := range eduSrcs {
			canonical.Provenance = append(canonical.Provenance, types.ProvenanceEntry{
				Field: "education", Source: s, Method: "union",
			})
		}
	}

	canonical.OverallConfidence = computeOverallConfidence(canonical.FieldConfidence)
	return canonical
}

func resolveScalar(
	logger *CorrelationLogger,
	field string,
	profiles []*types.NormalizedProfile,
	extractFn func(*types.NormalizedProfile) string,
	qualityFn validator,
) (value string, confidence float64, src types.Source, ok bool) {
	var candidates []scalarCandidate

	for _, p := range profiles {
		raw := extractFn(p)
		if raw == "" {
			continue
		}
		quality := qualityFn(raw)
		weight := sourceWeights[field][p.Source]
		score := weight * quality
		candidates = append(candidates, scalarCandidate{
			Value:      raw,
			Source:     p.Source,
			Confidence: score,
			Reason:     buildReason(field, p.Source, weight, quality),
		})
	}

	if len(candidates) == 0 {
		return "", 0, "", false
	}

	winner := pickScalarWinner(candidates, profiles)
	logger.logDecision(field, winner.Source, winner.Confidence, winner.Reason)
	return winner.Value, winner.Confidence, winner.Source, true
}

func pickScalarWinner(candidates []scalarCandidate, profiles []*types.NormalizedProfile) scalarCandidate {
	lastUpdatedBySource := make(map[types.Source]int64)
	for _, p := range profiles {
		lastUpdatedBySource[p.Source] = p.LastUpdated.Unix()
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		switch {
		case c.Confidence > best.Confidence:
			best = c
		case c.Confidence == best.Confidence:
			if lastUpdatedBySource[c.Source] > lastUpdatedBySource[best.Source] {
				best = c
			} else if lastUpdatedBySource[c.Source] == lastUpdatedBySource[best.Source] {
				if c.Source < best.Source {
					best = c
				}
			}
		}
	}
	return best
}

func buildReason(field string, source types.Source, weight, quality float64) string {
	if quality == 0 {
		return field + ": rejected by " + string(source) + " due to failed quality validation (confidence=0)"
	}
	return field + ": " + string(source) + " selected (weight=" + trimFloat(weight) + ", quality=" + trimFloat(quality) + ")"
}

func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}

func mergeStringSet(
	profiles []*types.NormalizedProfile,
	field string,
	extractFn func(*types.NormalizedProfile) []string,
	qualityFn validator,
) (values []string, confidence float64, sources []types.Source) {
	seen := make(map[string]struct{})
	srcSeen := make(map[types.Source]struct{})
	var totalWeight float64
	var count int

	for _, p := range profiles {
		contributed := false
		for _, v := range extractFn(p) {
			if qualityFn(v) == 0 {
				continue
			}
			if _, ok := seen[v]; ok {
				contributed = true
				continue
			}
			seen[v] = struct{}{}
			values = append(values, v)
			contributed = true
		}
		if contributed {
			if _, dup := srcSeen[p.Source]; !dup {
				srcSeen[p.Source] = struct{}{}
				sources = append(sources, p.Source)
				totalWeight += sourceWeights[field][p.Source]
				count++
			}
		}
	}
	if count > 0 {
		confidence = totalWeight / float64(count)
	}
	return values, confidence, sources
}

func resolveLocation(logger *CorrelationLogger, profiles []*types.NormalizedProfile) (*types.Location, float64, types.Source, bool) {
	type cand struct {
		loc        *types.Location
		source     types.Source
		confidence float64
		reason     string
	}
	var candidates []cand
	for _, p := range profiles {
		if p.Location == nil {
			continue
		}
		quality := 0.0
		if p.Location.City != "" || p.Location.Region != "" || p.Location.Country != "" {
			quality = 1
		}
		weight := sourceWeights["location"][p.Source]
		candidates = append(candidates, cand{
			loc: p.Location, source: p.Source, confidence: weight * quality,
			reason: buildReason("location", p.Source, weight, quality),
		})
	}
	if len(candidates) == 0 {
		return nil, 0, "", false
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.confidence > best.confidence ||
			(c.confidence == best.confidence && c.source < best.source) {
			best = c
		}
	}
	logger.logDecision("location", best.source, best.confidence, best.reason)
	return best.loc, best.confidence, best.source, true
}

func mergeLinks(profiles []*types.NormalizedProfile) (*types.Links, float64, []types.Source, bool) {
	merged := &types.Links{}
	var sources []types.Source
	srcSeen := make(map[types.Source]struct{})
	var totalWeight float64
	var count int

	for _, p := range profiles {
		if p.Links == nil {
			continue
		}
		contributed := false
		if merged.LinkedIn == "" && p.Links.LinkedIn != "" {
			merged.LinkedIn = p.Links.LinkedIn
			contributed = true
		}
		if merged.GitHub == "" && p.Links.GitHub != "" {
			merged.GitHub = p.Links.GitHub
			contributed = true
		}
		if merged.Portfolio == "" && p.Links.Portfolio != "" {
			merged.Portfolio = p.Links.Portfolio
			contributed = true
		}
		for _, o := range p.Links.Other {
			if !containsString(merged.Other, o) {
				merged.Other = append(merged.Other, o)
				contributed = true
			}
		}
		if contributed {
			if _, dup := srcSeen[p.Source]; !dup {
				srcSeen[p.Source] = struct{}{}
				sources = append(sources, p.Source)
				totalWeight += sourceWeights["links"][p.Source]
				count++
			}
		}
	}
	if merged.LinkedIn == "" && merged.GitHub == "" && merged.Portfolio == "" && len(merged.Other) == 0 {
		return nil, 0, nil, false
	}
	conf := 0.0
	if count > 0 {
		conf = totalWeight / float64(count)
	}
	return merged, conf, sources, true
}

func resolveYearsExperience(logger *CorrelationLogger, profiles []*types.NormalizedProfile) (float64, float64, types.Source, bool) {
	type cand struct {
		value      float64
		source     types.Source
		confidence float64
		reason     string
	}
	var candidates []cand
	for _, p := range profiles {
		if p.YearsExperience == nil {
			continue
		}
		weight := sourceWeights["years_experience"][p.Source]
		if weight == 0 {
			continue
		}
		candidates = append(candidates, cand{
			value: *p.YearsExperience, source: p.Source, confidence: weight,
			reason: buildReason("years_experience", p.Source, weight, 1),
		})
	}
	if len(candidates) == 0 {
		return 0, 0, "", false
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.confidence > best.confidence ||
			(c.confidence == best.confidence && c.source < best.source) {
			best = c
		}
	}
	logger.logDecision("years_experience", best.source, best.confidence, best.reason)
	return best.value, best.confidence, best.source, true
}

func mergeSkills(profiles []*types.NormalizedProfile) ([]string, float64, []types.Source) {
	type skillAcc struct {
		display string
		sources []types.Source
		weights []float64
		order   int
	}
	acc := make(map[string]*skillAcc)
	order := 0

	srcSeen := make(map[types.Source]struct{})
	var totalWeight float64
	var srcCount int
	var contributingSources []types.Source

	for _, p := range profiles {
		if len(p.Skills) == 0 {
			continue
		}
		if _, dup := srcSeen[p.Source]; !dup {
			srcSeen[p.Source] = struct{}{}
			contributingSources = append(contributingSources, p.Source)
			totalWeight += sourceWeights["skills"][p.Source]
			srcCount++
		}
		for _, s := range p.Skills {
			key := strings.ToLower(strings.TrimSpace(s))
			if key == "" {
				continue
			}
			if existing, ok := acc[key]; ok {
				existing.sources = append(existing.sources, p.Source)
				existing.weights = append(existing.weights, sourceWeights["skills"][p.Source])
				continue
			}
			acc[key] = &skillAcc{
				display: s,
				sources: []types.Source{p.Source},
				weights: []float64{sourceWeights["skills"][p.Source]},
				order:   order,
			}
			order++
		}
	}

	keys := make([]string, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return acc[keys[i]].order < acc[keys[j]].order })

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		a := acc[k]
		out = append(out, a.display)
	}
	overall := 0.0
	if srcCount > 0 {
		overall = totalWeight / float64(srcCount)
	}
	return out, overall, contributingSources
}

func mergeExperience(profiles []*types.NormalizedProfile) ([]types.ExperienceEntry, float64, []types.Source) {
	seen := make(map[string]struct{})
	srcSeen := make(map[types.Source]struct{})
	var out []types.ExperienceEntry
	var sources []types.Source
	var totalWeight float64
	var count int
	for _, p := range profiles {
		if len(p.Experience) == 0 {
			continue
		}
		contributed := false
		for _, e := range p.Experience {
			key := strings.ToLower(e.Company + "|" + e.Title + "|" + e.Start)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, e)
			contributed = true
		}
		if contributed {
			if _, dup := srcSeen[p.Source]; !dup {
				srcSeen[p.Source] = struct{}{}
				sources = append(sources, p.Source)
				totalWeight += sourceWeights["experience"][p.Source]
				count++
			}
		}
	}
	conf := 0.0
	if count > 0 {
		conf = totalWeight / float64(count)
	}
	return out, conf, sources
}

func mergeEducation(profiles []*types.NormalizedProfile) ([]types.EducationEntry, float64, []types.Source) {
	seen := make(map[string]struct{})
	srcSeen := make(map[types.Source]struct{})
	var out []types.EducationEntry
	var sources []types.Source
	var totalWeight float64
	var count int
	for _, p := range profiles {
		if len(p.Education) == 0 {
			continue
		}
		contributed := false
		for _, e := range p.Education {
			key := strings.ToLower(e.Institution + "|" + e.Degree + "|" + e.Field)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, e)
			contributed = true
		}
		if contributed {
			if _, dup := srcSeen[p.Source]; !dup {
				srcSeen[p.Source] = struct{}{}
				sources = append(sources, p.Source)
				totalWeight += sourceWeights["education"][p.Source]
				count++
			}
		}
	}
	conf := 0.0
	if count > 0 {
		conf = totalWeight / float64(count)
	}
	return out, conf, sources
}

func computeOverallConfidence(fc map[string]float64) float64 {
	if len(fc) == 0 {
		return 0
	}
	var sum float64
	for _, v := range fc {
		sum += v
	}
	return round2(sum / float64(len(fc)))
}

func joinSources(srcs []types.Source) string {
	parts := make([]string, len(srcs))
	for i, s := range srcs {
		parts[i] = string(s)
	}
	return strings.Join(parts, "+")
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
