package provider

import (
	"testing"
	"time"
)

func TestBuilderAggregatesRuntimeEvidence(t *testing.T) {
	builder := newBuilder()
	builder.add(workflowDetail{
		Workflow: workflowNode{
			Name:       "ci",
			Status:     "finished",
			StartedAt:  "2026-05-28T10:00:00Z",
			FinishedAt: "2026-05-28T10:20:00Z",
		},
		Jobs: []jobNode{
			{JobKey: "ci.yml:unit", Status: "finished", StartedAt: "2026-05-28T10:00:00Z", FinishedAt: "2026-05-28T10:05:00Z"},
			{JobKey: "ci.yml:integration", Status: "finished", StartedAt: "2026-05-28T10:00:00Z", FinishedAt: "2026-05-28T10:20:00Z"},
		},
	})
	builder.add(workflowDetail{
		Workflow: workflowNode{
			Name:       "ci",
			Status:     "failed",
			StartedAt:  "2026-05-28T11:00:00Z",
			FinishedAt: "2026-05-28T11:30:00Z",
		},
		Jobs: []jobNode{
			{JobKey: "ci.yml:unit", Status: "finished", StartedAt: "2026-05-28T11:00:00Z", FinishedAt: "2026-05-28T11:05:00Z"},
			{JobKey: "ci.yml:integration", Status: "failed", StartedAt: "2026-05-28T11:00:00Z", FinishedAt: "2026-05-28T11:30:00Z"},
		},
	})

	result := builder.profile()
	if len(result.Workflows) != 1 {
		t.Fatalf("expected one workflow, got %d", len(result.Workflows))
	}
	workflow := result.Workflows[0]
	if workflow.RunsAnalyzed != 2 {
		t.Fatalf("expected two runs, got %d", workflow.RunsAnalyzed)
	}
	if workflow.AvgDuration != 25*time.Minute {
		t.Fatalf("expected avg duration 25m, got %s", workflow.AvgDuration)
	}
	if len(workflow.Jobs) != 2 {
		t.Fatalf("expected two jobs, got %d", len(workflow.Jobs))
	}
	if workflow.Jobs[0].Name != "integration" {
		t.Fatalf("expected integration to be top slow job, got %s", workflow.Jobs[0].Name)
	}
	if workflow.Jobs[0].ContributionPct < 80 {
		t.Fatalf("expected integration contribution over 80%%, got %.1f", workflow.Jobs[0].ContributionPct)
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected runtime findings")
	}
}

func TestParseGitHubRepo(t *testing.T) {
	tests := map[string]string{
		"git@github.com:autoci-ai/autoci.git":     "autoci-ai/autoci",
		"https://github.com/autoci-ai/autoci":     "autoci-ai/autoci",
		"https://github.com/autoci-ai/autoci.git": "autoci-ai/autoci",
	}
	for input, want := range tests {
		if got := parseGitHubRepo(input); got != want {
			t.Fatalf("parseGitHubRepo(%q) = %q, want %q", input, got, want)
		}
	}
}
