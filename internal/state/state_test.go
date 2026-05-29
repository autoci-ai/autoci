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
}
