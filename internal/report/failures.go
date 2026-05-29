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
		if verbose {
			writeFailureArtifacts(w, theme, "   ")
			writeFailureEvidence(w, theme, "   ")
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
		writeFailureArtifacts(w, theme, "")
		if len(theme.Evidence) > 0 {
			fmt.Fprintln(w, "- Evidence:")
			for _, evidence := range theme.Evidence {
				fmt.Fprintf(w, "  - Run `%s`, job `%s`: %s\n", evidence.RunID, evidence.Job, evidence.LogExcerpt)
			}
		}
		fmt.Fprintf(w, "- Recommendation: %s\n\n", theme.Recommendation)
	}
	return nil
}

func writeFailureArtifacts(w io.Writer, theme failures.FailureTheme, indent string) {
	groups := []struct {
		label string
		items []string
	}{
		{"Extracted image references", theme.Artifacts.Images},
		{"Extracted packages", theme.Artifacts.Packages},
		{"Extracted modules", theme.Artifacts.Modules},
		{"Extracted URLs", theme.Artifacts.URLs},
		{"Extracted hosts", theme.Artifacts.Hosts},
		{"Extracted Dockerfiles", theme.Artifacts.Dockerfiles},
	}
	for _, group := range groups {
		if len(group.items) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s%s:\n", indent, group.label)
		for _, item := range group.items {
			fmt.Fprintf(w, "%s- %s\n", indent, item)
		}
	}
}

func writeFailureEvidence(w io.Writer, theme failures.FailureTheme, indent string) {
	if len(theme.Evidence) == 0 {
		return
	}
	fmt.Fprintf(w, "%sExample evidence:\n", indent)
	limit := len(theme.Evidence)
	if limit > 3 {
		limit = 3
	}
	for _, evidence := range theme.Evidence[:limit] {
		fmt.Fprintf(w, "%s- Run: %s Job: %s\n", indent, evidence.RunID, evidence.Job)
		if evidence.LogExcerpt != "" {
			fmt.Fprintf(w, "%s  %s\n", indent, evidence.LogExcerpt)
		}
		if len(evidence.ExtractedItems) > 0 {
			fmt.Fprintf(w, "%s  Extracted: %v\n", indent, evidence.ExtractedItems)
		}
	}
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
