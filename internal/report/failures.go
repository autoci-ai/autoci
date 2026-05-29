package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/autoci-ai/autoci/internal/failures"
)

func WriteFailuresTerminal(w io.Writer, analysis *failures.Analysis) {
	fmt.Fprintln(w, "Failure analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: %s\n", analysis.Workflow)
	fmt.Fprintf(w, "Runs analyzed: %d\n", analysis.RunsAnalyzed)
	fmt.Fprintf(w, "Failed runs: %d\n", analysis.FailedRuns)
	if len(analysis.FailureGroups) == 0 {
		fmt.Fprintln(w, "\nNo recurring failure groups found.")
		return
	}
	fmt.Fprintln(w, "\nTop failure groups:")
	for i, group := range analysis.FailureGroups {
		fmt.Fprintf(w, "\n%d. %s\n", i+1, group.Job)
		fmt.Fprintf(w, "   Occurrences: %d\n", group.Occurrences)
		fmt.Fprintf(w, "   Signature: %s\n", group.Signature)
		if group.ExampleRun != "" {
			fmt.Fprintf(w, "   Example run: %s\n", group.ExampleRun)
		}
		fmt.Fprintf(w, "   Recommendation: %s\n", group.Recommendation)
	}
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
	fmt.Fprintln(w, "## Failure Groups")
	fmt.Fprintln(w)
	if len(analysis.FailureGroups) == 0 {
		fmt.Fprintln(w, "No recurring failure groups found.")
		return nil
	}
	for _, group := range analysis.FailureGroups {
		fmt.Fprintf(w, "### %s\n\n", group.Signature)
		fmt.Fprintf(w, "- ID: `%s`\n", group.ID)
		fmt.Fprintf(w, "- Job: `%s`\n", group.Job)
		fmt.Fprintf(w, "- Occurrences: `%d`\n", group.Occurrences)
		if group.ExampleRun != "" {
			fmt.Fprintf(w, "- Example run: `%s`\n", group.ExampleRun)
		}
		fmt.Fprintf(w, "- Recommendation: %s\n\n", group.Recommendation)
	}
	return nil
}
