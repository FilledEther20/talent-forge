# Testing

This document describes the current test strategy for `talent-forge`, including how to run tests, what each package covers, and where the known gaps are.

## Commands

Run everything:

```bash
go test ./...
```

Run with coverage:

```bash
go test -cover ./...
```

Run verbose tests for one package:

```bash
go test -v ./extractor
go test -v ./merger
go test -v ./projector
```

Run the integration tests from the module root:

```bash
go test -v -run TestPipeline ./...
```

Run the merger fuzzer briefly:

```bash
go test ./merger -run=^$ -fuzz=FuzzResolveEmail -fuzztime=30s
```

Run static checks:

```bash
go vet ./...
```

Generate an HTML coverage report:

```bash
go test -coverprofile=cover.out ./... && go tool cover -html=cover.out
```

## Test Layout

| Area | File | Purpose |
| --- | --- | --- |
| Extractor unit tests | [../extractor/extractor_test.go](../extractor/extractor_test.go) | Retry behavior, normalization helpers, ATS/GitHub/notes parsing, file inputs, concurrent degradation. |
| Merger unit tests | [../merger/merger_test.go](../merger/merger_test.go) | Conflict resolution, malformed email filtering, scalar tie-breaks, unions, structured fields, provenance, confidence. |
| Projector unit tests | [../projector/projector_test.go](../projector/projector_test.go) | Config parsing, field paths, confidence output, normalization, missing values, required fields, type validation. |
| End-to-end tests | [../integration_test.go](../integration_test.go) | Extractor -> merger -> projector with spec-style config and deterministic output assertions. |

Tests are stdlib-only. There is no `testify`, no Docker, no network, and no external services.

## Extractor Coverage

[../extractor/extractor_test.go](../extractor/extractor_test.go) tests the ingestion layer directly, including unexported helpers because the test file is in package `extractor`.

Covered behavior:

- `withRetry` succeeds on the first try.
- `withRetry` succeeds after earlier failures.
- `withRetry` exhausts attempts and wraps the sentinel error.
- `withRetry` respects a pre-cancelled context and makes zero calls.
- `pow2` returns expected backoff multipliers.
- `cleanString` trims and collapses whitespace.
- `normalizePhoneE164` handles US 10-digit numbers, existing `+` prefixes, empty input, junk input, and formatted numbers.
- `normalizeEmails` lowercases, trims, deduplicates, and drops empty values.
- `normalizePhones` normalizes and deduplicates phones.
- `dedupeSkills` deduplicates case-insensitively and preserves first casing.
- `toISOCountry` maps common country names and two-letter codes.
- `parseLocationString` converts comma-separated location text into `Location`.

Extractor scenarios:

- `ATSExtractor` normalizes the built-in structured record.
- `ATSExtractor` can use interactive stub values.
- `ATSExtractor` loads JSON from a file.
- `GitHubExtractor` normalizes the built-in profile.
- `GitHubExtractor` loads JSON from a file.
- `NotesExtractor` parses recruiter notes for name, email, phone, skills, and links.

Orchestration scenarios:

- All extractors succeed and result order is preserved.
- One extractor fails and the others still return data.
- All extractors fail and the result slice contains only nil entries.

## Merger Coverage

[../merger/merger_test.go](../merger/merger_test.go) focuses on the canonical engine.

Covered behavior:

- Malformed email values are filtered out of the email union.
- All malformed emails produce an empty email set and zero confidence.
- Full name selection uses source weights.
- Scalar tie-breaking uses recency first and alphabetical source name second.
- Phones are unioned and deduplicated.
- Skills are unioned, deduplicated, and preserve first-seen casing.
- Structured fields are resolved or merged: candidate ID, location, headline, years of experience, links, experience, and education.
- Nil profile entries are skipped.
- Empty input does not panic and returns zero overall confidence.
- Field confidence lookup keys are populated for projector use.
- Provenance entries are created for union and weighted-choice fields.

The fuzzer `FuzzResolveEmail` feeds arbitrary strings into the email merge path. It checks that the merger does not panic and keeps email confidence inside `[0, 1]`.

## Projector Coverage

[../projector/projector_test.go](../projector/projector_test.go) tests the runtime output layer.

Covered behavior:

- Valid config parses.
- Invalid JSON fails.
- Simple field projection works.
- `include_confidence` wraps output as `{value, confidence}`.
- `normalize: "E164"` normalizes projected phones.
- `normalize: "canonical"` canonicalizes projected skills.
- `on_missing: "null"` writes an explicit null.
- `on_missing: "omit"` omits missing fields.
- Required missing fields return an error.
- Type mismatches return validation errors.

Important projector behavior to remember:

- `ParseConfig` is strict and rejects unknown JSON keys.
- `path` controls the output key.
- `from` controls the canonical source path.
- If `from` is omitted, the projector uses `path` as the source path.
- Confidence lookup uses paths like `full_name.value`, `emails.value`, and fallback root keys.

## Integration Coverage

[../integration_test.go](../integration_test.go) composes the real packages in process:

1. Build ATS, GitHub, and notes extractors.
2. Fetch normalized profiles concurrently.
3. Merge into a canonical profile.
4. Parse a spec-style projection config.
5. Project into final JSON.
6. Unmarshal and assert the output contract.

The integration test verifies:

- `candidate_id` is present.
- `full_name` resolves to `Jane Doe`.
- `primary_email` resolves to the valid GitHub email.
- Malformed ATS email is excluded.
- Emails include GitHub and notes values.
- Phones include ATS and notes values in E.164-like format.
- Location, links, headline, years of experience, skills, experience, education, provenance, and overall confidence are present.
- Confidence output can be enabled.
- Missing optional fields can become `{value: null, confidence: 0}`.
- Projected output is deterministic across two runs for the asserted keys.

The integration test does not shell out to `go run .`. Direct in-process composition is faster, less flaky, and tests the same package behavior without depending on terminal output formatting.

## Determinism Rules In Tests

The tests avoid non-determinism where possible:

- `FailureRate` is used at `0` or via fake extractors, not random mid-range values.
- Merger tie-break tests use fixed relative timestamps.
- Projection output order follows config order.
- Integration tests compare JSON-marshaled values across runs.

Some built-in sample data uses `time.Now()` for `LastUpdated`. Tests do not assert exact timestamps; they assert stable selected values and output fields.

## Known Gaps

Current gaps worth tracking:

- `go test ./...` should be treated as the source of truth before submission. If it fails, update the stale test or implementation before relying on docs.
- Skills are currently projected as `[]string`, while the richer `types.Skill` struct exists but is not yet produced by the merger.
- Projector type validation checks broad shapes like `object` and `object[]`; it does not validate nested object schemas.
- `normalizePhoneE164` is demo-grade and should be replaced by a real phone library for production.
- Mid-range random failure behavior is not deterministic because extractors use the global `math/rand` source.
- The integration test does not verify CLI flag parsing or embedded config loading through `os/exec`.

## Recommended Pre-Submission Check

Before recording a demo or submitting the project, run:

```bash
go fmt ./...
go test ./...
go test -cover ./...
go vet ./...
```

Then run the demo surfaces:

```bash
go run .
go run . -config samples/configs/custom.json
go run . -interactive
./demo.sh --fast
```

If the config or schema changes, update the docs, projector tests, and integration test together. Those three are the guardrails that keep the assignment requirements visible.
