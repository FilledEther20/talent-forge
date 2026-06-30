# Design Decisions

This document explains why the current `talent-forge` design looks the way it does. It reflects the latest implementation: richer canonical schema, ATS/GitHub/notes sources, file-based and interactive CLI input, generic `FieldValue[T]`, and spec-style projector configuration.

## 1. Three Stages Instead Of One Pipeline Function

The code is split into ingestion, canonical merging, and projection because those are different problems.

Ingestion knows how to talk to source-shaped data. The merger knows how to resolve conflicts. The projector knows how to satisfy a requested output schema. Keeping those concerns separate lets a new source be added without touching projection logic, and lets a new output shape be added without changing the merger.

The rejected alternative was one large `pipeline` package. That would be shorter initially, but it would mix source quirks, confidence rules, and output formatting into the same functions. For this assignment, the clean separation matters because the prompt explicitly asks for a separation between the internal canonical record and the projection layer.

## 2. A Shared `types` Package

`types` holds the contracts between stages: `NormalizedProfile`, `CanonicalProfile`, nested objects, sources, provenance, and `FieldValue[T]`.

The main reason is import-cycle avoidance. If `merger` owned `CanonicalProfile`, then `projector` would need to import `merger` just to use a data type. That would make the projector depend on merger internals. With `types`, each package depends on contracts, not sibling implementation.

The trade-off is that schema changes touch `types` first and then ripple into stages. That is intentional. Schema changes are important enough to be explicit.

## 3. Full Canonical Schema

The canonical schema includes candidate ID, name, email array, phone array, location, links, headline, years of experience, skills, experience, education, provenance, and overall confidence.

This mirrors the assignment's default output schema instead of keeping only a tiny demo subset. It also avoids pretending that inherently repeated values, like emails and phones, are scalars. That matters for correctness: multiple valid emails or phones can be true at the same time.

Some fields are pointer-backed in the normalized form, such as `Location`, `Links`, and `YearsExperience`, because missing is different from present-but-empty. That distinction feeds into projection policies like `null`, `omit`, and `error`.

## 4. Generic `FieldValue[T]`

Most canonical fields are wrapped in `FieldValue[T]`:

```go
type FieldValue[T any] struct {
    Value      T
    Source     Source
    Confidence float64
}
```

The original non-generic `interface{}` wrapper was flexible but weakly typed. The generic wrapper keeps the same provenance model while preserving type information in Go. `FieldValue[[]string]` is clearer than `FieldValue{Value: interface{}}`.

The projector still converts the canonical profile into a generic map internally because runtime paths like `emails.value[0]` and dynamic output fields are not statically known. That flexibility belongs in projection, not in the canonical model.

## 5. Provenance As A Top-Level Audit Trail

The canonical record includes `Provenance []ProvenanceEntry` in addition to field-level source and confidence.

Field-level metadata answers: "What source won this field?" Top-level provenance answers: "Which sources contributed to the final record, and by which method?"

The methods are intentionally simple strings:

- `weighted_choice` for scalar winner selection.
- `union` for arrays like emails, phones, skills, experience, and education.
- `merge` for object-like fields such as links.

This makes the result explainable without forcing every downstream consumer to replay the merger.

## 6. Deterministic Rules Instead Of ML

Conflict resolution is rule-based. That is deliberate.

The assignment values explainability and deterministic output. A model could be useful at larger scale, but it would introduce non-obvious behavior, training-data questions, drift, and a harder audit story. Here, the rules are visible in `sourceWeights`, validators, and tie-breakers.

Same input should produce the same canonical output. That is more important for this project than squeezing out a marginal improvement from statistical ranking.

## 7. Source Weights Plus Quality Validation

The merger uses this basic formula for scalar values:

$$
confidence = source\_weight \times quality
$$

Source weights encode field-specific reliability. ATS is strong for candidate ID, years of experience, experience, and education. GitHub is strong for links and technical skills. Recruiter notes are useful for phones, LinkedIn links, and extra skills, but they are less structured.

Quality validation prevents garbage from winning purely because it came from a high-priority source. For example, a malformed email scores quality `0`, so it does not survive into the merged email set.

Recency is used as a tie-breaker for scalar choices, not as part of the main score. That keeps the scoring model simple and avoids inventing a freshness decay formula.

## 8. Arrays Are Unioned Instead Of Winner-Take-All

Emails, phones, skills, experience, and education are merged as sets because sources can contribute complementary information.

If ATS has one valid phone and recruiter notes have another valid phone, keeping both is better than making a false winner-take-all choice. The same applies to skills: Go from ATS and Python from GitHub are not contradictory.

The trade-off is that array-level confidence becomes an aggregate rather than confidence for each element. The current code stores one confidence for the whole field. The `types.Skill` struct exists for a richer future shape with per-skill confidence and sources, but the live merger currently emits skills as `[]string`.

## 9. Notes Extractor For Unstructured Input

The project includes `NotesExtractor` to handle unstructured recruiter notes.

It uses regular expressions for emails, phones, URLs, and name phrases, plus keyword matching for skills. This is intentionally lightweight. It demonstrates the pipeline's ability to ingest unstructured text without bringing in PDF/DOCX parsing, NLP dependencies, or network services.

The rejected alternative was parsing resumes or LinkedIn pages. Those would require dependencies, scraping concerns, or file formats that distract from the core transformation problem.

## 10. File-Based And Interactive CLI Input

`main.go` supports both file flags and an interactive mode:

```bash
go run . -ats samples/ats.json -github samples/github.json -notes samples/notes.txt -config samples/configs/custom.json
go run . -interactive
```

File flags make the project submission-friendly: a reviewer can point the program at sample inputs and a config. Interactive mode makes the demo more presentable because values can be typed live and the resulting conflict resolution can be observed immediately.

The built-in defaults remain so `go run .` works with no setup.

## 11. Graceful Degradation

`FetchAll` runs every extractor concurrently and treats source failure as local. A failed source logs a warning and returns `nil`; the merger skips nil profiles.

This is a direct response to the robustness requirement: a missing or broken source must not crash the run. A partial but honest profile is better than no profile.

The trade-off is that downstream stages must handle missing data. That is why the projector has explicit `on_missing` behavior.

## 12. Runtime Projection Config

The projector accepts the assignment-style config shape:

```json
{
  "fields": [
    { "path": "primary_email", "from": "emails.value[0]", "type": "string", "required": true }
  ],
  "include_confidence": true,
  "on_missing": "null"
}
```

This config supports:

- Selecting a subset of fields.
- Renaming/remapping with `path` and `from`.
- Per-field normalization, currently `E164` and `canonical`.
- Confidence wrapping with `include_confidence`.
- Missing-value policy: `null`, `omit`, or `error`.
- Runtime type validation.

This design keeps the canonical record stable while letting output consumers request different JSON shapes.

## 13. Strict Config Parsing

`ParseConfig` uses `json.Decoder.DisallowUnknownFields()`.

The reason is simple: a typo in config should fail loudly. Without strict parsing, `include_confidnce` would be ignored and the user might think confidence is enabled when it is not.

The trade-off is less permissive config evolution. If future versions add new keys, older binaries will reject them. That is acceptable for this project because correctness is more important than loose compatibility.

## 14. Projection-Time Validation

The projector validates the requested schema while building output.

It checks:

- Required fields.
- Missing-field policy.
- Declared types.
- Normalization compatibility.

Validation returns all failures in a `ValidationError` instead of stopping on the first one. That makes bad configs easier to fix because the user gets a list of problems in one run.

## 15. Stable JSON Output Ordering

Projection uses `marshalOrdered` instead of directly marshaling a map.

Go map iteration order is randomized. For a demo, tests, and predictable review output, the final JSON should follow config order. `marshalOrdered` writes fields in the order they appear in `fields`.

This is a small amount of manual JSON assembly, but it is isolated and backed by tests.

## 16. Standard Library Only

The project uses no third-party dependencies.

That keeps setup simple for reviewers: clone, `go test ./...`, `go run .`. It also forces the important design decisions to be visible instead of hidden behind libraries.

The main place this hurts is phone normalization. Real E.164 handling should use a proven library in production. The current function is a demo-grade normalizer that handles the project samples and common cases.

## 17. What Is Deliberately Left Small

Several things are intentionally not built out:

- No database; the pipeline is stateless.
- No HTTP server; CLI is enough for the assignment and demo.
- No worker pool; three extractors do not need one.
- No real GitHub API calls; mock/file inputs make tests deterministic.
- No PDF/DOCX parser; recruiter notes are the unstructured source.

These are not missing because they were forgotten. They are scoped out to keep the implementation focused on the transformation engine, correctness, and reasoning.

## 18. Known Trade-Offs And Future Improvements

Useful next steps:

- Change `Skills` from `FieldValue[[]string]` to `FieldValue[[]Skill]` so each skill carries its own confidence and sources.
- Inject a `*rand.Rand` or fetch function into extractors for deterministic mid-range failure-rate tests.
- Replace the phone normalizer with a real libphonenumber-compatible package.
- Add config-driven source weights so merger behavior can be tuned without recompilation.
- Add a stricter schema validator for nested object shapes, not just top-level `object` and `object[]` checks.
- Add a golden output file for the default sample run if submission review requires exact output artifacts.
