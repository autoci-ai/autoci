package report

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/scanner"
)

func WriteProfileTerminal(w io.Writer, discovered []scanner.Workflow, runtimeProfile *profile.Profile) {
	fmt.Fprintln(w, "AutoCI Depot runtime profile")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Local workflows discovered: %d\n", len(discovered))
	fmt.Fprintf(w, "Runtime workflows analyzed: %d\n", len(runtimeProfile.Workflows))
	if len(runtimeProfile.Workflows) == 0 {
		fmt.Fprintln(w, "\nNo Depot workflow history was found for this repository.")
		return
	}
	fmt.Fprintln(w, "\nWorkflow summary:")
	for _, workflow := range runtimeProfile.Workflows {
		fmt.Fprintf(w, "- %s: %d runs, avg %s, P95 %s, failures %.0f%%\n", workflow.Name, workflow.RunsAnalyzed, profile.FormatDuration(workflow.AvgDuration), profile.FormatDuration(workflow.P95Duration), workflow.FailureRate*100)
	}
	if len(runtimeProfile.Findings) == 0 {
		fmt.Fprintln(w, "\nOptimization opportunities: none with enough runtime evidence.")
		return
	}
	fmt.Fprintln(w, "\nOptimization opportunities:")
	for _, finding := range runtimeProfile.Findings {
		target := finding.Workflow
		if finding.Job != "" {
			target += " / " + finding.Job
		}
		fmt.Fprintf(w, "\n[%s] %s\n", finding.Severity, finding.Title)
		fmt.Fprintf(w, "Target: %s\n", target)
		fmt.Fprintf(w, "Evidence: %s\n", finding.Evidence)
		fmt.Fprintf(w, "Recommendation: %s\n", finding.Recommendation)
	}
}

func WriteProfileMarkdownFile(path string, discovered []scanner.Workflow, runtimeProfile *profile.Profile) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteProfileMarkdown(file, discovered, runtimeProfile)
}

func WriteProfileMarkdown(w io.Writer, discovered []scanner.Workflow, runtimeProfile *profile.Profile) error {
	fmt.Fprintln(w, "# AutoCI Depot Runtime Profile")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Workflow Summary")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Local workflows discovered: %d\n\n", len(discovered))
	if len(runtimeProfile.Workflows) == 0 {
		fmt.Fprintln(w, "No Depot workflow history was found for this repository.")
		return nil
	}
	fmt.Fprintln(w, "| Workflow | Runs | Avg Duration | P95 Duration | Failure Rate |")
	fmt.Fprintln(w, "| --- | ---: | ---: | ---: | ---: |")
	for _, workflow := range runtimeProfile.Workflows {
		fmt.Fprintf(w, "| %s | %d | %s | %s | %.0f%% |\n", workflow.Name, workflow.RunsAnalyzed, profile.FormatDuration(workflow.AvgDuration), profile.FormatDuration(workflow.P95Duration), workflow.FailureRate*100)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Top Slow Jobs")
	fmt.Fprintln(w)
	for _, job := range topJobs(runtimeProfile.Workflows, func(a, b profile.JobProfile) bool {
		return a.AvgDuration > b.AvgDuration
	}, 5) {
		fmt.Fprintf(w, "- **%s / %s**: avg %s, P95 %s, %.0f%% of measured job runtime\n", job.workflow, job.JobProfile.Name, profile.FormatDuration(job.JobProfile.AvgDuration), profile.FormatDuration(job.JobProfile.P95Duration), job.JobProfile.ContributionPct)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Top Flaky Jobs")
	fmt.Fprintln(w)
	flaky := topJobs(runtimeProfile.Workflows, func(a, b profile.JobProfile) bool {
		return a.FailureRate > b.FailureRate
	}, 5)
	wroteFlaky := false
	for _, job := range flaky {
		if job.JobProfile.FailureRate <= 0 {
			continue
		}
		wroteFlaky = true
		fmt.Fprintf(w, "- **%s / %s**: %.0f%% failure rate across %d runs\n", job.workflow, job.JobProfile.Name, job.JobProfile.FailureRate*100, job.JobProfile.RunsAnalyzed)
	}
	if !wroteFlaky {
		fmt.Fprintln(w, "No job failures were observed in the sampled history.")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Optimization Opportunities")
	fmt.Fprintln(w)
	if len(runtimeProfile.Findings) == 0 {
		fmt.Fprintln(w, "No optimization opportunities had enough runtime evidence.")
	} else {
		for _, finding := range runtimeProfile.Findings {
			target := finding.Workflow
			if finding.Job != "" {
				target += " / " + finding.Job
			}
			fmt.Fprintf(w, "### %s\n\n", finding.Title)
			fmt.Fprintf(w, "- Severity: `%s`\n", finding.Severity)
			fmt.Fprintf(w, "- Target: %s\n", target)
			fmt.Fprintf(w, "- Evidence: %s\n", finding.Evidence)
			fmt.Fprintf(w, "- Recommendation: %s\n\n", finding.Recommendation)
		}
	}
	fmt.Fprintln(w, "## Supporting Evidence")
	fmt.Fprintln(w)
	for _, workflow := range runtimeProfile.Workflows {
		fmt.Fprintf(w, "### %s\n\n", workflow.Name)
		fmt.Fprintln(w, "| Job | Runs | Avg Duration | P95 Duration | Failure Rate | Runtime Share | Range |")
		fmt.Fprintln(w, "| --- | ---: | ---: | ---: | ---: | ---: | ---: |")
		for _, job := range workflow.Jobs {
			fmt.Fprintf(w, "| %s | %d | %s | %s | %.0f%% | %.0f%% | %s-%s |\n", job.Name, job.RunsAnalyzed, profile.FormatDuration(job.AvgDuration), profile.FormatDuration(job.P95Duration), job.FailureRate*100, job.ContributionPct, profile.FormatDuration(job.MinDuration), profile.FormatDuration(job.MaxDuration))
		}
		fmt.Fprintln(w)
	}
	return nil
}

type rankedJob struct {
	workflow string
	profile.JobProfile
}

func topJobs(workflows []profile.WorkflowProfile, less func(a, b profile.JobProfile) bool, limit int) []rankedJob {
	var jobs []rankedJob
	for _, workflow := range workflows {
		for _, job := range workflow.Jobs {
			jobs = append(jobs, rankedJob{workflow: workflow.Name, JobProfile: job})
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		return less(jobs[i].JobProfile, jobs[j].JobProfile)
	})
	if len(jobs) > limit {
		return jobs[:limit]
	}
	return jobs
}
