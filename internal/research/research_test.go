package research

import (
	"strings"
	"testing"

	"github.com/autoci-ai/autoci/internal/profile"
)

func TestFromProfileBuildsOpportunitiesFromFindings(t *testing.T) {
	plan := FromProfile("pr.yml", &profile.Profile{
		Workflows: []profile.WorkflowProfile{{RunsAnalyzed: 10}},
		Findings: []profile.Finding{{
			ID:       "flaky-job",
			Workflow: "pr",
			Job:      "frontend-unit-test",
			Evidence: "Failure rate 10% across 10 runs.",
		}},
	})

	if plan.Workflow != "pr.yml" {
		t.Fatalf("workflow = %q", plan.Workflow)
	}
	if plan.RunsAnalyzed != 10 {
		t.Fatalf("runs analyzed = %d", plan.RunsAnalyzed)
	}
	if len(plan.Opportunities) != 1 {
		t.Fatalf("expected one opportunity, got %d", len(plan.Opportunities))
	}
	opportunity := plan.Opportunities[0]
	if opportunity.ID != "research-flaky-frontend-unit-test" {
		t.Fatalf("opportunity id = %q", opportunity.ID)
	}
	if !strings.Contains(opportunity.Title, "frontend-unit-test") {
		t.Fatalf("title should include job name: %q", opportunity.Title)
	}
	if opportunity.Experiment == "" || opportunity.SuccessCriteria == "" {
		t.Fatalf("opportunity should include experiment and success criteria: %#v", opportunity)
	}
	if plan.TopRecommendation.Title != opportunity.Title {
		t.Fatalf("top recommendation should use first opportunity")
	}
}
