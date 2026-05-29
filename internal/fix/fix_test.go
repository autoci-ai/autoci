package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/scanner"
)

func TestGenerateDependencyInstallFixTargetsExactYarnJob(t *testing.T) {
	workflow := writeWorkflow(t, `name: PR
jobs:
  admin-test:
    steps:
      - run: npm install
  frontend-unit-test:
    steps:
      - run: yarn install --immutable
      - run: yarn test
`)

	plan, err := Generate(Options{
		Workflow:     workflow,
		WorkflowName: "pr.yml",
		Opportunity:  "reliability-npm-install-failure",
		DryRun:       true,
		Evidence:     "3 occurrences in frontend-unit-test with ETIMEDOUT during yarn install.",
		TargetJobs:   []string{"frontend-unit-test"},
		Occurrences:  3,
		Signature:    "dependency install failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.PatchGenerated {
		t.Fatalf("expected patch, got reason: %s", plan.Reason)
	}
	if plan.PatchScope.UnrelatedLinesChanged != 0 {
		t.Fatalf("unexpected unrelated changes: %#v", plan.PatchScope)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Job != "frontend-unit-test" {
		t.Fatalf("unexpected targets: %#v", plan.Targets)
	}
	if !strings.Contains(plan.Diff, "yarn install --immutable") {
		t.Fatalf("expected yarn command in diff:\n%s", plan.Diff)
	}
	if strings.Contains(plan.Diff, "+      - run: n=0; until npm install") {
		t.Fatalf("patched wrong npm command:\n%s", plan.Diff)
	}
}

func TestGenerateRefusesPackageManagerMismatch(t *testing.T) {
	workflow := writeWorkflow(t, `jobs:
  frontend-unit-test:
    steps:
      - run: yarn install --immutable
`)

	plan, err := Generate(Options{
		Workflow:     workflow,
		WorkflowName: "pr.yml",
		Opportunity:  "reliability-npm-install-failure",
		DryRun:       true,
		TargetJobs:   []string{"frontend-unit-test"},
		Occurrences:  3,
		Signature:    "pnpm install failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PatchGenerated {
		t.Fatalf("expected no patch for package manager mismatch:\n%s", plan.Diff)
	}
	if !strings.Contains(plan.Reason, "does not match") {
		t.Fatalf("expected mismatch reason, got %q", plan.Reason)
	}
}

func TestGenerateRefusesSingleOccurrenceDependencyFailure(t *testing.T) {
	workflow := writeWorkflow(t, `jobs:
  frontend-unit-test:
    steps:
      - run: yarn install --immutable
`)

	plan, err := Generate(Options{
		Workflow:     workflow,
		WorkflowName: "pr.yml",
		Opportunity:  "reliability-npm-install-failure",
		DryRun:       true,
		TargetJobs:   []string{"frontend-unit-test"},
		Occurrences:  1,
		Signature:    "dependency install failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PatchGenerated {
		t.Fatalf("expected no patch for single occurrence:\n%s", plan.Diff)
	}
	if !strings.Contains(plan.Reason, "only 1 occurrence") {
		t.Fatalf("expected single occurrence reason, got %q", plan.Reason)
	}
}

func TestGenerateImagePullFailureProducesPlanOnlyWithCandidateReferences(t *testing.T) {
	workflow := writeWorkflow(t, `jobs:
  go-lint:
    steps:
      - uses: docker://rhysd/actionlint:latest
  actionlint:
    steps:
      - uses: docker://unrelated/actionlint:latest
`)

	plan, err := Generate(Options{
		Workflow:     workflow,
		WorkflowName: "pr.yml",
		Opportunity:  "reliability-image-pull-failure",
		DryRun:       true,
		TargetJobs:   []string{"go-lint", "integration-test:matrix-03"},
		Occurrences:  6,
		Signature:    "image pull failure",
		Artifacts: map[string][]string{
			"images": []string{"docker://rhysd/actionlint:latest"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PatchGenerated {
		t.Fatalf("expected image pull to produce plan only:\n%s", plan.Diff)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Image != "docker://rhysd/actionlint:latest" || plan.Targets[0].Job != "go-lint" {
		t.Fatalf("expected candidate image target, got %#v", plan.Targets)
	}
	if plan.Confidence != "high" {
		t.Fatalf("expected high confidence from log artifact, got %s", plan.Confidence)
	}
}

func TestGenerateImagePullNeedsMoreEvidenceCreatesInstrumentationPatch(t *testing.T) {
	workflow := writeWorkflow(t, `jobs:
  go-lint:
    steps:
      - run: go test ./...
  unrelated-job:
    steps:
      - run: echo unrelated
`)

	options := Options{
		Workflow:     workflow,
		WorkflowName: "pr.yml",
		Opportunity:  "failure-theme-image-pull-failure",
		DryRun:       true,
		TargetJobs:   []string{"go-lint"},
		Occurrences:  6,
		Signature:    "image pull failure",
		Readiness:    lifecycle.ReadinessNeedsMoreEvidence,
		Gaps: []EvidenceGap{
			{Type: "missing_image", Message: "Exact failing image reference not identified"},
			{Type: "missing_registry", Message: "Registry host could not be determined from cached evidence"},
			{Type: "missing_pull_error", Message: "Exact pull error not present in cached evidence"},
		},
	}
	plan, err := Generate(options)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.PatchGenerated || plan.FixType != InstrumentationFix || plan.Confidence != "high" {
		t.Fatalf("expected high-confidence instrumentation patch, got %#v", plan)
	}
	if len(plan.PatchScope.JobsTouched) != 1 || plan.PatchScope.JobsTouched[0] != "go-lint" {
		t.Fatalf("jobs touched = %#v", plan.PatchScope.JobsTouched)
	}
	for _, want := range []string{"AutoCI capture container diagnostics", "docker version || true", "docker images || true", "docker events --since 30m --until 0s || true"} {
		if !strings.Contains(plan.Diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, plan.Diff)
		}
	}
	if strings.Contains(plan.Diff, "unrelated-job") {
		t.Fatalf("diff touched unrelated job:\n%s", plan.Diff)
	}
	again, err := Generate(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Diff != again.Diff {
		t.Fatalf("instrumentation diff is not deterministic:\nfirst:\n%s\nsecond:\n%s", plan.Diff, again.Diff)
	}
}

func TestNormalizeResearchOpportunityID(t *testing.T) {
	if got := normalizeOpportunity("research-image-pull-failure"); got != "failure-theme-image-pull-failure" {
		t.Fatalf("normalized id = %q", got)
	}
	if got := normalizeOpportunity("reliability-image-pull-failure"); got != "failure-theme-image-pull-failure" {
		t.Fatalf("normalized id = %q", got)
	}
}

func writeWorkflow(t *testing.T, content string) scanner.Workflow {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pr.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return scanner.Workflow{Path: path}
}
