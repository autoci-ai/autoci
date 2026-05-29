package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/autoci-ai/autoci/internal/findings"
)

func WriteFindingsJSON(w io.Writer, items []findings.Finding) error {
	if items == nil {
		items = []findings.Finding{}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(items)
}

func WriteFindingsTerminal(w io.Writer, items []findings.Finding) {
	if len(items) == 0 {
		fmt.Fprintln(w, "No cached AutoCI findings found.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Run `autoci failures` or `autoci profile` first.")
		return
	}
	currentHeader := ""
	for i, item := range items {
		header := strings.ToUpper(item.Priority + " " + item.Category)
		if header != currentHeader {
			if currentHeader != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, header)
			fmt.Fprintln(w)
			currentHeader = header
		}
		fmt.Fprintf(w, "%d. %s\n\n", i+1, item.ID)
		fmt.Fprintf(w, "Source: %s\n", item.Source)
		fmt.Fprintf(w, "Status: %s\n", item.Status)
		if item.Evidence != "" {
			fmt.Fprintf(w, "Evidence: %s\n", item.Evidence)
		}
		if len(item.Gaps) > 0 {
			fmt.Fprintln(w, "Gaps:")
			for _, gap := range item.Gaps {
				fmt.Fprintf(w, "- %s\n", gap.Message)
			}
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintf(w, "  %s\n", item.NextCommand)
		if i != len(items)-1 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "---")
			fmt.Fprintln(w)
		}
	}
}
