package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
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

func TestFindingsDeduplicatesByStableID(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureAggregationState(t, dir)
	writeFindingsRepeatedFailureProfileState(t, dir)

	var items []findingJSON
	runFindingsJSON(t, dir, &items)
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	item := items[0]
	if item.ID != "repeated-failures-workflow" || item.Source != "failures" {
		t.Fatalf("deduped item = %#v", item)
	}
	if !strings.Contains(item.Evidence, "4 repeated failures reported by gate.") {
		t.Fatalf("deduped item did not prefer failure evidence: %#v", item)
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
	if items[0].Status != "new" || items[0].NextAction != "research" {
		t.Fatalf("json lifecycle fields = %#v", items[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "[") {
		t.Fatalf("json output had non-json prefix:\n%s", output)
	}
}

func TestFindingsStateNewWhenNoResearchExists(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)

	var items []findingJSON
	runFindingsJSON(t, dir, &items, "--new")
	item := findFindingJSON(t, items, "failure-theme-image-pull-failure")
	if item.Status != "new" || item.NextAction != "research" || item.NextCommand != "autoci research failure-theme-image-pull-failure" {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsStateResearched(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsResearchState(t, dir, "failure-theme-image-pull-failure", "", nil, nil)

	var items []findingJSON
	runFindingsJSON(t, dir, &items)
	item := findFindingJSON(t, items, "failure-theme-image-pull-failure")
	if item.Status != "researched" || item.NextAction != "review_research" {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsStateNotReadyUsesGaps(t *testing.T) {
	dir := t.TempDir()
	writeFindingsProfileState(t, dir)
	writeFindingsResearchState(t, dir, "high-variance-integration-test-matrix-27", lifecycle.ReadinessNotReady, []map[string]string{{
		"type":    "missing_workflow_step_match",
		"message": "No workflow step matched the finding with enough confidence",
	}, {
		"type":    "missing_logs",
		"message": "Representative logs are missing",
	}}, []string{"inspect slow and fast runs for integration-test:matrix-27"})

	output := runFindings(t, dir, "--optimization")
	for _, want := range []string{
		"Status: needs_more_evidence",
		"Gaps:",
		"- No workflow step matched the finding with enough confidence",
		"- Representative logs are missing",
		"inspect slow and fast runs for integration-test:matrix-27",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	var items []findingJSON
	runFindingsJSON(t, dir, &items, "--optimization")
	item := findFindingJSON(t, items, "high-variance-integration-test-matrix-27")
	if item.Status != "needs_more_evidence" || item.NextAction != "inspect_evidence_gaps" || len(item.Gaps) != 2 {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsStateReadyForFix(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsResearchState(t, dir, "failure-theme-npm-install-failure", lifecycle.ReadinessReadyForFix, nil, nil)

	var items []findingJSON
	runFindingsJSON(t, dir, &items)
	item := findFindingJSON(t, items, "failure-theme-npm-install-failure")
	if item.Status != "ready_for_fix" || item.NextAction != "fix" || item.NextCommand != "autoci fix failure-theme-npm-install-failure" {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsStateInstrumentationAppliedAwaitingValidation(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsResearchState(t, dir, "failure-theme-image-pull-failure", lifecycle.ReadinessNeedsMoreEvidence, nil, nil)
	writeFindingsFixState(t, dir, "failure-theme-image-pull-failure", "instrumentation", true, true)

	var items []findingJSON
	runFindingsJSON(t, dir, &items, "--awaiting-validation")
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	item := items[0]
	if item.ID != "failure-theme-image-pull-failure" || item.Status != "awaiting_validation" || item.NextAction != "wait_for_runs" {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsStateRootCauseFixAppliedAwaitingValidation(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsResearchState(t, dir, "failure-theme-npm-install-failure", lifecycle.ReadinessReadyForFix, nil, nil)
	writeFindingsFixState(t, dir, "failure-theme-npm-install-failure", "root_cause", true, true)

	var items []findingJSON
	runFindingsJSON(t, dir, &items, "--awaiting-validation")
	item := findFindingJSON(t, items, "failure-theme-npm-install-failure")
	if item.Status != "awaiting_validation" || item.NextCommand != "wait for future CI runs to collect diagnostics" {
		t.Fatalf("item = %#v", item)
	}
}

func TestFindingsActiveFilterExcludesAwaitingValidation(t *testing.T) {
	dir := t.TempDir()
	writeFindingsFailureState(t, dir)
	writeFindingsFixState(t, dir, "failure-theme-image-pull-failure", "instrumentation", true, true)

	var items []findingJSON
	runFindingsJSON(t, dir, &items, "--active")
	if containsFindingJSON(items, "failure-theme-image-pull-failure") {
		t.Fatalf("awaiting validation item included in active output: %#v", items)
	}
	if !containsFindingJSON(items, "failure-theme-npm-install-failure") {
		t.Fatalf("new active item missing: %#v", items)
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
	Evidence    string `json:"evidence"`
	NextCommand string `json:"nextCommand"`
	NextAction  string `json:"nextAction"`
	Status      string `json:"status"`
	Gaps        []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"gaps"`
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

func writeFindingsFailureAggregationState(t *testing.T, dir string) {
	t.Helper()
	analysis := failures.Analysis{
		Workflow:     "pr.yml",
		RunsAnalyzed: 10,
		FailedRuns:   4,
		AggregationJobs: []failures.AggregationJob{{
			Job:         "gate",
			Occurrences: 4,
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
}

func writeFindingsRepeatedFailureProfileState(t *testing.T, dir string) {
	t.Helper()
	prof := &profile.Profile{Findings: []profile.Finding{{
		ID:       "repeated-failures-workflow",
		Severity: "high",
		Workflow: "pr.yml",
		Evidence: "Failure rate 40% across 10 runs.",
	}}}
	if err := state.Write(dir, "profile", "pr.yml", prof); err != nil {
		t.Fatal(err)
	}
}

func writeFindingsResearchState(t *testing.T, dir, id string, readiness lifecycle.Readiness, gaps []map[string]string, nextSteps []string) {
	t.Helper()
	evidence := map[string]any{
		"id":                       id,
		"readiness":                readiness,
		"gaps":                     gaps,
		"recommendedInvestigation": nextSteps,
	}
	if _, _, err := state.WriteTargetedResearch(dir, id, evidence, []byte("# report\n")); err != nil {
		t.Fatal(err)
	}
}

func writeFindingsFixState(t *testing.T, dir, sourceID, fixType string, patchGenerated, patchApplied bool) {
	t.Helper()
	if err := state.WriteFix(dir, map[string]any{
		"id":             "fix-" + strings.TrimPrefix(sourceID, "failure-theme-"),
		"sourceItemId":   sourceID,
		"workflow":       "pr.yml",
		"fixType":        fixType,
		"patchGenerated": patchGenerated,
		"patchApplied":   patchApplied,
	}); err != nil {
		t.Fatal(err)
	}
}

func findFindingJSON(t *testing.T, items []findingJSON, id string) findingJSON {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("finding %q not found in %#v", id, items)
	return findingJSON{}
}

func containsFindingJSON(items []findingJSON, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
