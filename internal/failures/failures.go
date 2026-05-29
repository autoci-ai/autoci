package failures

import (
	"sort"
	"strings"
)

type Analysis struct {
	Workflow      string         `json:"workflow"`
	RunsAnalyzed  int            `json:"runsAnalyzed"`
	FailedRuns    int            `json:"failedRuns"`
	FailureGroups []FailureGroup `json:"failureGroups"`
}

type FailureGroup struct {
	ID             string `json:"id"`
	Job            string `json:"job"`
	Signature      string `json:"signature"`
	Occurrences    int    `json:"occurrences"`
	ExampleRun     string `json:"exampleRun,omitempty"`
	Recommendation string `json:"recommendation"`
}

type Observation struct {
	Job     string
	RunID   string
	Message string
}

func Analyze(workflow string, runsAnalyzed, failedRuns int, observations []Observation) Analysis {
	groups := map[string]*FailureGroup{}
	for _, observation := range observations {
		signature := Signature(observation.Message)
		key := observation.Job + "\x00" + signature
		group := groups[key]
		if group == nil {
			group = &FailureGroup{
				ID:             "failure-group-" + slug(observation.Job+"-"+signature),
				Job:            observation.Job,
				Signature:      signature,
				ExampleRun:     observation.RunID,
				Recommendation: Recommendation(signature, observation.Job),
			}
			groups[key] = group
		}
		group.Occurrences++
	}

	analysis := Analysis{
		Workflow:      workflow,
		RunsAnalyzed:  runsAnalyzed,
		FailedRuns:    failedRuns,
		FailureGroups: []FailureGroup{},
	}
	for _, group := range groups {
		analysis.FailureGroups = append(analysis.FailureGroups, *group)
	}
	sort.SliceStable(analysis.FailureGroups, func(i, j int) bool {
		if analysis.FailureGroups[i].Occurrences == analysis.FailureGroups[j].Occurrences {
			return analysis.FailureGroups[i].ID < analysis.FailureGroups[j].ID
		}
		return analysis.FailureGroups[i].Occurrences > analysis.FailureGroups[j].Occurrences
	})
	return analysis
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

func Recommendation(signature, job string) string {
	switch signature {
	case "golangci-lint timeout":
		return "Investigate timeout thresholds and runtime regressions in the lint job."
	case "jest timeout":
		return "Investigate slow or hanging tests and whether test sharding is needed."
	case "npm install failure":
		return "Investigate package registry availability, lockfile consistency, and dependency cache behavior."
	case "image pull timeout", "image pull failure":
		return "Investigate registry availability and whether the image should be cached or pinned."
	case "context deadline exceeded", "timeout":
		return "Investigate timeout thresholds, external dependencies, and recent runtime regressions."
	case "permission denied":
		return "Investigate credentials, file permissions, and secret availability for this job."
	case "disk space exhausted":
		return "Investigate workspace cleanup, build artifact size, and cache growth."
	default:
		if job != "" {
			return "Inspect recent logs for this job and compare failing runs for a recurring signature."
		}
		return "Inspect recent failed runs and group logs by recurring error messages."
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
