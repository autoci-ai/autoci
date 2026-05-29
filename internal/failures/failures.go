package failures

import (
	"sort"
	"strings"
)

type Analysis struct {
	Workflow        string           `json:"workflow"`
	RunsAnalyzed    int              `json:"runsAnalyzed"`
	FailedRuns      int              `json:"failedRuns"`
	FailureThemes   []FailureTheme   `json:"failureThemes"`
	AggregationJobs []AggregationJob `json:"aggregationJobs,omitempty"`
}

type FailureTheme struct {
	ID             string   `json:"id"`
	Signature      string   `json:"signature"`
	Occurrences    int      `json:"occurrences"`
	Jobs           []string `json:"jobs"`
	ExampleRun     string   `json:"exampleRun,omitempty"`
	Recommendation string   `json:"recommendation"`
}

type AggregationJob struct {
	Job         string `json:"job"`
	Occurrences int    `json:"occurrences"`
}

type Observation struct {
	Job          string
	RunID        string
	Message      string
	IsAggregator bool
}

func Analyze(workflow string, runsAnalyzed, failedRuns int, observations []Observation) Analysis {
	themes := map[string]*themeBuilder{}
	aggregationJobs := map[string]int{}
	for _, observation := range observations {
		if IsAggregationJob(observation.Job) || observation.IsAggregator {
			aggregationJobs[observation.Job]++
			continue
		}
		signature := Signature(observation.Message)
		theme := themes[signature]
		if theme == nil {
			theme = &themeBuilder{
				FailureTheme: FailureTheme{
					ID:             "failure-theme-" + slug(signature),
					Signature:      signature,
					ExampleRun:     observation.RunID,
					Recommendation: Recommendation(signature),
				},
				jobs: map[string]bool{},
			}
			themes[signature] = theme
		}
		theme.Occurrences++
		if observation.Job != "" {
			theme.jobs[observation.Job] = true
		}
	}

	analysis := Analysis{
		Workflow:        workflow,
		RunsAnalyzed:    runsAnalyzed,
		FailedRuns:      failedRuns,
		FailureThemes:   []FailureTheme{},
		AggregationJobs: []AggregationJob{},
	}
	for _, theme := range themes {
		for job := range theme.jobs {
			theme.Jobs = append(theme.Jobs, job)
		}
		sort.Strings(theme.Jobs)
		analysis.FailureThemes = append(analysis.FailureThemes, theme.FailureTheme)
	}
	sort.SliceStable(analysis.FailureThemes, func(i, j int) bool {
		if analysis.FailureThemes[i].Occurrences == analysis.FailureThemes[j].Occurrences {
			return analysis.FailureThemes[i].ID < analysis.FailureThemes[j].ID
		}
		return analysis.FailureThemes[i].Occurrences > analysis.FailureThemes[j].Occurrences
	})
	for job, occurrences := range aggregationJobs {
		analysis.AggregationJobs = append(analysis.AggregationJobs, AggregationJob{Job: job, Occurrences: occurrences})
	}
	sort.SliceStable(analysis.AggregationJobs, func(i, j int) bool {
		if analysis.AggregationJobs[i].Occurrences == analysis.AggregationJobs[j].Occurrences {
			return analysis.AggregationJobs[i].Job < analysis.AggregationJobs[j].Job
		}
		return analysis.AggregationJobs[i].Occurrences > analysis.AggregationJobs[j].Occurrences
	})
	return analysis
}

type themeBuilder struct {
	FailureTheme
	jobs map[string]bool
}

func IsAggregationJob(job string) bool {
	lower := strings.ToLower(job)
	for _, term := range []string{"gate", "required", "status", "aggregate", "summary"} {
		if lower == term || strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func Signature(message string) string {
	lower := strings.ToLower(message)
	patterns := []struct {
		contains  []string
		signature string
	}{
		{[]string{"golangci-lint", "timeout"}, "golangci-lint timeout"},
		{[]string{"golangci-lint", "timed out"}, "golangci-lint timeout"},
		{[]string{"jest", "timeout"}, "jest timeout"},
		{[]string{"npm", "install"}, "npm install failure"},
		{[]string{"image", "pull"}, "image pull failure"},
		{[]string{"pull", "timeout"}, "image pull timeout"},
		{[]string{"context deadline exceeded"}, "context deadline exceeded"},
		{[]string{"permission denied"}, "permission denied"},
		{[]string{"no space left"}, "disk space exhausted"},
		{[]string{"connection refused"}, "connection refused"},
		{[]string{"timed out"}, "timeout"},
		{[]string{"timeout"}, "timeout"},
	}
	for _, pattern := range patterns {
		if containsAll(lower, pattern.contains) {
			return pattern.signature
		}
	}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, 80)
		}
	}
	return "unknown failure"
}

func Recommendation(signature string) string {
	switch signature {
	case "golangci-lint timeout":
		return "Investigate timeout thresholds and runtime regressions in the lint job."
	case "jest timeout":
		return "Investigate slow or hanging tests and whether test sharding is needed."
	case "npm install failure":
		return "Investigate package registry availability, lockfile consistency, and dependency cache behavior."
	case "image pull timeout", "image pull failure":
		return "Investigate registry availability, image caching, image pinning, and network reliability."
	case "context deadline exceeded", "timeout":
		return "Investigate timeout thresholds, external dependencies, and recent runtime regressions."
	case "permission denied":
		return "Investigate credentials, file permissions, and secret availability."
	case "disk space exhausted":
		return "Investigate workspace cleanup, build artifact size, and cache growth."
	default:
		return "Inspect recent failed runs and compare logs for recurring error messages."
	}
}

func containsAll(value string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func slug(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
