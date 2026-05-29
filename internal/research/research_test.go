package research

import (
	"strings"
	"testing"

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
	if plan.Opportunities[0].ID != "job-instability" {
		t.Fatalf("expected grouped flakiness first, got %s", plan.Opportunities[0].ID)
	}
	if !strings.Contains(plan.Opportunities[0].Evidence, "frontend-unit-test") || !strings.Contains(plan.Opportunities[0].Evidence, "go-lint") {
		t.Fatalf("expected grouped flaky evidence, got %q", plan.Opportunities[0].Evidence)
	}
	for _, opportunity := range plan.Opportunities {
		if opportunity.ID == "runtime-characterization-tiny-runtime" {
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
