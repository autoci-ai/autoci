package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/rules"
)

func WriteMarkdownFile(path string, analysis rules.Analysis) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteMarkdown(file, analysis)
}

func WriteMarkdown(w io.Writer, analysis rules.Analysis) error {
	if _, err := fmt.Fprintln(w, "# Autoci Depot CI Analysis"); err != nil {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: `%s`\n", analysis.Workflow)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Findings\n\n")
	if len(analysis.Findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return nil
	}
	for _, finding := range analysis.Findings {
		fmt.Fprintf(w, "### %s\n\n", finding.Title)
		fmt.Fprintf(w, "- ID: `%s`\n", finding.ID)
		fmt.Fprintf(w, "- Severity: `%s`\n", finding.Severity)
		fmt.Fprintf(w, "- File: `%s:%d`\n", filepath.ToSlash(finding.File), finding.Line)
		fmt.Fprintf(w, "- Evidence: %s\n", finding.Evidence)
		fmt.Fprintf(w, "- Recommendation: %s\n\n", finding.Recommendation)
	}
	return nil
}
