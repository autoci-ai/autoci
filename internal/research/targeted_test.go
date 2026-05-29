package research

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
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
	if report.WorkflowContext == nil || !strings.Contains(report.WorkflowContext.Content, "go-lint") {
		t.Fatalf("workflow context = %#v", report.WorkflowContext)
	}
	markdown := string(WriteTargetMarkdown(report))
	for _, want := range []string{"# AutoCI Research Report: failure-theme-image-pull-failure", "The exact failed image reference is not present", "`needs_more_evidence`"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("report missing %q:\n%s", want, markdown)
		}
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
