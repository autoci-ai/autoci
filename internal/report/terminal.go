package report

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/rules"
)

func WriteTerminal(w io.Writer, analysis rules.Analysis) {
	fmt.Fprintf(w, "Autoci Depot CI analysis\n\n")
	fmt.Fprintf(w, "Workflow: %s\n", analysis.Workflow)
	fmt.Fprintf(w, "\nFindings: %d\n", len(analysis.Findings))
	if len(analysis.Findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return
	}
	for _, finding := range analysis.Findings {
		fmt.Fprintf(w, "\n[%s] %s (%s)\n", finding.Severity, finding.Title, finding.ID)
		fmt.Fprintf(w, "File: %s:%d\n", filepath.ToSlash(finding.File), finding.Line)
		fmt.Fprintf(w, "Evidence: %s\n", finding.Evidence)
		fmt.Fprintf(w, "Recommendation: %s\n", finding.Recommendation)
	}
}
