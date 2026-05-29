package failures

import (
	"regexp"
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
	ID             string            `json:"id"`
	Signature      string            `json:"signature"`
	Occurrences    int               `json:"occurrences"`
	Jobs           []string          `json:"jobs"`
	ExampleRun     string            `json:"exampleRun,omitempty"`
	Recommendation string            `json:"recommendation"`
	Artifacts      FailureArtifacts  `json:"artifacts,omitempty"`
	Evidence       []FailureEvidence `json:"evidence,omitempty"`
}

type FailureEvidence struct {
	RunID          string   `json:"runId,omitempty"`
	Job            string   `json:"job,omitempty"`
	LogExcerpt     string   `json:"logExcerpt,omitempty"`
	ExtractedItems []string `json:"extractedItems,omitempty"`
}

type FailureArtifacts struct {
	Images      []string `json:"images,omitempty"`
	Packages    []string `json:"packages,omitempty"`
	Modules     []string `json:"modules,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	Hosts       []string `json:"hosts,omitempty"`
	Dockerfiles []string `json:"dockerfiles,omitempty"`
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
		evidence := Evidence(observation)
		if len(evidence.ExtractedItems) > 0 || evidence.LogExcerpt != "" {
			theme.Evidence = append(theme.Evidence, evidence)
		}
		mergeArtifacts(&theme.Artifacts, ExtractArtifacts(observation.Message))
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

func Evidence(observation Observation) FailureEvidence {
	return FailureEvidence{
		RunID:          observation.RunID,
		Job:            observation.Job,
		LogExcerpt:     excerpt(observation.Message),
		ExtractedItems: ExtractItems(observation.Message),
	}
}

func ExtractItems(message string) []string {
	artifacts := ExtractArtifacts(message)
	var items []string
	items = append(items, artifacts.Images...)
	items = append(items, artifacts.Packages...)
	items = append(items, artifacts.Modules...)
	items = append(items, artifacts.URLs...)
	items = append(items, artifacts.Hosts...)
	items = append(items, artifacts.Dockerfiles...)
	return uniqueSorted(items)
}

func ExtractArtifacts(message string) FailureArtifacts {
	return FailureArtifacts{
		Images:      uniqueSorted(matches(message, imagePattern)),
		Packages:    uniqueSorted(packageMatches(message)),
		Modules:     uniqueSorted(matches(message, modulePattern)),
		URLs:        uniqueSorted(matches(message, urlPattern)),
		Hosts:       uniqueSorted(hostMatches(message)),
		Dockerfiles: uniqueSorted(matches(message, dockerfilePattern)),
	}
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

var (
	imagePattern      = regexp.MustCompile(`(?:docker://|ghcr\.io/|docker\.io/|quay\.io/|[a-zA-Z0-9.-]+(?::[0-9]+)?/)[a-zA-Z0-9._/-]+(?::[a-zA-Z0-9._-]+|@[a-zA-Z0-9:+._-]+)?`)
	urlPattern        = regexp.MustCompile(`https?://[^\s"'<>]+`)
	modulePattern     = regexp.MustCompile(`[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/[a-zA-Z0-9._~/-]+`)
	dockerfilePattern = regexp.MustCompile(`(?:^|\s)(?:\.?/)?Dockerfile(?:\.[a-zA-Z0-9._-]+)?`)
	packagePattern    = regexp.MustCompile(`(?:npm|yarn|pnpm)(?: ERR!| error)?[^@\n]*(@?[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+|[a-zA-Z0-9._-]+)`)
	hostPattern       = regexp.MustCompile(`(?:registry|host|url|from|to)[:= ]+(https?://)?([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})(?:[:/][^\s]*)?`)
)

func matches(message string, pattern *regexp.Regexp) []string {
	var result []string
	for _, match := range pattern.FindAllString(message, -1) {
		match = strings.TrimSpace(strings.Trim(match, `"'<>.,;:`))
		if match != "" {
			result = append(result, match)
		}
	}
	return result
}

func packageMatches(message string) []string {
	var result []string
	for _, match := range packagePattern.FindAllStringSubmatch(message, -1) {
		if len(match) > 1 && match[1] != "" && !isPackageNoise(match[1]) {
			result = append(result, match[1])
		}
	}
	return result
}

func hostMatches(message string) []string {
	var result []string
	for _, match := range hostPattern.FindAllStringSubmatch(message, -1) {
		if len(match) > 2 && match[2] != "" {
			result = append(result, match[2])
		}
	}
	for _, rawURL := range matches(message, urlPattern) {
		host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
		if idx := strings.IndexAny(host, "/:"); idx >= 0 {
			host = host[:idx]
		}
		if host != "" {
			result = append(result, host)
		}
	}
	return uniqueSorted(result)
}

func mergeArtifacts(dst *FailureArtifacts, src FailureArtifacts) {
	dst.Images = uniqueSorted(append(dst.Images, src.Images...))
	dst.Packages = uniqueSorted(append(dst.Packages, src.Packages...))
	dst.Modules = uniqueSorted(append(dst.Modules, src.Modules...))
	dst.URLs = uniqueSorted(append(dst.URLs, src.URLs...))
	dst.Hosts = uniqueSorted(append(dst.Hosts, src.Hosts...))
	dst.Dockerfiles = uniqueSorted(append(dst.Dockerfiles, src.Dockerfiles...))
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func excerpt(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, 240)
		}
	}
	return truncate(message, 240)
}

func isPackageNoise(value string) bool {
	switch strings.ToLower(value) {
	case "install", "failed", "failure", "error", "warning", "warn", "err":
		return true
	default:
		return false
	}
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
