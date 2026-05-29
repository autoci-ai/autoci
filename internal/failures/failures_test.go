package failures

import "testing"

func TestAnalyzeGroupsFailuresByTheme(t *testing.T) {
	analysis := Analyze("pr.yml", 31, 7, []Observation{
		{Job: "go-lint", RunID: "run-1", Message: "image pull timeout"},
		{Job: "integration-test:matrix-03", RunID: "run-2", Message: "image pull failed"},
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
	if len(analysis.AggregationJobs) != 1 || analysis.AggregationJobs[0].Job != "gate" {
		t.Fatalf("aggregation jobs = %#v", analysis.AggregationJobs)
	}
}
