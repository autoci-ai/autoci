package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFindsDepotYAML(t *testing.T) {
	dir := t.TempDir()
	workflowDir := filepath.Join(dir, ".depot", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "ci.yml"), []byte("jobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.yml"), []byte("jobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workflows, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflows))
	}
}

func TestSelectWorkflowRequiresExplicitChoiceForMultipleWorkflows(t *testing.T) {
	dir := t.TempDir()
	workflows := []Workflow{
		{Path: filepath.Join(dir, ".depot", "workflows", "pr.yml")},
		{Path: filepath.Join(dir, ".depot", "workflows", "staging.yml")},
	}

	if _, err := SelectWorkflow(dir, workflows, "", "analyze"); err == nil {
		t.Fatal("expected error for multiple workflows without explicit selection")
	}
}

func TestSelectWorkflowMatchesBasenameAndRelativePath(t *testing.T) {
	dir := t.TempDir()
	workflow := Workflow{Path: filepath.Join(dir, ".depot", "workflows", "pr.yml")}
	workflows := []Workflow{workflow}

	selected, err := SelectWorkflow(dir, workflows, "pr.yml", "analyze")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Path != workflow.Path {
		t.Fatalf("expected basename match")
	}

	selected, err = SelectWorkflow(dir, workflows, ".depot/workflows/pr.yml", "profile")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Path != workflow.Path {
		t.Fatalf("expected relative path match")
	}
}
