#!/usr/bin/env bash
#
# demo.sh — guided walkthrough of the talent-forge project.
#
# Usage:
#   ./demo.sh                 # pauses for ENTER between sections; runs the canonical scenario
#   ./demo.sh --auto          # runs every section straight through (no pauses)
#   ./demo.sh --fast          # skips the fuzz test (saves ~5s)
#   ./demo.sh --interactive   # final pipeline step prompts you for ATS/GitHub field values
#
# Requires: go 1.22+. Optional: jq (for prettier final JSON).

set -euo pipefail

AUTO=0
FAST=0
INTERACTIVE=0
for arg in "$@"; do
  case "$arg" in
    --auto) AUTO=1 ;;
    --fast) FAST=1 ;;
    --interactive) INTERACTIVE=1 ;;
    -h|--help)
      sed -n '2,11p' "$0"
      exit 0
      ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# ---- colours (fall back to plain text if not a TTY) -------------------------
if [[ -t 1 ]]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; CYAN=$'\033[36m'; GREEN=$'\033[32m'
  YELLOW=$'\033[33m'; MAGENTA=$'\033[35m'; RESET=$'\033[0m'
else
  BOLD=""; DIM=""; CYAN=""; GREEN=""; YELLOW=""; MAGENTA=""; RESET=""
fi

step=0
section() {
  step=$((step + 1))
  echo
  echo "${CYAN}${BOLD}━━━ Step ${step}: $1 ${RESET}"
  echo "${DIM}$2${RESET}"
  echo
}

run() {
  echo "${YELLOW}\$ $*${RESET}"
  "$@"
}

pause() {
  if [[ "$AUTO" -eq 1 ]]; then return; fi
  echo
  read -r -p "${MAGENTA}↵ press ENTER for next step (q to quit) ${RESET}" reply
  if [[ "$reply" == "q" ]]; then
    echo "bye."
    exit 0
  fi
}

cd "$(dirname "$0")"

# ---- intro -------------------------------------------------------------------
clear || true
cat <<EOF
${BOLD}talent-forge — live demo${RESET}

A 3-stage Go pipeline that ingests candidate data from multiple sources,
resolves conflicts with a deterministic rules engine, and projects the
result into a configurable JSON shape.

  ${DIM}extractor  →  merger  →  projector${RESET}

This script walks through the project, runs the tests, and finishes by
executing the pipeline end-to-end so you can see the structured logs and
the final JSON output.
EOF
pause

# ---- 1. project layout -------------------------------------------------------
section "Project layout" \
  "Three packages — one per pipeline stage — plus a shared types package."
if command -v tree >/dev/null 2>&1; then
  run tree -L 2 -I 'node_modules|.git'
else
  run ls -1
  echo
  for d in extractor merger projector types docs; do
    [[ -d "$d" ]] && echo "${DIM}$d/${RESET}" && ls -1 "$d" | sed 's/^/  /'
  done
fi
pause

# ---- 2. build & vet ----------------------------------------------------------
section "Build & static checks" \
  "Confirms the module compiles cleanly and passes go vet."
run go build ./...
run go vet ./...
echo "${GREEN}✓ build + vet clean${RESET}"
pause

# ---- 3. merger tests ---------------------------------------------------------
section "Merger tests — the rules engine" \
  "Verifies conflict resolution: quality > weight, recency tiebreak, alphabetical fallback, skills union."
run go test -v ./merger/...
pause

# ---- 4. extractor tests ------------------------------------------------------
section "Extractor tests — ingestion, retry, graceful degradation" \
  "Covers normalization helpers, the exponential-backoff retry loop, and FetchAll's graceful degradation."
run go test -v ./extractor/...
pause

# ---- 5. projector tests ------------------------------------------------------
section "Projector tests — dynamic output shaping" \
  "Covers fields / path / from / include_confidence / on_missing, plus strict config parsing."
run go test -v ./projector/...
pause

# ---- 6. integration test -----------------------------------------------------
section "Integration test — end-to-end determinism" \
  "Wires all three stages together in-process and asserts the final JSON shape."
run go test -v -run TestPipeline ./...
pause

# ---- 7. fuzz test ------------------------------------------------------------
if [[ "$FAST" -eq 0 ]]; then
  section "Fuzz test — merger never panics on arbitrary email input" \
    "Runs the email-resolution fuzzer briefly (5s). Skip with --fast."
  run go test ./merger/... -run=^$ -fuzz=FuzzResolveEmail -fuzztime=5s
  pause
fi

# ---- 8. run the pipeline -----------------------------------------------------
if [[ "$INTERACTIVE" -eq 1 ]]; then
  section "Run the pipeline end-to-end (interactive)" \
    "You'll be prompted for each ATS and GitHub field. Press ENTER to accept the default shown in [brackets], or type a new value to see the merger react to it."
  echo "${YELLOW}\$ go run . -interactive${RESET}"
  go run . -interactive
  pause
else
  section "Run the pipeline end-to-end" \
    "Structured logs (JSON, one per line) are interleaved with the final projected JSON document at the end."

  tmp_log="$(mktemp)"
  tmp_out="$(mktemp)"
  trap 'rm -f "$tmp_log" "$tmp_out"' EXIT

  echo "${YELLOW}\$ go run .${RESET}"
  # main.go writes both slog JSON lines and the final pretty-printed JSON to stdout.
  # We capture everything, then split: the final JSON document is the trailing
  # block that begins with a line equal to "{".
  go run . | tee "$tmp_out" >/dev/null

  echo
  echo "${BOLD}Structured logs (one JSON object per line):${RESET}"
  # Logs are single-line JSON objects starting with '{"time"'.
  grep '^{"time"' "$tmp_out" || echo "${DIM}(no structured log lines captured)${RESET}"

  echo
  echo "${BOLD}Final projected document:${RESET}"
  # Everything from the first line that is exactly "{" to EOF is the pretty JSON.
  awk '/^\{$/{flag=1} flag{print}' "$tmp_out" > "$tmp_log"
  if command -v jq >/dev/null 2>&1; then
    jq . < "$tmp_log"
  else
    cat "$tmp_log"
  fi
  pause
fi

# ---- outro -------------------------------------------------------------------
section "Wrap-up" "Where to go next."
cat <<EOF
${BOLD}Read the design docs:${RESET}
  • ${CYAN}docs/architecture.md${RESET} — package layout & data flow
  • ${CYAN}docs/decisions.md${RESET}   — the "Why" behind every design choice
  • ${CYAN}docs/testing.md${RESET}     — test strategy and how to run things

${BOLD}Try the conflict scenarios yourself:${RESET}
  • Edit the mocked ${CYAN}fetchRaw${RESET} methods in ${CYAN}extractor/extractor.go${RESET}
    to introduce new conflicts and re-run ${CYAN}go run .${RESET}.
  • Toggle ${CYAN}include_confidence${RESET} or change ${CYAN}fields / path / from / on_missing${RESET}
    in ${CYAN}samples/configs/default.json${RESET} or pass ${CYAN}-config samples/configs/custom.json${RESET}
    to reshape the output without touching pipeline code.

${GREEN}${BOLD}Demo complete.${RESET}
EOF
