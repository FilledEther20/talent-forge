# talent-forge

A small Go pipeline that ingests candidate data from multiple sources, resolves conflicts between them with a deterministic rules engine, and projects the unified record into a configurable JSON output shape.

It is a self-contained demo: external sources (ATS, GitHub, recruiter notes) are mocked or file-backed behind interfaces so the program runs without any network access or credentials, and the module has **zero third-party dependencies**.

---

## Pipeline at a glance

```
┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
│  Stage 1         │    │  Stage 2         │    │  Stage 3         │
│  Ingestion       │ →  │  Canonical       │ →  │  Dynamic         │
│  (extractors)    │    │  Engine (merger) │    │  Projector       │
└──────────────────┘    └──────────────────┘    └──────────────────┘
   NormalizedProfile         CanonicalProfile         JSON bytes
   (one per source)          (one, with provenance)   (shape from config)
```

For a deeper explanation of each stage, see [docs/architecture.md](docs/architecture.md). For the *why* behind every major design call, see [docs/decisions.md](docs/decisions.md).

---

## Requirements

- Go **1.22** or newer (the module declares `go 1.22.2`)
- macOS, Linux, or Windows — no platform-specific code

No other tooling is required.

---

## Quick start

From the repository root:

```bash
# build everything
go build ./...

# run the pipeline (uses the embedded default config from samples/configs/default.json)
go run .

# run with sample input files and a custom projection config
go run . -ats samples/ats.json -github samples/github.json -notes samples/notes.txt -config samples/configs/custom.json

# run all tests
go test ./...

# run all tests verbose, with the race detector
go test -race -v ./...
```

A successful `go run .` prints structured `slog` JSON logs followed by the final projected JSON document. With the default config, fields are wrapped with confidence because `include_confidence` is enabled. Example shape:

```json
{
  "candidate_id": {
    "value": "cand-123",
    "confidence": 0.9
  },
  "full_name": {
    "value": "Jane Doe",
    "confidence": 0.6
  },
  "emails": {
    "value": ["jane.doe@example.com", "jane@janedoe.dev"],
    "confidence": 0.6
  }
}
```

The field selection, remapping, per-field normalization, confidence toggle, and missing-value behavior all come from the projector config. [main.go](main.go) embeds [samples/configs/default.json](samples/configs/default.json) for the no-argument path, and `-config <path>` can replace it at runtime.

---

## Repository layout

```
.
├── main.go                  # entry point + embedded default config
├── go.mod                   # module = talent-forge, go 1.22.2
├── integration_test.go      # end-to-end test (extractor → merger → projector)
├── types/
│   └── types.go             # shared data contracts (no cycles, no logic)
├── extractor/
│   ├── extractor.go         # Extractor interface, ATS + GitHub + notes, FetchAll
│   └── extractor_test.go    # retry, normalization, graceful-degradation tests
├── merger/
│   ├── merger.go            # rules engine: weights × quality + tiebreaks
│   └── merger_test.go       # table tests + FuzzResolveEmail
├── projector/
│   ├── projector.go         # Config-driven JSON shaping + validation
│   └── projector_test.go    # path / from / confidence / missing-value tests
├── samples/
│   ├── ats.json             # sample structured source input
│   ├── github.json          # sample GitHub-like source input
│   ├── notes.txt            # sample recruiter notes input
│   └── configs/             # default and custom projector configs
└── docs/
    ├── architecture.md      # full architecture walkthrough
    ├── decisions.md         # the "why" for every design call
    └── testing.md           # test strategy + how to run each suite
```

---

## Common commands

| Goal | Command |
| --- | --- |
| Build all packages | `go build ./...` |
| Run the demo | `go run .` |
| Run with sample files | `go run . -ats samples/ats.json -github samples/github.json -notes samples/notes.txt` |
| Run with custom config | `go run . -config samples/configs/custom.json` |
| Run interactively | `go run . -interactive` |
| Run all tests | `go test ./...` |
| Run with race detector | `go test -race ./...` |
| Run a single package's tests | `go test ./merger/...` |
| Run a single test by name | `go test ./merger/... -run TestCanonicalMerger` |
| Run the fuzz test (30s) | `go test ./merger/... -fuzz=FuzzResolveEmail -fuzztime=30s` |
| Vet | `go vet ./...` |
| Coverage summary | `go test -cover ./...` |
| Coverage report | `go test -coverprofile=cover.out ./... && go tool cover -html=cover.out` |

---

## Configuring the projector

The output shape is controlled by JSON config. The current model uses an ordered `fields` list. Each field chooses an output `path`, an optional canonical `from` path, an optional `type`, an optional `required` flag, and optional projection-time normalization.

Example:

```json
{
  "fields": [
    { "path": "full_name", "from": "full_name.value", "type": "string", "required": true },
    { "path": "primary_email", "from": "emails.value[0]", "type": "string", "required": true },
    { "path": "skills", "from": "skills.value", "type": "string[]", "normalize": "canonical" }
  ],
  "include_confidence": false,
  "on_missing": "omit"
}
```

Config keys:

| Key | Effect |
| --- | --- |
| `fields` | Ordered output field list. |
| `fields[].path` | Output key name. |
| `fields[].from` | Canonical source path, such as `full_name.value` or `emails.value[0]`. Defaults to `path`. |
| `fields[].type` | Optional validation: `string`, `number`, `boolean`, `string[]`, `object`, `object[]`. |
| `fields[].required` | Missing/empty values become validation errors. |
| `fields[].normalize` | Projection normalization. Current values: `E164` and `canonical`. |
| `include_confidence` | If true, emits each field as `{value, confidence}`. |
| `on_missing` | One of `null`, `omit`, or `error`. |

The shape of the output document changes without a redeploy — see [docs/decisions.md](docs/decisions.md#12-runtime-projection-config) for why this is structured as a separate stage rather than baked into the merger.

---

## Where to read next

- **New to the codebase?** Read [docs/architecture.md](docs/architecture.md) first.
- **Reviewing the design?** Read [docs/decisions.md](docs/decisions.md).
- **Adding tests or debugging a failure?** Read [docs/testing.md](docs/testing.md).
