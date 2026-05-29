package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/research"
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

func TestFixRefusesGenericRetryForSnykIntegrityResearch(t *testing.T) {
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
		Artifacts:   failures.FailureArtifacts{Packages: []string{"snyk"}},
		Evidence: []failures.FailureEvidence{{
			Job:         "frontend-unit-test",
			PackageName: "snyk",
			LogExcerpt:  "snyk@npm:1.1302.1 STDERR - actual: abc123",
		}, {
			Job:         "frontend-unit-test",
			PackageName: "snyk",
			LogExcerpt:  "snyk@npm:1.1302.1 STDERR - expected: def456",
		}},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	target, err := research.Targeted(dir, "", "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.WriteTargetedResearch(dir, target.ID, target, research.WriteTargetMarkdown(target)); err != nil {
		t.Fatal(err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--path", dir, "fix", "failure-theme-npm-install-failure", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, want := range []string{
		"Patch: not generated",
		"Confidence: low",
		"Evidence suggests a Snyk binary download problem",
		"generic yarn install retry is not a proven mitigation",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Patch: generated") || strings.Contains(output, "+        run:") {
		t.Fatalf("Snyk integrity evidence generated a generic retry:\n%s", output)
	}
}

func TestFixGeneratesReadableRetryForTransientInstallEvidence(t *testing.T) {
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
		Evidence: []failures.FailureEvidence{{
			Job:          "frontend-unit-test",
			LogExcerpt:   "corepack enable && yarn install --immutable failed with ETIMEDOUT while accessing registry.yarnpkg.com",
			InstallError: "yarn install failed with ETIMEDOUT",
		}},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	target, err := research.Targeted(dir, "", "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.WriteTargetedResearch(dir, target.ID, target, research.WriteTargetMarkdown(target)); err != nil {
		t.Fatal(err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--path", dir, "fix", "failure-theme-npm-install-failure", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, want := range []string{
		"Patch: generated",
		"Target step: frontend-unit-test `corepack enable && yarn install --immutable`",
		"+        run: |",
		"+          corepack enable",
		"+          n=0",
		"+          until yarn install --immutable; do",
		"+            sleep $((n * 5))",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestFixRejectsResearchEvidenceWithMissingReadiness(t *testing.T) {
	dir := setupNoInstallFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-npm-install-failure", map[string]any{
		"id":             "failure-theme-npm-install-failure",
		"workflow":       "pr.yml",
		"jobs":           []string{"frontend-unit-test"},
		"candidateSteps": []map[string]any{{"workflow": "pr.yml", "job": "frontend-unit-test", "command": "yarn install --immutable", "confidence": 0.9}},
	})

	err := runFix(t, dir)
	if err == nil || !strings.Contains(err.Error(), "is missing readiness") {
		t.Fatalf("expected missing readiness error, got %v", err)
	}
}

func TestFixRejectsResearchEvidenceWithInvalidReadiness(t *testing.T) {
	dir := setupNoInstallFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-npm-install-failure", map[string]any{
		"id":        "failure-theme-npm-install-failure",
		"workflow":  "pr.yml",
		"readiness": "maybe_ready",
	})

	err := runFix(t, dir)
	if err == nil || !strings.Contains(err.Error(), "invalid readiness") {
		t.Fatalf("expected invalid readiness error, got %v", err)
	}
}

func TestFixRejectsResearchEvidenceThatIsNotReady(t *testing.T) {
	dir := setupNoInstallFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-npm-install-failure", map[string]any{
		"id":        "failure-theme-npm-install-failure",
		"workflow":  "pr.yml",
		"readiness": lifecycle.ReadinessNeedsMoreEvidence,
		"gaps": []map[string]any{
			{"type": "missing_package", "message": "Exact package or dependency constraint not identified"},
			{"type": "missing_root_cause", "message": "Cached logs do not include a resolver, peer dependency, lockfile, or integrity marker"},
		},
	})

	err := runFix(t, dir)
	if err == nil || !strings.Contains(err.Error(), "Readiness: needs_more_evidence") || !strings.Contains(err.Error(), "Exact package or dependency constraint not identified") {
		t.Fatalf("expected not-ready error, got %v", err)
	}
}

func TestFixJSONReadyForFixEmitsJSONOnly(t *testing.T) {
	dir := setupTransientInstallFixState(t)
	output, err := runFixOutput(t, dir, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		SourceID       string              `json:"sourceId"`
		Workflow       string              `json:"workflow"`
		Readiness      lifecycle.Readiness `json:"readiness"`
		PatchGenerated bool                `json:"patchGenerated"`
		PatchApplied   bool                `json:"patchApplied"`
		Reason         string              `json:"reason"`
		Validation     []string            `json:"validation"`
	}
	assertJSONOnlyAndJQ(t, output, &plan)
	if plan.SourceID != "failure-theme-npm-install-failure" || plan.Workflow != "pr.yml" || plan.Readiness != lifecycle.ReadinessReadyForFix {
		t.Fatalf("unexpected json plan: %#v\n%s", plan, output)
	}
	if !plan.PatchGenerated || plan.PatchApplied {
		t.Fatalf("unexpected patch status: %#v", plan)
	}
}

func TestFixJSONNeedsMoreEvidenceEmitsJSONOnly(t *testing.T) {
	dir := setupNoInstallFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-image-pull-failure", map[string]any{
		"id":        "failure-theme-image-pull-failure",
		"workflow":  "pr.yml",
		"readiness": lifecycle.ReadinessNeedsMoreEvidence,
		"gaps": []map[string]any{
			{"type": "missing_image", "message": "Exact failing image reference not identified"},
		},
	})

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--path", dir, "fix", "failure-theme-image-pull-failure", "--workflow", "pr.yml", "--dry-run", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan struct {
		SourceID       string              `json:"sourceId"`
		Workflow       string              `json:"workflow"`
		Readiness      lifecycle.Readiness `json:"readiness"`
		PatchGenerated bool                `json:"patchGenerated"`
		PatchApplied   bool                `json:"patchApplied"`
		Reason         string              `json:"reason"`
		Gaps           []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"gaps"`
		PatchScope struct {
			JobsTouched  []string `json:"jobsTouched"`
			StepsTouched []string `json:"stepsTouched"`
		} `json:"patchScope"`
		Validation []string `json:"validation"`
	}
	assertJSONOnlyAndJQ(t, out.String(), &plan)
	if plan.SourceID != "failure-theme-image-pull-failure" || plan.Workflow != "pr.yml" || plan.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("unexpected refusal json: %#v\n%s", plan, out.String())
	}
	if plan.PatchGenerated || plan.PatchApplied || plan.Reason != "Cannot generate fix because required evidence is missing." {
		t.Fatalf("unexpected refusal status: %#v", plan)
	}
	if len(plan.Gaps) != 1 || plan.Gaps[0].Message != "Exact failing image reference not identified" {
		t.Fatalf("gaps = %#v", plan.Gaps)
	}
	if len(plan.Validation) != 0 {
		t.Fatalf("validation = %#v", plan.Validation)
	}
	if plan.PatchScope.JobsTouched == nil || len(plan.PatchScope.JobsTouched) != 0 {
		t.Fatalf("jobsTouched = %#v", plan.PatchScope.JobsTouched)
	}
	if plan.PatchScope.StepsTouched == nil || len(plan.PatchScope.StepsTouched) != 0 {
		t.Fatalf("stepsTouched = %#v", plan.PatchScope.StepsTouched)
	}
	if strings.Contains(out.String(), `"jobsTouched": null`) || strings.Contains(out.String(), `"stepsTouched": null`) {
		t.Fatalf("nullable patchScope arrays in output:\n%s", out.String())
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

func runFix(t *testing.T, dir string) error {
	t.Helper()
	_, err := runFixOutput(t, dir)
	return err
}

func runFixOutput(t *testing.T, dir string, extraArgs ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	args := []string{"--path", dir, "fix", "failure-theme-npm-install-failure", "--dry-run"}
	args = append(args, extraArgs...)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func writeRawResearchEvidence(t *testing.T, dir, id string, data map[string]any) {
	t.Helper()
	targetDir := filepath.Join(dir, ".autoci", "research", id)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "evidence.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
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

func setupTransientInstallFixState(t *testing.T) string {
	t.Helper()
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
		Evidence: []failures.FailureEvidence{{
			Job:          "frontend-unit-test",
			LogExcerpt:   "corepack enable && yarn install --immutable failed with ETIMEDOUT while accessing registry.yarnpkg.com",
			InstallError: "yarn install failed with ETIMEDOUT",
		}},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	target, err := research.Targeted(dir, "", "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.WriteTargetedResearch(dir, target.ID, target, research.WriteTargetMarkdown(target)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertJSONOnlyAndJQ(t *testing.T, output string, target any) {
	t.Helper()
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("output is not JSON-only:\n%s", output)
	}
	if err := json.Unmarshal([]byte(trimmed), target); err != nil {
		t.Fatalf("output did not parse as JSON: %v\n%s", err, output)
	}
	if jq, err := exec.LookPath("jq"); err == nil {
		cmd := exec.Command(jq, ".")
		cmd.Stdin = strings.NewReader(output)
		if combined, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("output did not parse with jq: %v\n%s\n%s", err, output, combined)
		}
	}
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
