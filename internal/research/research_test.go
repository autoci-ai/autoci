package research

import (
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/profile"
)

func TestFromProfilePrioritizesAndHidesBacklog(t *testing.T) {
	runtimeProfile := &profile.Profile{
		Workflows: []profile.WorkflowProfile{{RunsAnalyzed: 10}},
		Findings: []profile.Finding{
			{ID: "long-running-job", Workflow: "pr", Job: "tiny-runtime", Evidence: "Average duration 7m 14s; P95 8m 29s; consumes 4% of measured job runtime."},
			{ID: "flaky-job", Workflow: "pr", Job: "frontend-unit-test", Evidence: "Failure rate 10% across 10 runs."},
			{ID: "flaky-job", Workflow: "pr", Job: "go-lint", Evidence: "Failure rate 19% across 10 runs."},
			{ID: "high-leverage-slow-job", Workflow: "pr", Job: "integration-tests", Evidence: "Average duration 17m 12s; P95 28m 03s; consumes 43% of measured job runtime."},
			{ID: "high-variance", Workflow: "pr", Job: "cache-sensitive", Evidence: "Duration varies between 2m and 24m."},
			{ID: "failure-aggregation-job", Workflow: "pr", Job: "gate", Evidence: "Failure rate 12% across 10 runs. Job depends on 6 upstream jobs."},
		},
	}

	plan := FromProfileWithOptions("pr.yml", runtimeProfile, false)
	if plan.RunsAnalyzed != 10 {
		t.Fatalf("runs analyzed = %d", plan.RunsAnalyzed)
	}
	if len(plan.Opportunities) != 3 {
		t.Fatalf("expected 3 visible opportunities, got %d", len(plan.Opportunities))
	}
	if plan.HiddenCount != 1 {
		t.Fatalf("expected one hidden opportunity after suppressing low-leverage runtime, got %d", plan.HiddenCount)
	}
	if plan.Opportunities[0].ID != "reliability-recurring-job-instability" {
		t.Fatalf("expected grouped flakiness first, got %s", plan.Opportunities[0].ID)
	}
	if !strings.Contains(plan.Opportunities[0].Evidence, "frontend-unit-test") || !strings.Contains(plan.Opportunities[0].Evidence, "go-lint") {
		t.Fatalf("expected grouped flaky evidence, got %q", plan.Opportunities[0].Evidence)
	}
	for _, opportunity := range plan.Opportunities {
		if opportunity.ID == "performance-tiny-runtime-critical-path" {
			t.Fatal("low-leverage runtime characterization should be suppressed")
		}
		if len(opportunity.SuggestedCommands) == 0 {
			t.Fatalf("expected suggested commands for %#v", opportunity)
		}
	}
	if plan.TopRecommendation.WhyNow == "" {
		t.Fatal("expected top recommendation why now")
	}
}

func TestFromProfileAndFailuresPrioritizesFailureThemes(t *testing.T) {
	runtimeProfile := &profile.Profile{
		Workflows: []profile.WorkflowProfile{{RunsAnalyzed: 10}},
		Findings: []profile.Finding{
			{ID: "flaky-job", Workflow: "pr", Job: "go-lint", Evidence: "Failure rate 19% across 10 runs."},
		},
	}
	failureAnalysis := &failures.Analysis{
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-image-pull-failure",
			Signature:   "image pull failure",
			Occurrences: 6,
			Jobs:        []string{"go-lint", "integration-test:matrix-03"},
			Artifacts: failures.FailureArtifacts{
				Images: []string{"softwaremill/elasticmq:1.4.5"},
				Hosts:  []string{"docker.io"},
			},
			Evidence: []failures.FailureEvidence{{
				RunID:        "run-1",
				Job:          "go-lint",
				Image:        "softwaremill/elasticmq:1.4.5",
				Registry:     "docker.io",
				RegistryHost: "docker.io",
				PullError:    "manifest unknown",
				LogExcerpt:   "failed to pull image softwaremill/elasticmq:1.4.5: manifest unknown",
			}},
		}},
	}

	plan := FromProfileAndFailures("pr.yml", runtimeProfile, failureAnalysis, false)
	if len(plan.Opportunities) == 0 {
		t.Fatal("expected opportunities")
	}
	if plan.Opportunities[0].ID != "reliability-image-pull-failure" {
		t.Fatalf("expected image pull theme first, got %s", plan.Opportunities[0].ID)
	}
	if plan.Opportunities[0].Category != "Reliability" {
		t.Fatalf("expected reliability category, got %s", plan.Opportunities[0].Category)
	}
	if !strings.Contains(plan.Opportunities[0].Evidence, "6 occurrences across 2 jobs") {
		t.Fatalf("unexpected evidence: %s", plan.Opportunities[0].Evidence)
	}
	if len(plan.Opportunities[0].RawEvidence.Images) != 1 || plan.Opportunities[0].RawEvidence.Images[0] != "softwaremill/elasticmq:1.4.5" {
		t.Fatalf("raw evidence = %#v", plan.Opportunities[0].RawEvidence)
	}
	if len(plan.Opportunities[0].WhyWeBelieveThis) == 0 || !strings.Contains(strings.Join(plan.Opportunities[0].WhyWeBelieveThis, "\n"), "docker.io") {
		t.Fatalf("why = %#v", plan.Opportunities[0].WhyWeBelieveThis)
	}
	if len(plan.Opportunities[0].Hypotheses) < 2 || plan.Opportunities[0].Hypotheses[0].Confidence == 0 {
		t.Fatalf("hypotheses = %#v", plan.Opportunities[0].Hypotheses)
	}
	if !strings.Contains(strings.Join(plan.Opportunities[0].InvestigationSteps, "\n"), "softwaremill/elasticmq:1.4.5") {
		t.Fatalf("steps = %#v", plan.Opportunities[0].InvestigationSteps)
	}
	if len(plan.Opportunities[0].SupportingArtifacts) == 0 {
		t.Fatalf("supporting artifacts = %#v", plan.Opportunities[0].SupportingArtifacts)
	}
}

func TestNPMResearchDoesNotClaimReferencedPackagesWhenEvidenceIsEmpty(t *testing.T) {
	failureAnalysis := &failures.Analysis{
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 3,
			Jobs:        []string{"frontend"},
			Artifacts: failures.FailureArtifacts{
				Packages: []string{"-", "0m", "111myarn", "because", "the", "your", "173mcore-js", "173mpact-core", "173munrs-resolver", "Corepack", "Yarn", "corepack", "npm", "p-prefixed", "peer-requirements", "six-letter", "yarn", "msw", "protobufjs", "snyk", "esbuild"},
			},
			Evidence: []failures.FailureEvidence{{
				RunID:        "run-1",
				Job:          "frontend",
				InstallError: "yarn install failed because your lockfile would have been modified",
				LogExcerpt:   "yarn install failed because your lockfile would have been modified",
				RegistryURL:  "https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js",
			}},
		}},
	}

	plan := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true)
	if len(plan.Opportunities) == 0 {
		t.Fatal("expected opportunities")
	}
	opportunity := plan.Opportunities[0]
	for _, bad := range []string{"173mcore-js", "173mpact-core", "173munrs-resolver", "Corepack", "Yarn", "corepack", "npm", "p-prefixed", "peer-requirements", "six-letter", "yarn"} {
		if containsResearchString(opportunity.RawEvidence.Modules, bad) || containsSupportingModule(opportunity.SupportingArtifacts, bad) {
			t.Fatalf("bad package %q leaked into package evidence: %#v", bad, opportunity)
		}
	}
	if len(opportunity.RawEvidence.Modules) != 0 {
		t.Fatalf("expected no package evidence from broad cached artifacts, got %#v", opportunity.RawEvidence.Modules)
	}
	if !strings.Contains(strings.Join(opportunity.RawEvidence.URLs, "\n"), "repo.yarnpkg.com") {
		t.Fatalf("expected URL evidence, got %#v", opportunity.RawEvidence.URLs)
	}
}

func TestWeakImagePullEvidenceDoesNotAssertRootCause(t *testing.T) {
	failureAnalysis := &failures.Analysis{
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-image-pull-failure",
			Signature:   "image pull failure",
			Occurrences: 6,
			Jobs:        []string{"go-lint", "integration-test:matrix-03", "integration-test:matrix-11"},
		}},
	}

	plan := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true)
	opportunity := plan.Opportunities[0]
	if !strings.Contains(opportunity.Hypothesis, "Recurring image/container setup failures are affecting go-lint, integration-test:matrix-03, integration-test:matrix-11") {
		t.Fatalf("weak hypothesis over/under stated: %q", opportunity.Hypothesis)
	}
	for _, forbidden := range []string{"rate-limited", "unavailable", "manifest lookup", "mutable", "public registry"} {
		if strings.Contains(opportunity.Hypothesis, forbidden) {
			t.Fatalf("weak hypothesis asserted %q: %q", forbidden, opportunity.Hypothesis)
		}
	}
	combined := opportunity.Experiment + "\n" + strings.Join(opportunity.InvestigationSteps, "\n")
	for _, forbidden := range []string{"affected image references", "registries and jobs", "registry access", "mutable tags"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("weak image opportunity implied missing evidence %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(opportunity.Experiment, "exact failing image/container setup step") {
		t.Fatalf("expected thin-evidence experiment, got %q", opportunity.Experiment)
	}
}

func TestWeakNPMEvidenceDoesNotAssertRootCause(t *testing.T) {
	failureAnalysis := &failures.Analysis{
		FailureThemes: []failures.FailureTheme{{
			ID:          "failure-theme-npm-install-failure",
			Signature:   "npm install failure",
			Occurrences: 4,
			Jobs:        []string{"frontend"},
		}},
	}

	plan := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true)
	opportunity := plan.Opportunities[0]
	if !strings.Contains(opportunity.Hypothesis, "Frontend dependency installation is failing in frontend") {
		t.Fatalf("weak npm hypothesis over/under stated: %q", opportunity.Hypothesis)
	}
	for _, forbidden := range []string{"peer constraints disagree", "registry access", "lockfile state"} {
		if strings.Contains(opportunity.Hypothesis, forbidden) {
			t.Fatalf("weak npm hypothesis asserted %q: %q", forbidden, opportunity.Hypothesis)
		}
	}
	combined := opportunity.Experiment + "\n" + strings.Join(opportunity.InvestigationSteps, "\n")
	for _, forbidden := range []string{"affected packages", "registry URLs", "lockfile and dependency cache"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("weak npm opportunity implied missing evidence %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(opportunity.Experiment, "exact package, dependency constraint, registry URL, or package-manager error") {
		t.Fatalf("expected thin-evidence npm experiment, got %q", opportunity.Experiment)
	}
}

func TestStrongImagePullEvidenceUsesConcreteArtifacts(t *testing.T) {
	failureAnalysis := &failures.Analysis{FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-image-pull-failure",
		Signature:   "image pull failure",
		Occurrences: 2,
		Jobs:        []string{"integration"},
		Evidence: []failures.FailureEvidence{{
			Job:          "integration",
			Image:        "mysql:8.0",
			Registry:     "docker.io",
			RegistryHost: "docker.io",
			LogExcerpt:   "failed to pull image mysql:8.0 from docker.io: manifest unknown",
			PullError:    "manifest unknown",
		}},
	}}}

	opportunity := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true).Opportunities[0]
	combined := opportunity.Hypothesis + "\n" + strings.Join(opportunity.WhyWeBelieveThis, "\n") + "\n" + strings.Join(opportunity.InvestigationSteps, "\n")
	for _, want := range []string{"mysql:8.0", "docker.io"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("expected strong image evidence %q in opportunity: %#v", want, opportunity)
		}
	}
}

func TestStrongNPMEvidenceUsesCleanPackagesOnly(t *testing.T) {
	failureAnalysis := &failures.Analysis{FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 2,
		Jobs:        []string{"frontend"},
		Evidence: []failures.FailureEvidence{{
			Job:          "frontend",
			PackageName:  "msw",
			InstallError: "yarn install failed: ERESOLVE peer dependency conflict for msw",
			LogExcerpt:   "yarn install failed: ERESOLVE peer dependency conflict for msw",
		}},
	}}}

	opportunity := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true).Opportunities[0]
	combined := opportunity.Hypothesis + "\n" + strings.Join(opportunity.WhyWeBelieveThis, "\n") + "\n" + strings.Join(opportunity.InvestigationSteps, "\n")
	if !strings.Contains(combined, "msw") {
		t.Fatalf("expected clean package evidence in opportunity: %#v", opportunity)
	}
	for _, bad := range []string{"173mcore-js", "90mYN0000", "31mSTDERR", "--immutable", "six-letter", "p-prefixed", "your", "because", "home/runner"} {
		if strings.Contains(combined, bad) {
			t.Fatalf("bad npm token leaked into opportunity: %q in %s", bad, combined)
		}
	}
}

func TestNPMDownloadEvidenceDoesNotClaimResolutionFailure(t *testing.T) {
	failureAnalysis := &failures.Analysis{FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 3,
		Jobs:        []string{"frontend-unit-test"},
		Artifacts: failures.FailureArtifacts{
			Packages: []string{"core-js", "pact-core", "unrs-resolver", "msw"},
			URLs:     []string{"https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js", "https://downloads.snyk.io/cli"},
		},
		Evidence: []failures.FailureEvidence{
			{
				Job:         "frontend-unit-test",
				LogExcerpt:  "Corepack is about to download https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js",
				RegistryURL: "https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js",
			},
			{
				Job:         "frontend-unit-test",
				LogExcerpt:  "Downloading Snyk binary from https://downloads.snyk.io/cli",
				RegistryURL: "https://downloads.snyk.io/cli",
			},
			{
				Job:        "frontend-unit-test",
				LogExcerpt: "✓ test passed successfully",
			},
		},
	}}}

	opportunity := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true).Opportunities[0]
	combined := opportunity.Hypothesis + "\n" + strings.Join(opportunity.WhyWeBelieveThis, "\n") + "\n" + strings.Join(opportunity.Hypotheses[0].Evidence, "\n")
	for _, forbidden := range []string{"Dependency resolution is failing", "peer-dependency conflict", "lockfile state"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("download-only evidence asserted %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(opportunity.Hypothesis, "package manager/bootstrap or external download activity") {
		t.Fatalf("expected cautious bootstrap/download hypothesis, got %q", opportunity.Hypothesis)
	}
	if strings.Contains(strings.Join(opportunity.WhyWeBelieveThis, "\n"), "test passed successfully") {
		t.Fatalf("successful test line leaked into representative evidence: %#v", opportunity.WhyWeBelieveThis)
	}
}

func TestNPMResolutionMarkersAllowResolutionHypothesis(t *testing.T) {
	failureAnalysis := &failures.Analysis{FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 2,
		Jobs:        []string{"frontend-unit-test"},
		Evidence: []failures.FailureEvidence{{
			Job:         "frontend-unit-test",
			PackageName: "msw",
			LogExcerpt:  "YN0002: msw doesn't provide @types/node, requested by protobufjs",
		}},
	}}}

	opportunity := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true).Opportunities[0]
	if !strings.Contains(opportunity.Hypothesis, "Dependency resolution is failing") {
		t.Fatalf("expected explicit resolution marker to allow resolution hypothesis, got %q", opportunity.Hypothesis)
	}
}

func TestSnykHashMismatchClassifiesAsBinaryIntegrity(t *testing.T) {
	failureAnalysis := &failures.Analysis{FailureThemes: []failures.FailureTheme{{
		ID:          "failure-theme-npm-install-failure",
		Signature:   "npm install failure",
		Occurrences: 3,
		Jobs:        []string{"frontend-unit-test"},
		Artifacts: failures.FailureArtifacts{
			Packages: []string{"snyk", "core-js", "esbuild", "msw", "protobufjs"},
			URLs:     []string{"https://downloads.snyk.io/cli/v1.1302.1/snyk-linux"},
		},
		Evidence: []failures.FailureEvidence{
			{Job: "frontend-unit-test", LogExcerpt: "Corepack is about to download https://repo.yarnpkg.com/4.5.1/packages/yarnpkg-cli/bin/yarn.js"},
			{Job: "frontend-unit-test", PackageName: "snyk", LogExcerpt: "snyk@npm:1.1302.1 STDERR - actual: abc123"},
			{Job: "frontend-unit-test", PackageName: "snyk", LogExcerpt: "snyk@npm:1.1302.1 STDERR - expected: def456"},
		},
	}}}

	opportunity := FromProfileAndFailures("pr.yml", nil, failureAnalysis, true).Opportunities[0]
	combined := opportunity.Hypothesis + "\n" + strings.Join(opportunity.Hypotheses[0].Evidence, "\n") + "\n" + strings.Join(opportunity.InvestigationSteps, "\n") + "\n" + strings.Join(opportunity.RawEvidence.Modules, "\n") + "\n" + strings.Join(opportunity.RawEvidence.URLs, "\n")
	if !strings.Contains(opportunity.Hypothesis, "Snyk package install is failing during binary download or integrity verification") {
		t.Fatalf("expected Snyk binary integrity hypothesis, got %q", opportunity.Hypothesis)
	}
	if len(opportunity.RawEvidence.Modules) != 1 || opportunity.RawEvidence.Modules[0] != "snyk" {
		t.Fatalf("expected only snyk package evidence, got %#v", opportunity.RawEvidence.Modules)
	}
	if !strings.Contains(strings.Join(opportunity.RawEvidence.URLs, "\n"), "downloads.snyk.io/cli") {
		t.Fatalf("expected Snyk download URL evidence, got %#v", opportunity.RawEvidence.URLs)
	}
	for _, forbidden := range []string{"Dependency resolution is failing", "peer-dependency conflict", "core-js", "esbuild", "msw", "protobufjs"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("Snyk integrity evidence produced resolver claim %q: %s", forbidden, combined)
		}
	}
}

func containsResearchString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSupportingModule(values []SupportingArtifact, want string) bool {
	for _, value := range values {
		if value.Type == "module" && value.Value == want {
			return true
		}
	}
	return false
}

func TestVerboseShowsCompleteBacklog(t *testing.T) {
	runtimeProfile := &profile.Profile{Findings: []profile.Finding{
		{ID: "flaky-job", Workflow: "pr", Job: "frontend-unit-test", Evidence: "Failure rate 10% across 10 runs."},
		{ID: "high-leverage-slow-job", Workflow: "pr", Job: "integration-tests", Evidence: "Average duration 17m 12s; P95 28m 03s; consumes 43% of measured job runtime."},
		{ID: "high-variance", Workflow: "pr", Job: "cache-sensitive", Evidence: "Duration varies between 2m and 24m."},
		{ID: "failure-aggregation-job", Workflow: "pr", Job: "gate", Evidence: "Failure rate 12% across 10 runs. Job depends on 6 upstream jobs."},
	}}

	plan := FromProfileWithOptions("pr.yml", runtimeProfile, true)
	if len(plan.Opportunities) != 4 {
		t.Fatalf("expected full backlog, got %d", len(plan.Opportunities))
	}
	if plan.HiddenCount != 0 {
		t.Fatalf("expected no hidden count in verbose mode, got %d", plan.HiddenCount)
	}
}
