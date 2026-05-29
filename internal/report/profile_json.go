package report

import (
	"encoding/json"
	"io"
	"math"
	"strings"

	"github.com/autoci-ai/autoci/internal/profile"
)

type profileJSON struct {
	Workflow     string         `json:"workflow"`
	RunsAnalyzed int            `json:"runsAnalyzed"`
	Summary      profileSummary `json:"summary"`
	Findings     []findingJSON  `json:"findings"`
}

type profileSummary struct {
	FailureRate        float64 `json:"failureRate"`
	AvgDurationSeconds int64   `json:"avgDurationSeconds"`
	P95DurationSeconds int64   `json:"p95DurationSeconds"`
}

type findingJSON struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Score          int    `json:"score"`
	Title          string `json:"title"`
	Workflow       string `json:"workflow,omitempty"`
	Job            string `json:"job,omitempty"`
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation"`
}

func WriteProfileJSON(w io.Writer, workflowName string, runtimeProfile *profile.Profile) error {
	output := profileJSON{Workflow: workflowName, Findings: []findingJSON{}}
	if len(runtimeProfile.Workflows) > 0 {
		workflow := runtimeProfile.Workflows[0]
		output.RunsAnalyzed = workflow.RunsAnalyzed
		output.Summary = profileSummary{
			FailureRate:        workflow.FailureRate,
			AvgDurationSeconds: int64(workflow.AvgDuration.Seconds()),
			P95DurationSeconds: int64(workflow.P95Duration.Seconds()),
		}
	}
	for _, finding := range runtimeProfile.Findings {
		output.Findings = append(output.Findings, findingJSON{
			ID:             finding.ID,
			Severity:       finding.Severity,
			Score:          findingScore(finding),
			Title:          finding.Title,
			Workflow:       finding.Workflow,
			Job:            finding.Job,
			Evidence:       finding.Evidence,
			Recommendation: finding.Recommendation,
		})
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func findingScore(finding profile.Finding) int {
	score := map[string]int{
		"repeated-failures":       95,
		"flaky-job":               90,
		"failure-aggregation-job": 75,
		"high-leverage-slow-job":  70,
		"long-running-job":        45,
		"high-variance":           40,
		"critical-path-blocker":   80,
	}[findingKind(finding.ID)]
	if score == 0 {
		score = 50
	}
	switch finding.Severity {
	case "high":
		score += 5
	case "low":
		score -= 5
	}
	return int(math.Max(0, math.Min(100, float64(score))))
}

func findingKind(id string) string {
	for _, kind := range []string{
		"repeated-failures",
		"flaky-job",
		"failure-aggregation-job",
		"high-leverage-slow-job",
		"long-running-job",
		"high-variance",
		"critical-path-blocker",
	} {
		if id == kind || strings.HasPrefix(id, kind+"-") {
			return kind
		}
	}
	return id
}
