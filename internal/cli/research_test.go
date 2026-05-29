package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/state"
)

func TestResearchByIDWritesPersistentReport(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("jobs:\n  go-lint:\n    steps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{Workflow: "pr.yml", FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-image-pull-failure",
		Signature:   "image pull failure",
		Occurrences: 2,
		Jobs:        []string{"go-lint"},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--path", dir, "research", "failure-theme-image-pull-failure"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, ".autoci", "research", "failure-theme-image-pull-failure", "research.md")
	evidencePath := filepath.Join(dir, ".autoci", "research", "failure-theme-image-pull-failure", "evidence.json")
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(evidencePath); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reportData), "# AutoCI Research Report: failure-theme-image-pull-failure") {
		t.Fatalf("unexpected report:\n%s", reportData)
	}
	if !strings.Contains(out.String(), "Wrote targeted research for failure-theme-image-pull-failure") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}
