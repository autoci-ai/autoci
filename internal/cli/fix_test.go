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

func TestFixGeneratesYarnSnykInstrumentationForSnykIntegrityResearch(t *testing.T) {
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
  other-job:
    steps:
      - run: echo unrelated
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{Workflow: "pr.yml", FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 3,
		Jobs:        []string{"frontend-unit-test"},
		Artifacts:   failures.FailureArtifacts{Packages: []string{"snyk"}, URLs: []string{"https://downloads.snyk.io/cli/v1.1302.1/snyk-linux"}},
		Evidence: []failures.FailureEvidence{{
			Job:         "frontend-unit-test",
			PackageName: "snyk",
			LogExcerpt:  "snyk@npm:1.1302.1 STDERR - downloading https://downloads.snyk.io/cli/v1.1302.1/snyk-linux",
			RegistryURL: "https://downloads.snyk.io/cli/v1.1302.1/snyk-linux",
		}, {
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
		"Patch type: instrumentation",
		"Patch generated: yes",
		"Workflow jobs touched:",
		"- frontend-unit-test",
		"AutoCI capture yarn install diagnostics",
		"Reason: Readiness is needs_more_evidence; generated instrumentation from Snyk/Yarn install gaps",
		"+      - name: AutoCI capture yarn install diagnostics",
		"+        if: failure()",
		"+          node --version || true",
		"+          corepack --version || true",
		"+          yarn --version || true",
		"+          yarn config || true",
		"+          yarn config get cacheFolder || yarn cache dir || true",
		"+          curl -fsSIL --max-time 10 https://downloads.snyk.io/",
		"+          curl -fsSIL --max-time 10 https://repo.yarnpkg.com/",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "until yarn install --immutable") || strings.Contains(output, "+          yarn install") {
		t.Fatalf("Snyk instrumentation wrapped or reran install:\n%s", output)
	}
	if strings.Contains(output, "other-job") {
		t.Fatalf("unrelated job modified:\n%s", output)
	}
}

func TestFixJSONYarnSnykInstrumentationIsValid(t *testing.T) {
	dir := setupSnykInstrumentationFixState(t)
	output, err := runFixOutput(t, dir, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		SourceID       string              `json:"sourceId"`
		Readiness      lifecycle.Readiness `json:"readiness"`
		FixType        string              `json:"fixType"`
		PatchGenerated bool                `json:"patchGenerated"`
		PatchApplied   bool                `json:"patchApplied"`
		Targets        []struct {
			Workflow    string   `json:"workflow"`
			Job         string   `json:"job"`
			Step        string   `json:"step"`
			DerivedFrom []string `json:"derivedFrom"`
		} `json:"targets"`
		PatchScope struct {
			JobsTouched  []string `json:"jobsTouched"`
			StepsTouched []string `json:"stepsTouched"`
		} `json:"patchScope"`
		Diff string `json:"diff"`
	}
	assertJSONOnlyAndJQ(t, output, &plan)
	if plan.SourceID != "failure-theme-npm-install-failure" || plan.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("plan = %#v\n%s", plan, output)
	}
	if !plan.PatchGenerated || plan.PatchApplied || plan.FixType != "instrumentation" {
		t.Fatalf("patch status = %#v\n%s", plan, output)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Job != "frontend-unit-test" || plan.Targets[0].Step != "AutoCI capture yarn install diagnostics" {
		t.Fatalf("targets = %#v", plan.Targets)
	}
	if len(plan.PatchScope.JobsTouched) != 1 || plan.PatchScope.JobsTouched[0] != "frontend-unit-test" {
		t.Fatalf("jobsTouched = %#v", plan.PatchScope.JobsTouched)
	}
	if len(plan.PatchScope.StepsTouched) != 1 || plan.PatchScope.StepsTouched[0] != "AutoCI capture yarn install diagnostics" {
		t.Fatalf("stepsTouched = %#v", plan.PatchScope.StepsTouched)
	}
	for _, want := range []string{"AutoCI capture yarn install diagnostics", "if: failure()", "node --version || true", "downloads.snyk.io", "repo.yarnpkg.com"} {
		if !strings.Contains(plan.Diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, plan.Diff)
		}
	}
	if strings.Contains(plan.Diff, "until yarn install") {
		t.Fatalf("instrumentation diff retried install:\n%s", plan.Diff)
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
	if target.Readiness != lifecycle.ReadinessReadyForFix {
		t.Fatalf("test fixture should be ready_for_fix, got %q", target.Readiness)
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

	output, err := runFixOutput(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "Patch type: root_cause") {
		t.Fatalf("not-ready output claimed root-cause fix:\n%s", output)
	}
	for _, want := range []string{"Cannot generate fix.", "Readiness: needs_more_evidence", "Exact package or dependency constraint not identified"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestFixTextNotReadyProfileFindingUsesResearchGaps(t *testing.T) {
	dir := setupProfileNotReadyFixState(t)
	output, err := runFixOutputForID(t, dir, "high-variance-go-unit-test-matrix-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Cannot generate fix.",
		"Readiness: not_ready",
		"Reason:",
		"- No workflow step matched the finding with enough confidence.",
		"- Representative log excerpts are not present in cached evidence.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"Patch type: root_cause", "No surgical fix generator is available"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output contains %q:\n%s", unwanted, output)
		}
	}
}

func TestFixJSONNotReadyProfileFindingIncludesGapsAndNextSteps(t *testing.T) {
	dir := setupProfileNotReadyFixState(t)
	output, err := runFixOutputForID(t, dir, "high-variance-go-unit-test-matrix-2", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		SourceID       string              `json:"sourceId"`
		Readiness      lifecycle.Readiness `json:"readiness"`
		FixType        string              `json:"fixType"`
		PatchGenerated bool                `json:"patchGenerated"`
		PatchApplied   bool                `json:"patchApplied"`
		Reason         string              `json:"reason"`
		Gaps           []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"gaps"`
		NextSteps []string `json:"nextSteps"`
	}
	assertJSONOnlyAndJQ(t, output, &plan)
	if plan.SourceID != "high-variance-go-unit-test-matrix-2" || plan.Readiness != lifecycle.ReadinessNotReady {
		t.Fatalf("unexpected plan: %#v\n%s", plan, output)
	}
	if plan.FixType != "" || plan.PatchGenerated || plan.PatchApplied {
		t.Fatalf("not-ready plan claimed patch/fix type: %#v\n%s", plan, output)
	}
	for _, want := range []string{"No workflow step matched the finding with enough confidence.", "Representative log excerpts are not present in cached evidence."} {
		if !strings.Contains(plan.Reason, want) {
			t.Fatalf("reason missing %q: %#v", want, plan)
		}
	}
	if len(plan.Gaps) != 2 {
		t.Fatalf("gaps = %#v", plan.Gaps)
	}
	gapsByMessage := map[string]string{}
	for _, gap := range plan.Gaps {
		gapsByMessage[strings.TrimSuffix(gap.Message, ".")] = gap.Type
	}
	if gapsByMessage["No workflow step matched the finding with enough confidence"] != "missing_workflow_step_match" {
		t.Fatalf("workflow step gap missing: %#v", plan.Gaps)
	}
	if gapsByMessage["Representative log excerpts are not present in cached evidence"] != "missing_logs" {
		t.Fatalf("missing logs gap missing: %#v", plan.Gaps)
	}
	if len(plan.NextSteps) == 0 || !strings.Contains(strings.Join(plan.NextSteps, "\n"), "Inspect workflow steps for job go-unit-test:matrix-2") {
		t.Fatalf("nextSteps = %#v", plan.NextSteps)
	}
	if strings.Contains(output, "No surgical fix generator is available") || strings.Contains(output, `"fixType": "root_cause"`) {
		t.Fatalf("json contains generator/root cause claim:\n%s", output)
	}
}

func TestFixNotReadyTextReasonsHaveStructuredJSONGaps(t *testing.T) {
	dir := setupProfileNotReadyFixState(t)
	textOutput, err := runFixOutputForID(t, dir, "high-variance-go-unit-test-matrix-2")
	if err != nil {
		t.Fatal(err)
	}
	jsonOutput, err := runFixOutputForID(t, dir, "high-variance-go-unit-test-matrix-2", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Gaps []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"gaps"`
	}
	assertJSONOnlyAndJQ(t, jsonOutput, &plan)
	gapsByMessage := map[string]bool{}
	for _, gap := range plan.Gaps {
		if gap.Type == "" {
			t.Fatalf("gap missing type: %#v", gap)
		}
		gapsByMessage[strings.TrimSuffix(gap.Message, ".")] = true
	}
	for _, reason := range textReasonLines(textOutput) {
		if !gapsByMessage[strings.TrimSuffix(reason, ".")] {
			t.Fatalf("text reason %q missing structured gap in %#v\ntext:\n%s\njson:\n%s", reason, plan.Gaps, textOutput, jsonOutput)
		}
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
		FixType        string              `json:"fixType"`
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
	if plan.FixType != "root_cause" {
		t.Fatalf("fixType = %q", plan.FixType)
	}
}

func TestFixJSONNeedsMoreEvidenceEmitsJSONOnly(t *testing.T) {
	dir := setupImageNeedsEvidenceFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-image-pull-failure", map[string]any{
		"id":        "failure-theme-image-pull-failure",
		"workflow":  "pr.yml",
		"jobs":      []string{"integration-test:matrix-03", "integration-test:matrix-11"},
		"readiness": lifecycle.ReadinessNeedsMoreEvidence,
		"gaps": []map[string]any{
			{"type": "missing_image", "message": "Exact failing image reference not identified"},
			{"type": "missing_registry", "message": "Registry host could not be determined from cached evidence"},
			{"type": "missing_pull_error", "message": "Exact pull error not present in cached evidence"},
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
		FixType        string              `json:"fixType"`
		AffectedJobs   []string            `json:"affectedJobs"`
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
		Targets []struct {
			Workflow    string   `json:"workflow"`
			Job         string   `json:"job"`
			DerivedFrom []string `json:"derivedFrom"`
		} `json:"targets"`
		Validation []string `json:"validation"`
		Diff       string   `json:"diff"`
	}
	assertJSONOnlyAndJQ(t, out.String(), &plan)
	if plan.SourceID != "failure-theme-image-pull-failure" || plan.Workflow != "pr.yml" || plan.Readiness != lifecycle.ReadinessNeedsMoreEvidence {
		t.Fatalf("unexpected refusal json: %#v\n%s", plan, out.String())
	}
	if !plan.PatchGenerated || plan.PatchApplied || plan.FixType != "instrumentation" {
		t.Fatalf("unexpected instrumentation status: %#v", plan)
	}
	if len(plan.Gaps) != 3 || plan.Gaps[0].Message != "Exact failing image reference not identified" {
		t.Fatalf("gaps = %#v", plan.Gaps)
	}
	if len(plan.Validation) == 0 {
		t.Fatalf("validation = %#v", plan.Validation)
	}
	if got := strings.Join(plan.AffectedJobs, ","); got != "integration-test:matrix-03,integration-test:matrix-11" {
		t.Fatalf("affectedJobs = %#v", plan.AffectedJobs)
	}
	if len(plan.PatchScope.JobsTouched) != 1 || plan.PatchScope.JobsTouched[0] != "integration-test" {
		t.Fatalf("jobsTouched = %#v", plan.PatchScope.JobsTouched)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Job != "integration-test" {
		t.Fatalf("targets = %#v", plan.Targets)
	}
	if got := strings.Join(plan.Targets[0].DerivedFrom, ","); got != "integration-test:matrix-03,integration-test:matrix-11" {
		t.Fatalf("derivedFrom = %#v", plan.Targets[0].DerivedFrom)
	}
	if len(plan.PatchScope.StepsTouched) != 1 || plan.PatchScope.StepsTouched[0] != "AutoCI capture container diagnostics" {
		t.Fatalf("stepsTouched = %#v", plan.PatchScope.StepsTouched)
	}
	for _, want := range []string{"AutoCI capture container diagnostics", "if: failure()", "docker info || true", "docker events --since 30m || true"} {
		if !strings.Contains(plan.Diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, plan.Diff)
		}
	}
	if strings.Index(plan.Diff, "+      - name: AutoCI capture container diagnostics") < strings.Index(plan.Diff, "       - run: go test ./...") {
		t.Fatalf("instrumentation was not appended after existing steps:\n%s", plan.Diff)
	}
	if strings.Contains(plan.Diff, "unrelated-job") {
		t.Fatalf("instrumentation touched unrelated job:\n%s", plan.Diff)
	}
	if strings.Contains(out.String(), `"jobsTouched": null`) || strings.Contains(out.String(), `"stepsTouched": null`) {
		t.Fatalf("nullable patchScope arrays in output:\n%s", out.String())
	}
}

func TestFixHumanOutputShowsRuntimeAndWorkflowJobs(t *testing.T) {
	dir := setupImageNeedsEvidenceFixState(t)
	writeRawResearchEvidence(t, dir, "failure-theme-image-pull-failure", map[string]any{
		"id":        "failure-theme-image-pull-failure",
		"workflow":  "pr.yml",
		"jobs":      []string{"integration-test:matrix-03", "integration-test:matrix-11"},
		"readiness": lifecycle.ReadinessNeedsMoreEvidence,
		"gaps": []map[string]any{
			{"type": "missing_image", "message": "Exact failing image reference not identified"},
		},
	})

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--path", dir, "fix", "failure-theme-image-pull-failure", "--workflow", "pr.yml", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	for _, want := range []string{
		"Affected runtime jobs:",
		"- integration-test:matrix-03",
		"- integration-test:matrix-11",
		"Workflow jobs touched:",
		"- integration-test",
		"Derived from: [integration-test:matrix-03 integration-test:matrix-11]",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
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
	return runFixOutputForID(t, dir, "failure-theme-npm-install-failure", extraArgs...)
}

func runFixOutputForID(t *testing.T, dir, id string, extraArgs ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	args := []string{"--path", dir, "fix", id, "--dry-run"}
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

func setupImageNeedsEvidenceFixState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  integration-test:
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
  unrelated-job:
    steps:
      - run: echo unrelated
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{Workflow: "pr.yml", FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-image-pull-failure",
		Signature:   "image pull failure",
		Occurrences: 3,
		Jobs:        []string{"integration-test:matrix-03", "integration-test:matrix-11"},
	}}}
	if err := state.Write(dir, "failures", "pr.yml", analysis); err != nil {
		t.Fatal(err)
	}
	return dir
}

func setupProfileNotReadyFixState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, ".depot", "workflows", "pr.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  go-unit-test:
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRawResearchEvidence(t, dir, "high-variance-go-unit-test-matrix-2", map[string]any{
		"id":        "high-variance-go-unit-test-matrix-2",
		"workflow":  "pr.yml",
		"jobs":      []string{"go-unit-test:matrix-2"},
		"readiness": lifecycle.ReadinessNotReady,
		"gaps": []map[string]any{
			{"type": "missing_logs", "message": "Representative log excerpts are not present in cached evidence."},
		},
		"recommendedInvestigation": []string{
			"Inspect workflow steps for job go-unit-test:matrix-2 in pr.yml and identify the command or action consuming the measured time.",
			"Compare recent slow and fast runs for the same job before changing cache keys, matrix shape, or runner sizing.",
		},
	})
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

func setupSnykInstrumentationFixState(t *testing.T) string {
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
  other-job:
    steps:
      - run: echo unrelated
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	analysis := failures.Analysis{Workflow: "pr.yml", FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 3,
		Jobs:        []string{"frontend-unit-test"},
		Artifacts:   failures.FailureArtifacts{Packages: []string{"snyk"}, URLs: []string{"https://downloads.snyk.io/cli/v1.1302.1/snyk-linux"}},
		Evidence: []failures.FailureEvidence{{
			Job:         "frontend-unit-test",
			PackageName: "snyk",
			LogExcerpt:  "snyk@npm:1.1302.1 STDERR - downloading https://downloads.snyk.io/cli/v1.1302.1/snyk-linux",
			RegistryURL: "https://downloads.snyk.io/cli/v1.1302.1/snyk-linux",
		}, {
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

func textReasonLines(output string) []string {
	var reasons []string
	inReason := false
	for _, line := range strings.Split(output, "\n") {
		switch strings.TrimSpace(line) {
		case "Reason:":
			inReason = true
			continue
		case "Next step:":
			inReason = false
		}
		if !inReason {
			continue
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			reasons = append(reasons, strings.TrimPrefix(line, "- "))
		}
	}
	return reasons
}

func readFixRecord(t *testing.T, dir string) (fixRecordSnapshot, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".autoci", "fixes", "failure-theme-npm-install-failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record fixRecordSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record, string(data)
}
