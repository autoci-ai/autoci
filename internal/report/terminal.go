package report

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/rules"
	"github.com/autoci-ai/autoci/internal/scanner"
)

func WriteTerminal(w io.Writer, workflows []scanner.Workflow, findings []rules.Finding) {
	fmt.Fprintf(w, "Autoci Depot CI analysis\n\n")
	fmt.Fprintf(w, "Workflows detected: %d\n", len(workflows))
	for _, workflow := range workflows {
		fmt.Fprintf(w, "- %s\n", filepath.ToSlash(workflow.Path))
	}
	fmt.Fprintf(w, "\nFindings: %d\n", len(findings))
	if len(findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return
	}
	for _, finding := range findings {
		fmt.Fprintf(w, "\n[%s] %s (%s)\n", finding.Severity, finding.Title, finding.ID)
		fmt.Fprintf(w, "File: %s:%d\n", filepath.ToSlash(finding.File), finding.Line)
		fmt.Fprintf(w, "Evidence: %s\n", finding.Evidence)
		fmt.Fprintf(w, "Recommendation: %s\n", finding.Recommendation)
	}
}
