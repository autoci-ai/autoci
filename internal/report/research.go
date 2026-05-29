package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

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
	fmt.Fprintf(w, "Why now: %s\n", plan.TopRecommendation.WhyNow)
	if len(plan.Opportunities) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Research opportunities:")
	for _, opportunity := range plan.Opportunities {
		fmt.Fprintf(w, "\n%s\n", opportunity.Title)
		fmt.Fprintf(w, "ID: %s\n", opportunity.ID)
		fmt.Fprintf(w, "Hypothesis: %s\n", opportunity.Hypothesis)
		fmt.Fprintf(w, "Evidence: %s\n", opportunity.Evidence)
		fmt.Fprintf(w, "Experiment: %s\n", opportunity.Experiment)
		fmt.Fprintf(w, "Success criteria: %s\n", opportunity.SuccessCriteria)
		fmt.Fprintf(w, "Risk: %s\n", opportunity.Risk)
		fmt.Fprintf(w, "Estimated impact: %s\n", opportunity.EstimatedImpact)
		fmt.Fprintln(w, "Suggested commands:")
		for _, command := range opportunity.SuggestedCommands {
			fmt.Fprintf(w, "- %s\n", command)
		}
	}
	if plan.HiddenCount > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%d additional research opportunities hidden. Use --verbose to show the complete backlog.\n", plan.HiddenCount)
	}
}

func WriteResearchJSON(w io.Writer, plan research.Plan) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

func WriteResearchMarkdownFile(path string, plan research.Plan) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return WriteResearchMarkdown(file, plan)
}

func WriteResearchMarkdown(w io.Writer, plan research.Plan) error {
	fmt.Fprintln(w, "# AutoCI Research Plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Workflow: `%s`\n\n", plan.Workflow)
	fmt.Fprintf(w, "Runs analyzed: `%d`\n\n", plan.RunsAnalyzed)
	fmt.Fprintln(w, "## Top Recommendation")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**%s**\n\n", plan.TopRecommendation.Title)
	fmt.Fprintf(w, "- Reason: %s\n", plan.TopRecommendation.Reason)
	fmt.Fprintf(w, "- Expected value: %s\n", plan.TopRecommendation.ExpectedValue)
	fmt.Fprintf(w, "- Why now: %s\n\n", plan.TopRecommendation.WhyNow)
	fmt.Fprintln(w, "## Research Opportunities")
	fmt.Fprintln(w)
	if len(plan.Opportunities) == 0 {
		fmt.Fprintln(w, "No high-value research opportunities were identified from the sampled history.")
		return nil
	}
	for _, opportunity := range plan.Opportunities {
		fmt.Fprintf(w, "### %s\n\n", opportunity.Title)
		fmt.Fprintf(w, "- ID: `%s`\n", opportunity.ID)
		fmt.Fprintf(w, "- Hypothesis: %s\n", opportunity.Hypothesis)
		fmt.Fprintf(w, "- Evidence: %s\n", opportunity.Evidence)
		fmt.Fprintf(w, "- Experiment: %s\n", opportunity.Experiment)
		fmt.Fprintf(w, "- Success criteria: %s\n", opportunity.SuccessCriteria)
		fmt.Fprintf(w, "- Risk: %s\n", opportunity.Risk)
		fmt.Fprintf(w, "- Estimated impact: %s\n", opportunity.EstimatedImpact)
		fmt.Fprintln(w, "- Suggested commands:")
		for _, command := range opportunity.SuggestedCommands {
			fmt.Fprintf(w, "  - `%s`\n", command)
		}
		fmt.Fprintln(w)
	}
	if plan.HiddenCount > 0 {
		fmt.Fprintf(w, "_%d additional research opportunities hidden. Use `--verbose` to show the complete backlog._\n", plan.HiddenCount)
	}
	return nil
}
