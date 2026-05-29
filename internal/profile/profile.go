package profile

import (
	"fmt"
	"time"
)

type Profile struct {
	Workflows []WorkflowProfile
	Findings  []Finding
}

type WorkflowProfile struct {
	Name         string
	Path         string
	RunsAnalyzed int
	AvgDuration  time.Duration
	P95Duration  time.Duration
	FailureRate  float64
	Jobs         []JobProfile
}

type JobProfile struct {
	Name            string
	RunsAnalyzed    int
	AvgDuration     time.Duration
	P95Duration     time.Duration
	FailureRate     float64
	ContributionPct float64
	MinDuration     time.Duration
	MaxDuration     time.Duration
}

type Finding struct {
	ID             string
	Title          string
	Severity       string
	Workflow       string
	Job            string
	Evidence       string
	Recommendation string
}

func FormatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	duration = duration.Round(time.Second)
	minutes := int(duration.Minutes())
	seconds := int(duration.Seconds()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
