package provider

import (
	"testing"
	"time"

	"github.com/autoci-ai/autoci/internal/profile"
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

func TestFindingsRankAndClassifyRuntimeEvidence(t *testing.T) {
	workflows := []profile.WorkflowProfile{{
		Name:         "pr",
		RunsAnalyzed: 10,
		FailureRate:  0.20,
		Jobs: []profile.JobProfile{
			{Name: "gate", RunsAnalyzed: 10, FailureRate: 0.30, AvgDuration: time.Minute, ContributionPct: 1, IsAggregator: true, DependsOnCount: 5},
			{Name: "acceptance-tests", RunsAnalyzed: 10, FailureRate: 0.10, AvgDuration: 3 * time.Minute, ContributionPct: 12},
			{Name: "integration-tests", RunsAnalyzed: 10, AvgDuration: 17 * time.Minute, P95Duration: 28 * time.Minute, ContributionPct: 43},
			{Name: "docs", RunsAnalyzed: 10, AvgDuration: 7 * time.Minute, P95Duration: 8 * time.Minute, ContributionPct: 4},
			{Name: "cache-sensitive", RunsAnalyzed: 10, AvgDuration: 4 * time.Minute, MinDuration: 2 * time.Minute, MaxDuration: 24 * time.Minute, ContributionPct: 8},
		},
	}}

	findings := findingsFor(workflows)
	ids := make([]string, 0, len(findings))
	titles := map[string]string{}
	for _, finding := range findings {
		ids = append(ids, finding.ID)
		titles[finding.ID] = finding.Title
	}

	wantOrder := []string{"repeated-failures", "flaky-job", "failure-aggregation-job", "high-leverage-slow-job", "long-running-job", "high-variance"}
	for i, want := range wantOrder {
		if len(ids) <= i || ids[i] != want {
			t.Fatalf("finding order = %v, want prefix %v", ids, wantOrder)
		}
	}
	if titles["long-running-job"] != "Long-running job" {
		t.Fatalf("expected long-running title, got %q", titles["long-running-job"])
	}
	if titles["high-leverage-slow-job"] != "High-leverage slow job" {
		t.Fatalf("expected high-leverage title, got %q", titles["high-leverage-slow-job"])
	}
}

func TestAggregationJobIsNotClassifiedAsFlaky(t *testing.T) {
	workflows := []profile.WorkflowProfile{{
		Name: "pr",
		Jobs: []profile.JobProfile{
			{Name: "required-status", RunsAnalyzed: 5, FailureRate: 0.40, IsAggregator: true, DependsOnCount: 6},
		},
	}}

	findings := findingsFor(workflows)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %d", len(findings))
	}
	if findings[0].ID != "failure-aggregation-job" {
		t.Fatalf("expected aggregation finding, got %s", findings[0].ID)
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
