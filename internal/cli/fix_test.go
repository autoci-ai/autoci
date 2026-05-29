package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/state"
)

func TestFixDryRunOverwritesStaleSuccessfulStateWhenNoPatchGenerated(t *testing.T) {
	for _, args := range [][]string{
		{"fix", "failure-theme-npm-install-failure", "--workflow", "pr.yml", "--dry-run"},
		{"fix", "--workflow", "pr.yml", "failure-theme-npm-install-failure", "--dry-run"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := setupNoInstallFixState(t)
			stale := map[string]any{
				"id":             "fix-npm-install-failure",
				"workflow":       "pr.yml",
				"confidence":     "medium",
				"patchGenerated": true,
				"filesChanged":   []string{".depot/workflows/pr.yml"},
				"changeSummary":  "Added retry logic to .depot/workflows/pr.yml.",
			}
			if err := state.WriteFix(dir, stale); err != nil {
				t.Fatal(err)
			}

			root := newRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs(append([]string{"--path", dir}, args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}

			record, raw := readFixRecord(t, dir)
			if record.Data.Workflow != "pr.yml" {
				t.Fatalf("workflow = %q", record.Data.Workflow)
			}
			if record.Data.Confidence != "low" {
				t.Fatalf("confidence = %q", record.Data.Confidence)
			}
			if record.Data.PatchGenerated {
				t.Fatalf("patchGenerated = true in %#v", record)
			}
			if record.Data.Reason != "AutoCI could not find a dependency install command in the targeted job." {
				t.Fatalf("reason = %q", record.Data.Reason)
			}
			if len(record.Data.FilesChanged) != 0 {
				t.Fatalf("filesChanged = %#v", record.Data.FilesChanged)
			}
			if !strings.Contains(raw, `"filesChanged": []`) {
				t.Fatalf("filesChanged was not persisted as an empty array:\n%s", raw)
			}
			encoded, _ := json.Marshal(record)
			for _, staleClaim := range []string{".depot/workflows/pr.yml", "retry logic"} {
				if strings.Contains(string(encoded), staleClaim) {
					t.Fatalf("stale claim %q remained in %s", staleClaim, encoded)
				}
			}
		})
	}
}

type fixRecordSnapshot struct {
	Data struct {
		Workflow       string   `json:"workflow"`
		Confidence     string   `json:"confidence"`
		PatchGenerated bool     `json:"patchGenerated"`
		Reason         string   `json:"reason"`
		FilesChanged   []string `json:"filesChanged"`
		ChangeSummary  string   `json:"changeSummary"`
	} `json:"data"`
}

func setupNoInstallFixState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  frontend-unit-test:
    steps:
      - run: yarn test
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{Workflow: "pr.yml", FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 3,
		Jobs:        []string{"frontend-unit-test"},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readFixRecord(t *testing.T, dir string) (fixRecordSnapshot, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".autoci", "fixes", "fix-npm-install-failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record fixRecordSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record, string(data)
}
