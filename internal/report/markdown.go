package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/rules"
	"github.com/autoci-ai/autoci/internal/scanner"
)

func WriteMarkdownFile(path string, workflows []scanner.Workflow, findings []rules.Finding) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteMarkdown(file, workflows, findings)
}

func WriteMarkdown(w io.Writer, workflows []scanner.Workflow, findings []rules.Finding) error {
	if _, err := fmt.Fprintln(w, "# Autoci Depot CI Analysis"); err != nil {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Workflows Detected\n\n")
	if len(workflows) == 0 {
		fmt.Fprintln(w, "No Depot CI workflow files were detected.")
	} else {
		for _, workflow := range workflows {
			fmt.Fprintf(w, "- `%s`\n", filepath.ToSlash(workflow.Path))
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Findings\n\n")
	if len(findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return nil
	}
	for _, finding := range findings {
		fmt.Fprintf(w, "### %s\n\n", finding.Title)
		fmt.Fprintf(w, "- ID: `%s`\n", finding.ID)
		fmt.Fprintf(w, "- Severity: `%s`\n", finding.Severity)
		fmt.Fprintf(w, "- File: `%s:%d`\n", filepath.ToSlash(finding.File), finding.Line)
		fmt.Fprintf(w, "- Evidence: %s\n", finding.Evidence)
		fmt.Fprintf(w, "- Recommendation: %s\n\n", finding.Recommendation)
	}
	return nil
}
