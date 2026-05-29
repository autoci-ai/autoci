package failures

import "testing"

func TestAnalyzeGroupsFailuresByJobAndSignature(t *testing.T) {
	analysis := Analyze("pr.yml", 31, 7, []Observation{
		{Job: "go-lint", RunID: "run-1", Message: "golangci-lint timed out after 10m"},
		{Job: "go-lint", RunID: "run-2", Message: "golangci-lint timeout"},
		{Job: "frontend-unit-test", RunID: "run-3", Message: "jest timeout exceeded"},
	})

	if len(analysis.FailureGroups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(analysis.FailureGroups))
	}
	if analysis.FailureGroups[0].Job != "go-lint" {
		t.Fatalf("expected go-lint first, got %s", analysis.FailureGroups[0].Job)
	}
	if analysis.FailureGroups[0].Signature != "golangci-lint timeout" {
		t.Fatalf("signature = %q", analysis.FailureGroups[0].Signature)
	}
	if analysis.FailureGroups[0].ID != "failure-group-go-lint-golangci-lint-timeout" {
		t.Fatalf("id = %q", analysis.FailureGroups[0].ID)
	}
}
