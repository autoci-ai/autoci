package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
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
