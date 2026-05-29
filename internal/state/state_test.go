package state

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestFindItemReadsNormalizedFailureEvidence(t *testing.T) {
	dir := t.TempDir()
	failuresData := map[string]any{
		"findings": []map[string]any{{
			"id":          "failure-theme-image-pull-failure",
			"kind":        "failure",
			"signature":   "image pull failure",
			"occurrences": 6,
			"jobs":        []string{"go-lint"},
			"evidence": []map[string]any{{
				"registry":     "docker.io",
				"registryHost": "docker.io",
				"image":        "golang:1.24",
				"pullError":    "manifest unknown",
			}},
		}},
	}
	if err := Write(dir, "failures", "pr.yml", failuresData); err != nil {
		t.Fatal(err)
	}
	item, ok := FindItem(dir, "pr.yml", "failure-theme-image-pull-failure")
	if !ok {
		t.Fatal("expected stored item")
	}
	if item.Signature != "image pull failure" || item.Occurrences != 6 {
		t.Fatalf("item = %#v", item)
	}
	if len(item.Artifacts["images"]) != 1 || item.Artifacts["images"][0] != "golang:1.24" {
		t.Fatalf("artifacts = %#v", item.Artifacts)
	}
	if len(item.ExtractedItems) == 0 {
		t.Fatalf("extracted items = %#v", item.ExtractedItems)
	}
}

func TestUpdateTargetedResearchReadiness(t *testing.T) {
	dir := t.TempDir()
	evidence := map[string]any{
		"id":        "failure-theme-npm-install-failure",
		"readiness": "ready_for_fix",
		"fixNotes": map[string]any{
			"readiness": "ready_for_fix",
		},
	}
	if _, _, err := WriteTargetedResearch(dir, "failure-theme-npm-install-failure", evidence, []byte("# report\n")); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTargetedResearchReadiness(dir, "failure-theme-npm-install-failure", "validated"); err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Readiness string `json:"readiness"`
		FixNotes  struct {
			Readiness string `json:"readiness"`
		} `json:"fixNotes"`
	}
	loaded, err := ReadTargetedResearch[struct {
		Readiness string `json:"readiness"`
		FixNotes  struct {
			Readiness string `json:"readiness"`
		} `json:"fixNotes"`
	}](dir, "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	stored = *loaded
	if stored.Readiness != "validated" || stored.FixNotes.Readiness != "validated" {
		t.Fatalf("readiness not updated: %#v", stored)
	}
}

func TestWriteFixUsesSourceIDStablePath(t *testing.T) {
	for _, sourceField := range []string{"sourceId", "sourceItemId"} {
		t.Run(sourceField, func(t *testing.T) {
			dir := t.TempDir()
			record := map[string]any{
				"id":        "fix-image-pull-failure",
				sourceField: "failure-theme-image-pull-failure",
				"workflow":  "pr.yml",
			}
			if err := WriteFix(dir, record); err != nil {
				t.Fatal(err)
			}
			newPath := filepath.Join(dir, ".autoci", "fixes", "failure-theme-image-pull-failure.json")
			if _, err := os.Stat(newPath); err != nil {
				t.Fatalf("expected source-ID fix artifact at %s: %v", newPath, err)
			}
			oldPath := filepath.Join(dir, ".autoci", "fixes", "fix-image-pull-failure.json")
			if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
				t.Fatalf("legacy fix artifact should not exist at %s", oldPath)
			}
			snapshot, err := ReadFix(dir, "failure-theme-image-pull-failure")
			if err != nil {
				t.Fatal(err)
			}
			var stored map[string]any
			if !snapshotData(snapshot.Data, &stored) {
				t.Fatalf("could not decode fix snapshot: %#v", snapshot)
			}
			if stored["id"] != "fix-image-pull-failure" || stored[sourceField] != "failure-theme-image-pull-failure" {
				t.Fatalf("stored fix data changed: %#v", stored)
			}
		})
	}
}

func TestReadFixFallsBackToLegacyFixIDPath(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"id":       "fix-image-pull-failure",
		"workflow": "pr.yml",
	}
	if err := WriteFix(dir, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".autoci", "fixes", "fix-image-pull-failure.json")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadFix(dir, "failure-theme-image-pull-failure")
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ID string `json:"id"`
	}
	if !snapshotData(snapshot.Data, &stored) {
		t.Fatalf("could not decode fix snapshot: %#v", snapshot)
	}
	if stored.ID != "fix-image-pull-failure" {
		t.Fatalf("stored ID = %q", stored.ID)
	}
}

func TestWriteFixRemovesLegacyFixIDArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFix(dir, map[string]any{
		"id":       "fix-image-pull-failure",
		"workflow": "pr.yml",
	}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFix(dir, map[string]any{
		"id":           "fix-image-pull-failure",
		"sourceItemId": "failure-theme-image-pull-failure",
		"workflow":     "pr.yml",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".autoci", "fixes", "fix-image-pull-failure.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy artifact was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".autoci", "fixes", "failure-theme-image-pull-failure.json")); err != nil {
		t.Fatalf("source artifact missing: %v", err)
	}
}

func snapshotData(value any, target any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}
