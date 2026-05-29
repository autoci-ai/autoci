package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/autoci-ai/autoci/internal/fix"
)

func WriteFixTerminal(w io.Writer, plan fix.Plan, dryRun bool) {
	if dryRun {
		fmt.Fprintln(w, "Fix plan generated")
	} else {
		fmt.Fprintln(w, "Fix generated")
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "ID: %s\n", plan.ID)
	fmt.Fprintf(w, "Branch: %s\n", plan.Branch)
	fmt.Fprintf(w, "Workflow: %s\n", plan.Workflow)
	fmt.Fprintf(w, "Hypothesis: %s\n", plan.Hypothesis)
	fmt.Fprintf(w, "Evidence: %s\n", plan.Evidence)
	fmt.Fprintf(w, "Change: %s\n", plan.ChangeSummary)
	fmt.Fprintf(w, "Success criteria: %s\n", plan.SuccessCriteria)
	fmt.Fprintf(w, "Confidence: %s\n", plan.Confidence)
	if dryRun && plan.Diff != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Diff:")
		fmt.Fprint(w, plan.Diff)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next step:")
	for _, command := range plan.Validation {
		fmt.Fprintf(w, "  %s\n", command)
	}
}

func WriteFixJSON(w io.Writer, plan fix.Plan) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}
