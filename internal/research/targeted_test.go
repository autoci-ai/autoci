package research

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/state"
)

func TestTargetedResearchImagePullWithoutImageIsEvidenceSafe(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("jobs:\n  go-lint:\n    steps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{
		Workflow: "pr.yml",
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-image-pull-failure",
			Signature:   "image pull failure",
			Occurrences: 6,
			Jobs:        []string{"go-lint"},
			Evidence: []failures.FailureEvidence{{
				RunID:      "run-1",
				Job:        "go-lint",
				LogExcerpt: "container setup failed before logs captured",
			}},
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}

	report, err := Targeted(dir, "", "failure-theme-image-pull-failure")
	if err != nil {
		t.Fatal(err)
	}
	if report.ID != "failure-theme-image-pull-failure" || report.Workflow != "pr.yml" {
		t.Fatalf("report target = %#v", report.CurrentFinding)
	}
	if report.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("readiness = %q", report.Readiness)
	}
	if len(report.Gaps) == 0 {
		t.Fatalf("expected structured evidence gaps")
	}
	if report.WorkflowContext == nil || !strings.Contains(report.WorkflowContext.Content, "go-lint") {
		t.Fatalf("workflow context = %#v", report.WorkflowContext)
	}
	markdown := string(WriteTargetMarkdown(report))
	for _, want := range []string{
		"# AutoCI Research Report: failure-theme-image-pull-failure",
		"Exact failing image reference not identified",
		"Generate an instrumentation patch",
		"`needs_more_evidence`",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("report missing %q:\n%s", want, markdown)
		}
	}
	for _, gap := range report.Gaps {
		if gap.Type == "" || gap.Message == "" {
			t.Fatalf("empty gap = %#v", gap)
		}
		if !strings.Contains(markdown, gap.Message) {
			t.Fatalf("markdown missing gap %q:\n%s", gap.Message, markdown)
		}
	}
	evidencePath, _, err := state.WriteTargetedResearch(dir, report.ID, report, []byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var stored TargetReport
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Gaps) != len(report.Gaps) {
		t.Fatalf("stored gaps = %#v, want %#v", stored.Gaps, report.Gaps)
	}
	for _, forbidden := range []string{"docker.io", "ghcr.io", "rate limit"} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("report hallucinated %q:\n%s", forbidden, markdown)
		}
	}
}

func TestTargetedResearchAttachesDependencyInstallCandidateStep(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  frontend-unit-test:
    steps:
      - name: Install dependencies
        run: corepack enable && yarn install --immutable
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{
		Workflow: "pr.yml",
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 3,
			Jobs:        []string{"frontend-unit-test"},
			Artifacts:   failures.FailureArtifacts{Packages: []string{"snyk"}},
			Evidence: []failures.FailureEvidence{{
				RunID:       "run-1",
				Job:         "frontend-unit-test",
				PackageName: "snyk",
				LogExcerpt:  "corepack enable && yarn install --immutable failed with ETIMEDOUT while installing snyk",
			}},
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}

	report, err := Targeted(dir, "", "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.CandidateSteps) != 1 {
		t.Fatalf("candidate steps = %#v", report.CandidateSteps)
	}
	if report.Readiness != lifecycle.ReadinessReadyForFix {
		t.Fatalf("readiness = %q", report.Readiness)
	}
	step := report.CandidateSteps[0]
	if step.Job != "frontend-unit-test" || step.Command != "corepack enable && yarn install --immutable" || step.Confidence < 0.90 {
		t.Fatalf("candidate step = %#v", step)
	}
	markdown := string(WriteTargetMarkdown(report))
	if !strings.Contains(markdown, "Candidate workflow steps") || !strings.Contains(markdown, "corepack enable && yarn install --immutable") {
		t.Fatalf("markdown missing candidate step:\n%s", markdown)
	}
	if !strings.Contains(markdown, "`ready_for_fix`") || !strings.Contains(markdown, `"readiness": "ready_for_fix"`) {
		t.Fatalf("markdown readiness does not match evidence model:\n%s", markdown)
	}
	evidencePath, _, err := state.WriteTargetedResearch(dir, report.ID, report, []byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var stored TargetReport
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Readiness != report.Readiness || stored.FixNotes.Readiness != report.Readiness {
		t.Fatalf("stored readiness mismatch: report=%q stored=%q notes=%q", report.Readiness, stored.Readiness, stored.FixNotes.Readiness)
	}
}

func TestTargetedResearchSnykIntegrityNeedsMoreEvidence(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  frontend-unit-test:
    steps:
      - name: Install dependencies
        run: corepack enable && yarn install --immutable
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{
		Workflow: "pr.yml",
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 3,
			Jobs:        []string{"frontend-unit-test"},
			Artifacts:   failures.FailureArtifacts{Packages: []string{"snyk"}},
			Evidence: []failures.FailureEvidence{{
				RunID:       "run-1",
				Job:         "frontend-unit-test",
				PackageName: "snyk",
				LogExcerpt:  "snyk@npm:1.1302.1 STDERR - actual: abc123",
			}, {
				RunID:       "run-1",
				Job:         "frontend-unit-test",
				PackageName: "snyk",
				LogExcerpt:  "snyk@npm:1.1302.1 STDERR - expected: def456",
			}},
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}

	report, err := Targeted(dir, "", "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	if report.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("readiness = %q", report.Readiness)
	}
	for _, want := range []string{"missing_root_cause_disambiguation", "missing_safe_patch_strategy"} {
		if !hasGapType(report.Gaps, want) {
			t.Fatalf("missing gap %q in %#v", want, report.Gaps)
		}
	}
	markdown := string(WriteTargetMarkdown(report))
	for _, want := range []string{
		"`needs_more_evidence`",
		"Generate an instrumentation patch",
		"Snyk install/download diagnostics",
		"cannot select a safe surgical workflow patch",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestTargetedResearchCorrelatesFlakyJobWithFailureTheme(t *testing.T) {
	dir := t.TempDir()
	analysis := failures.Analysis{
		Workflow: "pr.yml",
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 2,
			Jobs:        []string{"frontend-unit-test"},
		}},
	}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	prof := &profile.Profile{Findings: []profile.Finding{{
		ID:       "flaky-job-frontend-unit-test",
		Title:    "Flaky job",
		Severity: "high",
		Workflow: "pr.yml",
		Job:      "frontend-unit-test",
		Evidence: "Failure rate 20% across 10 runs.",
	}}}
	if err := state.Write(dir, "profile", "pr.yml", prof); err != nil {
		t.Fatal(err)
	}

	report, err := Targeted(dir, "", "flaky-job-frontend-unit-test")
	if err != nil {
		t.Fatal(err)
	}
	if report.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("readiness = %q", report.Readiness)
	}
	if len(report.RelatedFindings) != 1 || report.RelatedFindings[0].ID != "failure-theme-npm-install-failure" {
		t.Fatalf("related findings = %#v", report.RelatedFindings)
	}
	if len(report.RelatedFailureThemes) != 1 || len(report.CorrelatedFailureThemes) != 1 {
		t.Fatalf("related themes = %#v correlated = %#v", report.RelatedFailureThemes, report.CorrelatedFailureThemes)
	}
	if len(report.RecommendedInvestigation) == 0 || !strings.Contains(report.RecommendedInvestigation[0], "autoci research failure-theme-npm-install-failure") {
		t.Fatalf("next steps = %#v", report.RecommendedInvestigation)
	}
	if !hasGapType(report.Gaps, "related_failure_theme") {
		t.Fatalf("gaps = %#v", report.Gaps)
	}
	markdown := string(WriteTargetMarkdown(report))
	for _, want := range []string{
		"This flaky job overlaps with failure-theme-npm-install-failure",
		"Investigate the specific failure theme before treating this as generic flakiness",
		"autoci research failure-theme-npm-install-failure",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func hasGapType(gaps []EvidenceGap, gapType string) bool {
	for _, gap := range gaps {
		if gap.Type == gapType {
			return true
		}
	}
	return false
}
