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
				Packages: []string{"-", "0m", "111myarn", "because", "the", "your"},
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
	if strings.Contains(strings.Join(opportunity.WhyWeBelieveThis, "\n"), "Referenced packages") {
		t.Fatalf("unexpected package claim: %#v", opportunity.WhyWeBelieveThis)
	}
	if len(opportunity.RawEvidence.Modules) != 0 {
		t.Fatalf("expected noisy cached package artifacts to be filtered, got %#v", opportunity.RawEvidence.Modules)
	}
	if !strings.Contains(opportunity.Hypothesis, "dependency install") {
		t.Fatalf("hypothesis should describe install failure, got %q", opportunity.Hypothesis)
	}
	if !strings.Contains(strings.Join(opportunity.RawEvidence.URLs, "\n"), "repo.yarnpkg.com") {
		t.Fatalf("expected URL evidence, got %#v", opportunity.RawEvidence.URLs)
	}
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
