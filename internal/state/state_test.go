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

func TestWriteFixPreservesAppliedRecordOnIdempotentNoOp(t *testing.T) {
	dir := t.TempDir()
	applied := map[string]any{
		"id":             "fix-image-pull-failure",
		"sourceItemId":   "failure-theme-image-pull-failure",
		"workflow":       "pr.yml",
		"fixType":        "instrumentation",
		"patchGenerated": true,
		"patchApplied":   true,
		"reason":         "Readiness is needs_more_evidence; generated instrumentation from gaps: missing_image.",
	}
	if err := WriteFix(dir, applied); err != nil {
		t.Fatal(err)
	}
	noop := map[string]any{
		"id":             "fix-image-pull-failure",
		"sourceItemId":   "failure-theme-image-pull-failure",
		"workflow":       "pr.yml",
		"fixType":        "instrumentation",
		"patchGenerated": false,
		"patchApplied":   false,
		"reason":         "The selected workflow already contains the AutoCI container diagnostics step.",
	}
	if err := WriteFix(dir, noop); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadFix(dir, "failure-theme-image-pull-failure")
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ID             string `json:"id"`
		FixType        string `json:"fixType"`
		PatchGenerated bool   `json:"patchGenerated"`
		PatchApplied   bool   `json:"patchApplied"`
		Reason         string `json:"reason"`
		LastRun        struct {
			PatchGenerated bool   `json:"patchGenerated"`
			PatchApplied   bool   `json:"patchApplied"`
			Reason         string `json:"reason"`
			Idempotent     bool   `json:"idempotent"`
		} `json:"lastRun"`
	}
	if !snapshotData(snapshot.Data, &stored) {
		t.Fatalf("could not decode fix snapshot: %#v", snapshot)
	}
	if stored.ID != "fix-image-pull-failure" || stored.FixType != "instrumentation" {
		t.Fatalf("stored identity changed: %#v", stored)
	}
	if !stored.PatchGenerated || !stored.PatchApplied {
		t.Fatalf("applied state was overwritten: %#v", stored)
	}
	if stored.Reason != applied["reason"] {
		t.Fatalf("top-level reason was overwritten: %#v", stored)
	}
	if stored.LastRun.PatchGenerated || stored.LastRun.PatchApplied || !stored.LastRun.Idempotent {
		t.Fatalf("lastRun status = %#v", stored.LastRun)
	}
	if stored.LastRun.Reason != noop["reason"] {
		t.Fatalf("lastRun reason = %q", stored.LastRun.Reason)
	}
}

func TestWriteFixPersistsChangeHistoryForOneFinding(t *testing.T) {
	dir := t.TempDir()
	instrumentation := map[string]any{
		"id":             "fix-npm-install-failure",
		"sourceItemId":   "failure-theme-npm-install-failure",
		"workflow":       "pr.yml",
		"fixType":        "instrumentation",
		"branch":         "autoci/instrument-npm-install-failure",
		"changeId":       "instrumentation-001",
		"patchGenerated": true,
		"patchApplied":   true,
		"change": map[string]any{
			"id":         "instrumentation-001",
			"findingId":  "failure-theme-npm-install-failure",
			"changeType": "instrumentation",
			"branch":     "autoci/instrument-npm-install-failure",
		},
	}
	if err := WriteFix(dir, instrumentation); err != nil {
		t.Fatal(err)
	}
	update := map[string]any{
		"id":             "fix-npm-install-failure",
		"sourceItemId":   "failure-theme-npm-install-failure",
		"workflow":       "pr.yml",
		"fixType":        "instrumentation_update",
		"branch":         "autoci/update-instrumentation-npm-install-failure",
		"changeId":       "instrumentation-update-001",
		"patchGenerated": true,
		"patchApplied":   true,
		"change": map[string]any{
			"id":         "instrumentation-update-001",
			"findingId":  "failure-theme-npm-install-failure",
			"changeType": "instrumentation_update",
			"branch":     "autoci/update-instrumentation-npm-install-failure",
		},
	}
	if err := WriteFix(dir, update); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadFix(dir, "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		SourceItemID string `json:"sourceItemId"`
		Branch       string `json:"branch"`
		Changes      []struct {
			ID         string `json:"id"`
			FindingID  string `json:"findingId"`
			ChangeType string `json:"changeType"`
			Branch     string `json:"branch"`
		} `json:"changes"`
	}
	if !snapshotData(snapshot.Data, &stored) {
		t.Fatalf("could not decode fix snapshot: %#v", snapshot)
	}
	if stored.SourceItemID != "failure-theme-npm-install-failure" {
		t.Fatalf("finding id changed: %#v", stored)
	}
	if stored.Branch != "autoci/update-instrumentation-npm-install-failure" {
		t.Fatalf("latest branch = %q", stored.Branch)
	}
	if len(stored.Changes) != 2 {
		t.Fatalf("changes = %#v", stored.Changes)
	}
	if stored.Changes[0].Branch == stored.Changes[1].Branch {
		t.Fatalf("branch collision in changes: %#v", stored.Changes)
	}
	for _, change := range stored.Changes {
		if change.FindingID != "failure-theme-npm-install-failure" {
			t.Fatalf("change not tied to stable finding id: %#v", change)
		}
	}
}

func TestWriteFixDoesNotDuplicateChangeHistoryOnRepeatedRuns(t *testing.T) {
	dir := t.TempDir()
	record := map[string]any{
		"id":             "fix-image-pull-failure",
		"sourceItemId":   "failure-theme-image-pull-failure",
		"workflow":       "pr.yml",
		"fixType":        "instrumentation",
		"branch":         "autoci/instrument-image-pull-failure",
		"changeId":       "instrumentation-001",
		"patchGenerated": true,
		"patchApplied":   false,
		"change": map[string]any{
			"id":         "instrumentation-001",
			"findingId":  "failure-theme-image-pull-failure",
			"changeType": "instrumentation",
			"branch":     "autoci/instrument-image-pull-failure",
		},
	}
	if err := WriteFix(dir, record); err != nil {
		t.Fatal(err)
	}
	if err := WriteFix(dir, record); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadFix(dir, "failure-theme-image-pull-failure")
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Changes []map[string]any `json:"changes"`
	}
	if !snapshotData(snapshot.Data, &stored) {
		t.Fatalf("could not decode fix snapshot: %#v", snapshot)
	}
	if len(stored.Changes) != 1 {
		t.Fatalf("changes duplicated: %#v", stored.Changes)
	}
}

func snapshotData(value any, target any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}
