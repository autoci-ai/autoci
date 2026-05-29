package failures

import (
	"strings"
	"testing"
)

func TestAnalyzeGroupsFailuresByTheme(t *testing.T) {
	analysis := Analyze("pr.yml", 31, 7, []Observation{
		{Job: "go-lint", RunID: "run-1", Message: "failed to pull image docker://rhysd/actionlint:latest context deadline exceeded"},
		{Job: "integration-test:matrix-03", RunID: "run-2", Message: "image pull failed from ghcr.io/example/image:latest"},
		{Job: "frontend-unit-test", RunID: "run-3", Message: "npm install failed"},
		{Job: "gate", RunID: "run-4", Message: "Complete job name: gate"},
	})

	if len(analysis.FailureThemes) != 2 {
		t.Fatalf("expected 2 themes, got %d", len(analysis.FailureThemes))
	}
	if analysis.FailureThemes[0].Signature != "image pull failure" {
		t.Fatalf("signature = %q", analysis.FailureThemes[0].Signature)
	}
	if analysis.FailureThemes[0].ID != "failure-theme-image-pull-failure" {
		t.Fatalf("id = %q", analysis.FailureThemes[0].ID)
	}
	if analysis.FailureThemes[0].Occurrences != 2 {
		t.Fatalf("occurrences = %d", analysis.FailureThemes[0].Occurrences)
	}
	if len(analysis.FailureThemes[0].Jobs) != 2 {
		t.Fatalf("jobs = %#v", analysis.FailureThemes[0].Jobs)
	}
	if len(analysis.FailureThemes[0].Evidence) != 2 {
		t.Fatalf("evidence = %#v", analysis.FailureThemes[0].Evidence)
	}
	if len(analysis.FailureThemes[0].Artifacts.Images) != 2 {
		t.Fatalf("images = %#v", analysis.FailureThemes[0].Artifacts.Images)
	}
	if len(analysis.Findings) != 2 || analysis.Findings[0].ID != "failure-theme-image-pull-failure" {
		t.Fatalf("findings = %#v", analysis.Findings)
	}
	if len(analysis.AggregationJobs) != 1 || analysis.AggregationJobs[0].Job != "gate" {
		t.Fatalf("aggregation jobs = %#v", analysis.AggregationJobs)
	}
}

func TestImagePullEvidenceDoesNotCollectUnrelatedPathsModulesOrURLs(t *testing.T) {
	message := `go test ./internal/failures
github.com/autoci-ai/autoci/internal/failures failed
/home/runner/work/autoci/internal/failures/failures.go:12
https://example.com/docs/image-pull
failed to pull image docker://golang:1.24: manifest unknown`

	analysis := Analyze("pr.yml", 10, 2, []Observation{{
		Job:     "go-lint",
		RunID:   "run-1",
		Message: message,
	}})

	theme := analysis.FailureThemes[0]
	if theme.ID != "failure-theme-image-pull-failure" {
		t.Fatalf("id = %q", theme.ID)
	}
	if got := theme.Artifacts.Images; len(got) != 1 || got[0] != "docker://golang:1.24" {
		t.Fatalf("images = %#v", got)
	}
	if len(theme.Artifacts.Modules) != 0 || len(theme.Artifacts.URLs) != 0 || len(theme.Artifacts.Packages) != 0 {
		t.Fatalf("polluted artifacts = %#v", theme.Artifacts)
	}
	if len(theme.Evidence) != 1 || theme.Evidence[0].PullError != "manifest unknown" {
		t.Fatalf("evidence = %#v", theme.Evidence)
	}
}

func TestImagePullEvidenceOnlyUsesExplicitImageContexts(t *testing.T) {
	message := `17:42:31.001 image pull check started
deadline:2026-05-29T17:42:00Z image pull timeout metadata
test TestHTTP/17:42 failed while waiting for image pull
Creating container for image mysql:8.0
Creating container for image mysql:8.0
failed to pull image mysql:8.0: manifest unknown`

	analysis := Analyze("pr.yml", 10, 2, []Observation{{
		Job:     "integration",
		RunID:   "run-1",
		Message: message,
	}})

	theme := analysis.FailureThemes[0]
	if got := theme.Artifacts.Images; len(got) != 1 || got[0] != "mysql:8.0" {
		t.Fatalf("images = %#v", got)
	}
	if len(theme.Evidence) != 1 {
		t.Fatalf("expected only failure lines as evidence, got %#v", theme.Evidence)
	}
	for _, evidence := range theme.Evidence {
		if !isImagePullErrorLine(evidence.LogExcerpt) {
			t.Fatalf("expected failure line evidence, got %#v", evidence)
		}
		if evidence.Image == "17:42" || strings.HasPrefix(evidence.Image, "deadline:") {
			t.Fatalf("unexpected metadata image = %#v", evidence)
		}
	}
}

func TestImagePullEvidenceIgnoresSuccessfulLifecycleLogs(t *testing.T) {
	evidence := ExtractEvidence("image pull failure", Observation{
		Job:   "integration",
		RunID: "run-1",
		Message: `2026/05/29 14:57:32 🐳 Creating container for image mysql:8.0
2026/05/29 14:57:33 Waiting for container for image mysql:8.0 to be ready
2026/05/29 14:57:34 Connected to container mysql
2026/05/29 14:57:35 Created container 17:42
2026/05/29 14:57:36 Started container deadline:soon
2026/05/29 14:57:37 Container is ready Port:3306`,
	})

	if len(evidence) != 0 {
		t.Fatalf("successful lifecycle logs should not produce failure evidence: %#v", evidence)
	}
}

func TestImagePullEvidenceUsesAdjacentLifecycleImageForErrorLine(t *testing.T) {
	evidence := ExtractEvidence("image pull failure", Observation{
		Job:   "integration",
		RunID: "run-1",
		Message: `2026/05/29 14:57:32 Creating container for image mysql:8.0
2026/05/29 14:57:33 failed to pull: manifest unknown`,
	})

	if len(evidence) != 1 {
		t.Fatalf("evidence = %#v", evidence)
	}
	if evidence[0].Image != "mysql:8.0" || !strings.Contains(evidence[0].LogExcerpt, "failed to pull") {
		t.Fatalf("unexpected evidence = %#v", evidence[0])
	}
}

func TestNPMEvidenceIsFailureSpecific(t *testing.T) {
	analysis := Analyze("pr.yml", 10, 2, []Observation{{
		Job:   "frontend",
		RunID: "run-1",
		Message: `npm ERR! ERESOLVE unable to resolve dependency tree
npm ERR! peer react@"^18" from @testing-library/react@14.1.2
npm ERR! registry https://registry.npmjs.org/`,
	}})

	theme := analysis.FailureThemes[0]
	if theme.ID != "failure-theme-npm-install-failure" {
		t.Fatalf("id = %q", theme.ID)
	}
	if len(theme.Artifacts.Packages) == 0 {
		t.Fatalf("packages = %#v", theme.Artifacts.Packages)
	}
	if len(theme.Artifacts.Modules) != 0 {
		t.Fatalf("modules = %#v", theme.Artifacts.Modules)
	}
	if len(theme.Evidence) == 0 || theme.Evidence[0].InstallError == "" {
		t.Fatalf("evidence = %#v", theme.Evidence)
	}
}

func TestNPMEvidenceRejectsLogNoiseAndPreservesUsefulURLs(t *testing.T) {
	analysis := Analyze("pr.yml", 10, 2, []Observation{{
		Job:   "frontend",
		RunID: "run-1",
		Message: "\x1b[31m➤ YN0000\x1b[0m: 0m 111myarn because the your --immutable\n" +
			"2026-05-29T14:57:32Z yarn install failed because your lockfile would have been modified\n" +
			"➤ YN0001: │ Error: @snyk/protect@npm:1.1294.0 failed because @snyk/cli-interface@npm:^2.0.0 could not be resolved\n" +
			"➤ YN0002: │ 173mcore-js@npm:3.37.1 173mpact-core@npm:1.0.0 173munrs-resolver@npm:1.9.0\n" +
			"➤ YN0003: │ Corepack Yarn corepack npm p-prefixed peer-requirements six-letter yarn\n" +
			"➤ YN0000: │ Downloading https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js\n" +
			"npm ERR! request to https://downloads.snyk.io/cli failed",
	}})

	theme := analysis.FailureThemes[0]
	for _, bad := range []string{"-", "0m", "111myarn", "because", "the", "your", "--immutable", "Corepack", "Yarn", "corepack", "npm", "p-prefixed", "peer-requirements", "six-letter", "yarn", "173mcore-js", "173mpact-core", "173munrs-resolver"} {
		if containsString(theme.Artifacts.Packages, bad) {
			t.Fatalf("unexpected package %q in %#v", bad, theme.Artifacts.Packages)
		}
	}
	for _, want := range []string{"@snyk/protect", "@snyk/cli-interface", "core-js", "pact-core", "unrs-resolver"} {
		if !containsString(theme.Artifacts.Packages, want) {
			t.Fatalf("missing package %q in %#v", want, theme.Artifacts.Packages)
		}
	}
	for _, want := range []string{"https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js", "https://downloads.snyk.io/cli"} {
		if !containsString(theme.Artifacts.URLs, want) {
			t.Fatalf("missing url %q in %#v", want, theme.Artifacts.URLs)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
