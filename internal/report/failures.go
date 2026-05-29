package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/autoci-ai/autoci/internal/failures"
)

func WriteFailuresTerminal(w io.Writer, analysis *failures.Analysis, verbose bool) {
	fmt.Fprintln(w, "Failure analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: %s\n", analysis.Workflow)
	fmt.Fprintf(w, "Runs analyzed: %d\n", analysis.RunsAnalyzed)
	fmt.Fprintf(w, "Failed runs: %d\n", analysis.FailedRuns)
	if len(analysis.FailureThemes) == 0 {
		fmt.Fprintln(w, "\nNo recurring failure themes found.")
		writeAggregationJobs(w, analysis, verbose)
		return
	}
	fmt.Fprintln(w, "\nTop failure themes:")
	for i, theme := range analysis.FailureThemes {
		fmt.Fprintf(w, "\n%d. %s\n", i+1, theme.Signature)
		fmt.Fprintf(w, "   ID: %s\n", theme.ID)
		fmt.Fprintf(w, "   Occurrences: %d\n", theme.Occurrences)
		if len(theme.Jobs) > 0 {
			fmt.Fprintln(w, "   Jobs:")
			for _, job := range theme.Jobs {
				fmt.Fprintf(w, "   - %s\n", job)
			}
		}
		if theme.ExampleRun != "" {
			fmt.Fprintf(w, "   Example run: %s\n", theme.ExampleRun)
		}
		fmt.Fprintf(w, "   Recommendation: %s\n", theme.Recommendation)
	}
	writeAggregationJobs(w, analysis, verbose)
}

func WriteFailuresJSON(w io.Writer, analysis *failures.Analysis) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(analysis)
}

func WriteFailuresMarkdownFile(path string, analysis *failures.Analysis) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteFailuresMarkdown(file, analysis)
}

func WriteFailuresMarkdown(w io.Writer, analysis *failures.Analysis) error {
	fmt.Fprintln(w, "# AutoCI Failure Analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: `%s`\n\n", analysis.Workflow)
	fmt.Fprintf(w, "Runs analyzed: `%d`\n\n", analysis.RunsAnalyzed)
	fmt.Fprintf(w, "Failed runs: `%d`\n\n", analysis.FailedRuns)
	fmt.Fprintln(w, "## Failure Themes")
	fmt.Fprintln(w)
	if len(analysis.FailureThemes) == 0 {
		fmt.Fprintln(w, "No recurring failure themes found.")
		return nil
	}
	for _, theme := range analysis.FailureThemes {
		fmt.Fprintf(w, "### %s\n\n", theme.Signature)
		fmt.Fprintf(w, "- ID: `%s`\n", theme.ID)
		fmt.Fprintf(w, "- Occurrences: `%d`\n", theme.Occurrences)
		if len(theme.Jobs) > 0 {
			fmt.Fprintln(w, "- Jobs:")
			for _, job := range theme.Jobs {
				fmt.Fprintf(w, "  - `%s`\n", job)
			}
		}
		if theme.ExampleRun != "" {
			fmt.Fprintf(w, "- Example run: `%s`\n", theme.ExampleRun)
		}
		fmt.Fprintf(w, "- Recommendation: %s\n\n", theme.Recommendation)
	}
	return nil
}

func writeAggregationJobs(w io.Writer, analysis *failures.Analysis, verbose bool) {
	if len(analysis.AggregationJobs) == 0 {
		return
	}
	if !verbose {
		fmt.Fprintf(w, "\n%d failure aggregation jobs were excluded from root-cause ranking. Use --verbose to show them.\n", len(analysis.AggregationJobs))
		return
	}
	fmt.Fprintln(w, "\nFailure aggregation jobs detected:")
	for _, job := range analysis.AggregationJobs {
		fmt.Fprintf(w, "- %s (%d occurrences)\n", job.Job, job.Occurrences)
	}
	fmt.Fprintln(w, "These jobs reflect upstream failures and were excluded from root-cause ranking.")
}
