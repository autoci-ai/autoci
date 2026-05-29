package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/autoci-ai/autoci/internal/research"
)

func WriteResearchTerminal(w io.Writer, plan research.Plan) {
	fmt.Fprintln(w, "AutoCI research plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: %s\n", plan.Workflow)
	fmt.Fprintf(w, "Runs analyzed: %d\n", plan.RunsAnalyzed)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Top research opportunity:")
	fmt.Fprintf(w, "%s\n", plan.TopRecommendation.Title)
	fmt.Fprintf(w, "Reason: %s\n", plan.TopRecommendation.Reason)
	fmt.Fprintf(w, "Expected value: %s\n", plan.TopRecommendation.ExpectedValue)
	if len(plan.Opportunities) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Research opportunities:")
	for _, opportunity := range plan.Opportunities {
		fmt.Fprintf(w, "\n%s\n", opportunity.Title)
		fmt.Fprintf(w, "Hypothesis: %s\n", opportunity.Hypothesis)
		fmt.Fprintf(w, "Evidence: %s\n", opportunity.Evidence)
		fmt.Fprintf(w, "Experiment: %s\n", opportunity.Experiment)
		fmt.Fprintf(w, "Success criteria: %s\n", opportunity.SuccessCriteria)
		fmt.Fprintf(w, "Risk: %s\n", opportunity.Risk)
		fmt.Fprintf(w, "Estimated impact: %s\n", opportunity.EstimatedImpact)
	}
}

func WriteResearchJSON(w io.Writer, plan research.Plan) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}
