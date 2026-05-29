package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/scanner"
)

type DepotProvider struct {
	Repo           string
	RepoPath       string
	Limit          int
	LocalWorkflows []scanner.Workflow
}

func (p DepotProvider) Profile(ctx context.Context) (*profile.Profile, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	repo := p.Repo
	if repo == "" {
		repo = detectGitHubRepo(ctx, p.RepoPath)
	}

	workflows, err := p.listWorkflows(ctx, repo, limit)
	if err != nil {
		return nil, err
	}
	workflows = p.filterToLocalWorkflows(workflows)
	if len(workflows) == 0 {
		return &profile.Profile{}, nil
	}

	builder := newBuilder()
	for _, item := range workflows {
		detail, err := p.showWorkflow(ctx, item.WorkflowID)
		if err != nil {
			return nil, err
		}
		builder.add(detail)
	}
	return builder.profile(), nil
}

func (p DepotProvider) listWorkflows(ctx context.Context, repo string, limit int) ([]workflowListItem, error) {
	args := []string{"ci", "workflow", "list", "--output", "json", "-n", fmt.Sprintf("%d", limit)}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	out, err := runDepot(ctx, p.RepoPath, args...)
	if err != nil {
		return nil, err
	}
	var items []workflowListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse depot workflow list JSON: %w", err)
	}
	return items, nil
}

func (p DepotProvider) showWorkflow(ctx context.Context, workflowID string) (workflowDetail, error) {
	out, err := runDepot(ctx, p.RepoPath, "ci", "workflow", "show", workflowID, "--output", "json")
	if err != nil {
		return workflowDetail{}, err
	}
	var detail workflowDetail
	if err := json.Unmarshal(out, &detail); err != nil {
		return workflowDetail{}, fmt.Errorf("parse depot workflow show JSON: %w", err)
	}
	return detail, nil
}

func (p DepotProvider) filterToLocalWorkflows(items []workflowListItem) []workflowListItem {
	if len(p.LocalWorkflows) == 0 {
		return items
	}
	paths := map[string]bool{}
	for _, workflow := range p.LocalWorkflows {
		path := strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(workflow.Path, "\\", "/")), "./")
		paths[path] = true
		paths[strings.TrimPrefix(path, ".depot/workflows/")] = true
		paths[strings.TrimPrefix(path, ".depot/")] = true
	}
	var filtered []workflowListItem
	for _, item := range items {
		path := strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(item.WorkflowPath, "\\", "/")), "./")
		if paths[path] || paths[strings.TrimPrefix(path, ".depot/workflows/")] || paths[strings.TrimPrefix(path, ".depot/")] {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return items
	}
	return filtered
}

func runDepot(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "depot", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func detectGitHubRepo(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseGitHubRepo(strings.TrimSpace(string(out)))
}

func parseGitHubRepo(remote string) string {
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		return strings.TrimPrefix(remote, "git@github.com:")
	case strings.HasPrefix(remote, "https://github.com/"):
		return strings.TrimPrefix(remote, "https://github.com/")
	default:
		return ""
	}
}

type workflowListItem struct {
	WorkflowID   string `json:"workflow_id"`
	Name         string `json:"name"`
	WorkflowPath string `json:"workflow_path"`
}

type workflowDetail struct {
	Workflow workflowNode `json:"workflow"`
	Jobs     []jobNode    `json:"jobs"`
}

type workflowNode struct {
	Name         string `json:"name"`
	WorkflowPath string `json:"workflow_path"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	CreatedAt    string `json:"created_at"`
}

type jobNode struct {
	JobKey     string        `json:"job_key"`
	Status     string        `json:"status"`
	StartedAt  string        `json:"started_at"`
	FinishedAt string        `json:"finished_at"`
	Attempts   []attemptNode `json:"attempts"`
}

type attemptNode struct {
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

type builder struct {
	workflows map[string]*workflowAggregate
}

type workflowAggregate struct {
	name      string
	path      string
	durations []time.Duration
	failures  int
	jobs      map[string]*jobAggregate
}

type jobAggregate struct {
	name      string
	durations []time.Duration
	failures  int
}

func newBuilder() *builder {
	return &builder{workflows: map[string]*workflowAggregate{}}
}

func (b *builder) add(detail workflowDetail) {
	name := detail.Workflow.Name
	if name == "" {
		name = detail.Workflow.WorkflowPath
	}
	if name == "" {
		name = "unknown workflow"
	}
	key := name
	aggregate := b.workflows[key]
	if aggregate == nil {
		aggregate = &workflowAggregate{name: name, path: detail.Workflow.WorkflowPath, jobs: map[string]*jobAggregate{}}
		b.workflows[key] = aggregate
	}
	if duration := durationBetween(detail.Workflow.StartedAt, detail.Workflow.FinishedAt); duration > 0 {
		aggregate.durations = append(aggregate.durations, duration)
	}
	if isFailure(detail.Workflow.Status) {
		aggregate.failures++
	}
	for _, job := range detail.Jobs {
		jobName := normalizeJobName(job.JobKey, detail.Workflow.WorkflowPath)
		jobAgg := aggregate.jobs[jobName]
		if jobAgg == nil {
			jobAgg = &jobAggregate{name: jobName}
			aggregate.jobs[jobName] = jobAgg
		}
		if duration := jobDuration(job); duration > 0 {
			jobAgg.durations = append(jobAgg.durations, duration)
		}
		if isFailure(job.Status) {
			jobAgg.failures++
		}
	}
}

func normalizeJobName(jobKey, workflowPath string) string {
	if workflowPath != "" {
		if name := strings.TrimPrefix(jobKey, workflowPath+":"); name != jobKey && name != "" {
			return name
		}
	}
	if _, name, ok := strings.Cut(jobKey, ":"); ok && name != "" {
		return name
	}
	if jobKey == "" {
		return "unknown job"
	}
	return jobKey
}

func (b *builder) profile() *profile.Profile {
	var workflows []profile.WorkflowProfile
	for _, aggregate := range b.workflows {
		workflow := profile.WorkflowProfile{
			Name:         aggregate.name,
			Path:         aggregate.path,
			RunsAnalyzed: len(aggregate.durations),
			AvgDuration:  avg(aggregate.durations),
			P95Duration:  percentile(aggregate.durations, 0.95),
			FailureRate:  rate(aggregate.failures, len(aggregate.durations)),
		}
		var totalAvg time.Duration
		for _, job := range aggregate.jobs {
			totalAvg += avg(job.durations)
		}
		for _, job := range aggregate.jobs {
			jobProfile := profile.JobProfile{
				Name:         job.name,
				RunsAnalyzed: len(job.durations),
				AvgDuration:  avg(job.durations),
				P95Duration:  percentile(job.durations, 0.95),
				FailureRate:  rate(job.failures, len(job.durations)),
				MinDuration:  min(job.durations),
				MaxDuration:  max(job.durations),
			}
			if totalAvg > 0 {
				jobProfile.ContributionPct = float64(jobProfile.AvgDuration) / float64(totalAvg) * 100
			}
			workflow.Jobs = append(workflow.Jobs, jobProfile)
		}
		sort.Slice(workflow.Jobs, func(i, j int) bool {
			return workflow.Jobs[i].AvgDuration > workflow.Jobs[j].AvgDuration
		})
		workflows = append(workflows, workflow)
	}
	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].AvgDuration > workflows[j].AvgDuration
	})
	result := &profile.Profile{Workflows: workflows}
	result.Findings = findingsFor(workflows)
	return result
}

func jobDuration(job jobNode) time.Duration {
	if duration := durationBetween(job.StartedAt, job.FinishedAt); duration > 0 {
		return duration
	}
	var total time.Duration
	for _, attempt := range job.Attempts {
		total += durationBetween(attempt.StartedAt, attempt.FinishedAt)
	}
	return total
}

func durationBetween(start, finish string) time.Duration {
	startedAt, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0
	}
	finishedAt, err := time.Parse(time.RFC3339, finish)
	if err != nil {
		return 0
	}
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}

func isFailure(status string) bool {
	switch strings.ToLower(status) {
	case "failed", "failure", "timed_out", "cancelled":
		return true
	default:
		return false
	}
}

func avg(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	var total time.Duration
	for _, value := range values {
		total += value
	}
	return total / time.Duration(len(values))
}

func percentile(values []time.Duration, pct float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(float64(len(sorted))*pct)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func min(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func max(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func rate(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func findingsFor(workflows []profile.WorkflowProfile) []profile.Finding {
	var findings []profile.Finding
	for _, workflow := range workflows {
		if workflow.RunsAnalyzed >= 2 && workflow.AvgDuration >= 10*time.Minute {
			findings = append(findings, profile.Finding{
				ID:             "slow-workflow",
				Title:          "Slow workflow",
				Severity:       "medium",
				Workflow:       workflow.Name,
				Evidence:       fmt.Sprintf("Average duration %s across %d runs; P95 %s.", profile.FormatDuration(workflow.AvgDuration), workflow.RunsAnalyzed, profile.FormatDuration(workflow.P95Duration)),
				Recommendation: "Focus first on the slowest jobs in this workflow; they set the practical optimization ceiling.",
			})
		}
		if workflow.RunsAnalyzed >= 3 && workflow.FailureRate >= 0.10 {
			findings = append(findings, profile.Finding{
				ID:             "repeated-failures",
				Title:          "Repeated workflow failures",
				Severity:       "high",
				Workflow:       workflow.Name,
				Evidence:       fmt.Sprintf("Failure rate %.0f%% across %d runs.", workflow.FailureRate*100, workflow.RunsAnalyzed),
				Recommendation: "Stabilize recurring workflow failures before optimizing speed.",
			})
		}
		if len(workflow.Jobs) == 0 {
			continue
		}
		top := workflow.Jobs[0]
		if top.RunsAnalyzed >= 2 && top.ContributionPct >= 40 {
			findings = append(findings, profile.Finding{
				ID:             "runtime-concentration",
				Title:          "Runtime concentrated in one job",
				Severity:       "high",
				Workflow:       workflow.Name,
				Job:            top.Name,
				Evidence:       fmt.Sprintf("%s consumes %.0f%% of measured job runtime; average %s, P95 %s.", top.Name, top.ContributionPct, profile.FormatDuration(top.AvgDuration), profile.FormatDuration(top.P95Duration)),
				Recommendation: "Treat this as the highest leverage optimization target.",
			})
		}
		for _, job := range workflow.Jobs {
			if job.RunsAnalyzed >= 2 && job.AvgDuration >= 5*time.Minute {
				findings = append(findings, profile.Finding{
					ID:             "slow-job",
					Title:          "Slow job",
					Severity:       "medium",
					Workflow:       workflow.Name,
					Job:            job.Name,
					Evidence:       fmt.Sprintf("Average duration %s; P95 %s; consumes %.0f%% of measured job runtime.", profile.FormatDuration(job.AvgDuration), profile.FormatDuration(job.P95Duration), job.ContributionPct),
					Recommendation: "Investigate sharding, dependency setup, and expensive test groups for this job.",
				})
			}
			if job.RunsAnalyzed >= 3 && job.FailureRate >= 0.05 {
				findings = append(findings, profile.Finding{
					ID:             "flaky-job",
					Title:          "Flaky job",
					Severity:       "high",
					Workflow:       workflow.Name,
					Job:            job.Name,
					Evidence:       fmt.Sprintf("Failure rate %.0f%% across %d runs.", job.FailureRate*100, job.RunsAnalyzed),
					Recommendation: "Investigate instability before adding retries.",
				})
			}
			if job.RunsAnalyzed >= 3 && job.MinDuration > 0 && job.MaxDuration >= 3*job.MinDuration && job.MaxDuration-job.MinDuration >= 5*time.Minute {
				findings = append(findings, profile.Finding{
					ID:             "high-variance",
					Title:          "High runtime variance",
					Severity:       "medium",
					Workflow:       workflow.Name,
					Job:            job.Name,
					Evidence:       fmt.Sprintf("Duration varies between %s and %s.", profile.FormatDuration(job.MinDuration), profile.FormatDuration(job.MaxDuration)),
					Recommendation: "Investigate cache effectiveness and external dependencies for this job.",
				})
			}
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return severityRank(findings[i].Severity) > severityRank(findings[j].Severity)
	})
	return findings
}

func severityRank(severity string) int {
	switch severity {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
