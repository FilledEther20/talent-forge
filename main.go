package main

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"talent-forge/extractor"
	"talent-forge/merger"
	"talent-forge/projector"
)

//go:embed samples/configs/default.json
var defaultConfigJSON []byte

func main() {
	atsFile := flag.String("ats", "", "path to ATS JSON file (default: built-in sample)")
	githubFile := flag.String("github", "", "path to GitHub JSON file (default: built-in sample)")
	notesFile := flag.String("notes", "", "path to recruiter notes .txt file (default: built-in sample)")
	configFile := flag.String("config", "", "path to projector config JSON (default: embedded default.json)")
	interactive := flag.Bool("interactive", false, "prompt for ATS and GitHub values from stdin")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var atsStub *extractor.ATSStub
	var ghStub *extractor.GitHubStub
	if *interactive {
		atsStub, ghStub = promptForStubs(os.Stdin, os.Stderr)
	}

	extractors := []extractor.Extractor{
		&extractor.ATSExtractor{CandidateID: "cand-123", FailureRate: 0, FilePath: *atsFile, Stub: atsStub},
		&extractor.GitHubExtractor{Username: "janedoe", FailureRate: 0, FilePath: *githubFile, Stub: ghStub},
		&extractor.NotesExtractor{FailureRate: 0, FilePath: *notesFile},
	}
	normalized := extractor.FetchAll(ctx, extractors)

	correlationID := "req-" + fmt.Sprint(time.Now().UnixNano())
	canonicalLogger := merger.NewCorrelationLogger(correlationID)
	canonical := merger.Merge(canonicalLogger, normalized)

	configBytes := defaultConfigJSON
	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			slog.Error("failed to read config file", "path", *configFile, "error", err)
			os.Exit(1)
		}
		configBytes = data
	}

	cfg, err := projector.ParseConfig(configBytes)
	if err != nil {
		slog.Error("failed to parse runtime config", "error", err)
		os.Exit(1)
	}

	output, err := projector.Project(canonical, cfg)
	if err != nil {
		slog.Error("failed to project canonical profile", "error", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}

func promptForStubs(in io.Reader, prompts io.Writer) (*extractor.ATSStub, *extractor.GitHubStub) {
	reader := bufio.NewReader(in)

	fmt.Fprintln(prompts)
	fmt.Fprintln(prompts, "─── ATS source ────────────────────────────────────────")
	fmt.Fprintln(prompts, "Press ENTER to accept the default in [brackets].")
	fmt.Fprintln(prompts)
	ats := &extractor.ATSStub{
		Name:   ask(reader, prompts, "  Name  ", "  Jane   Doe "),
		Email:  ask(reader, prompts, "  Email ", " JANE.DOE@badformat "),
		Phone:  ask(reader, prompts, "  Phone ", "555-123-4567"),
		Skills: askList(reader, prompts, "  Skills (comma-separated)", []string{"Go", "  go ", "SQL"}),
	}

	fmt.Fprintln(prompts)
	fmt.Fprintln(prompts, "─── GitHub source ─────────────────────────────────────")
	fmt.Fprintln(prompts, "Press ENTER to accept the default in [brackets].")
	fmt.Fprintln(prompts)
	gh := &extractor.GitHubStub{
		Name:   ask(reader, prompts, "  Name  ", " Jane Doe"),
		Email:  ask(reader, prompts, "  Email ", "jane.doe@example.com"),
		Skills: askList(reader, prompts, "  Skills (comma-separated)", []string{"Go", "Python", "TypeScript"}),
	}

	fmt.Fprintln(prompts)
	fmt.Fprintln(prompts, "─── Running pipeline ──────────────────────────────────")
	fmt.Fprintln(prompts)
	return ats, gh
}

func ask(r *bufio.Reader, w io.Writer, label, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", label, def)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return def
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return def
	}
	return line
}

func askList(r *bufio.Reader, w io.Writer, label string, def []string) []string {
	defStr := strings.Join(def, ", ")
	raw := ask(r, w, label, defStr)
	if raw == defStr {
		return def
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
