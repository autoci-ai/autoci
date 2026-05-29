package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanFindsDepotYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".depot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".depot", "ci.yml"), []byte("jobs: {}\n"), 0o644); err != nil {
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
