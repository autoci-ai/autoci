package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/state"
)

func TestFindingsFailuresOnly(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)

	output := runFindings(t, dir, "--failures")
	for _, want := range []string{
		"HIGH RELIABILITY",
		"failure-theme-image-pull-failure",
		"Source: failures",
		"Evidence: 3 occurrences across integration-test:matrix-03, integration-test:matrix-11.",
		"autoci research failure-theme-image-pull-failure",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "high-variance-integration-test-matrix-27") {
		t.Fatalf("profile finding leaked into failures-only output:\n%s", output)
	}
}

func TestFindingsProfileOnly(t *testing.T) {
	dir := t.TempDir()
	writeFindingsProfileState(t, dir)

	output := runFindings(t, dir, "--profile")
	for _, want := range []string{
		"HIGH RELIABILITY",
		"flaky-job-go-lint",
		"MEDIUM OPTIMIZATION",
		"high-variance-integration-test-matrix-27",
		"Source: profile",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestFindingsCombinedDefaultPrioritizesFailures(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsProfileState(t, dir)

	var items []findingJSON
	output := runFindingsJSON(t, dir, &items)
	if len(items) != 4 {
		t.Fatalf("items = %#v\n%s", items, output)
	}
	if items[0].ID != "failure-theme-image-pull-failure" || items[1].ID != "failure-theme-npm-install-failure" {
		t.Fatalf("unexpected order: %#v\n%s", items, output)
	}
}

func TestFindingsLimitAndNext(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsProfileState(t, dir)

	var limited []findingJSON
	runFindingsJSON(t, dir, &limited, "--limit", "2")
	if len(limited) != 2 {
		t.Fatalf("limited items = %#v", limited)
	}

	var next []findingJSON
	runFindingsJSON(t, dir, &next, "--next")
	if len(next) != 1 || next[0].ID != "failure-theme-image-pull-failure" {
		t.Fatalf("next = %#v", next)
	}
}

func TestFindingsCategoryFilters(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsProfileState(t, dir)

	var reliability []findingJSON
	runFindingsJSON(t, dir, &reliability, "--reliability")
	for _, item := range reliability {
		if item.Category != "reliability" {
			t.Fatalf("reliability output included %#v", item)
		}
	}

	var optimization []findingJSON
	runFindingsJSON(t, dir, &optimization, "--optimization")
	for _, item := range optimization {
		if item.Category != "optimization" {
			t.Fatalf("optimization output included %#v", item)
		}
	}
	if len(optimization) != 1 || optimization[0].ID != "high-variance-integration-test-matrix-27" {
		t.Fatalf("optimization = %#v", optimization)
	}
}

func TestFindingsJSONOutput(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)

	var items []findingJSON
	output := runFindingsJSON(t, dir, &items)
	if len(items) == 0 {
		t.Fatalf("empty json output: %s", output)
	}
	if items[0].NextCommand != "autoci research failure-theme-image-pull-failure" || items[0].Priority != "high" {
		t.Fatalf("json item = %#v", items[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "[") {
		t.Fatalf("json output had non-json prefix:\n%s", output)
	}
}

func TestFindingsEmptyStateDirectory(t *testing.T) {
	dir := t.TempDir()
	output := runFindings(t, dir)
	if !strings.Contains(output, "No cached AutoCI findings found.") {
		t.Fatalf("unexpected empty output:\n%s", output)
	}
	var items []findingJSON
	jsonOutput := runFindingsJSON(t, dir, &items)
	if len(items) != 0 || strings.TrimSpace(jsonOutput) != "[]" {
		t.Fatalf("json empty output = %#v\n%s", items, jsonOutput)
	}
}

type findingJSON struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Category    string `json:"category"`
	Priority    string `json:"priority"`
	NextCommand string `json:"nextCommand"`
}

func runFindings(t *testing.T, dir string, args ...string) string {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(append([]string{"--path", dir, "findings"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func runFindingsJSON(t *testing.T, dir string, target any, args ...string) string {
	t.Helper()
	allArgs := append([]string{"--format", "json"}, args...)
	output := runFindings(t, dir, allArgs...)
	if err := json.Unmarshal([]byte(output), target); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, output)
	}
	return output
}

func writeFindingsFailureState(t *testing.T, dir string) {
	t.Helper()
	analysis := failures.Analysis{
		Workflow:     "pr.yml",
		RunsAnalyzed: 10,
		FailedRuns:   4,
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-image-pull-failure",
			Signature:   "image pull failure",
			Occurrences: 3,
			Jobs:        []string{"integration-test:matrix-03", "integration-test:matrix-11"},
		}, {
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 2,
			Jobs:        []string{"frontend-unit-test"},
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
}

func writeFindingsProfileState(t *testing.T, dir string) {
	t.Helper()
	prof := &profile.Profile{Findings: []profile.Finding{{
		ID:       "flaky-job-go-lint",
		Severity: "high",
		Workflow: "pr.yml",
		Job:      "go-lint",
		Evidence: "Failure rate 19% across 10 runs.",
	}, {
		ID:       "high-variance-integration-test-matrix-27",
		Severity: "medium",
		Workflow: "pr.yml",
		Job:      "integration-test:matrix-27",
		Evidence: "Duration varies between 2m and 10m.",
	}}}
	if err := state.Write(dir, "profile", "pr.yml", prof); err != nil {
		t.Fatal(err)
	}
}
