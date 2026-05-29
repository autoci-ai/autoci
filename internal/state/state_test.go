package state

import (
	"testing"
)

func TestWriteAndFindItem(t *testing.T) {
	dir := t.TempDir()
	data := map[string]any{
		"failureThemes": []map[string]any{{
			"id":        "failure-theme-image-pull-failure",
			"signature": "image pull failure",
			"jobs":      []string{"go-lint"},
			"artifacts": map[string]any{
				"images": []string{"docker://rhysd/actionlint:latest"},
			},
		}},
	}
	if err := Write(dir, "failures", "pr.yml", data); err != nil {
		t.Fatal(err)
	}
	item, ok := FindItem(dir, "pr.yml", "failure-theme-image-pull-failure")
	if !ok {
		t.Fatal("expected stored item")
	}
	if item.Evidence != "image pull failure" {
		t.Fatalf("evidence = %q", item.Evidence)
	}
	if len(item.Jobs) != 1 || item.Jobs[0] != "go-lint" {
		t.Fatalf("jobs = %#v", item.Jobs)
	}
	if len(item.Artifacts["images"]) != 1 {
		t.Fatalf("artifacts = %#v", item.Artifacts)
	}
}

func TestFindItemMergesFailureArtifactsAfterResearchMatch(t *testing.T) {
	dir := t.TempDir()
	researchData := map[string]any{
		"opportunities": []map[string]any{{
			"id":       "reliability-image-pull-failure",
			"evidence": "6 occurrences across multiple jobs.",
		}},
	}
	failuresData := map[string]any{
		"failureThemes": []map[string]any{{
			"id":          "failure-theme-image-pull-failure",
			"signature":   "image pull failure",
			"occurrences": 6,
			"jobs":        []string{"go-lint"},
			"artifacts": map[string]any{
				"images": []string{"docker://rhysd/actionlint:latest"},
			},
		}},
	}
	if err := Write(dir, "research", "pr.yml", researchData); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, "failures", "pr.yml", failuresData); err != nil {
		t.Fatal(err)
	}
	item, ok := FindItem(dir, "pr.yml", "reliability-image-pull-failure")
	if !ok {
		t.Fatal("expected stored item")
	}
	if item.Evidence != "6 occurrences across multiple jobs." {
		t.Fatalf("evidence = %q", item.Evidence)
	}
	if item.Occurrences != 6 || len(item.Artifacts["images"]) != 1 || item.Jobs[0] != "go-lint" {
		t.Fatalf("merged item = %#v", item)
	}
}
