package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/autoci-ai/autoci/internal/fix"
)

func WriteFixTerminal(w io.Writer, plan fix.Plan, dryRun bool) {
	if plan.PatchGenerated && !dryRun {
		fmt.Fprintln(w, "Fix generated")
	} else if plan.PatchGenerated {
		fmt.Fprintln(w, "Fix plan generated")
	} else {
		fmt.Fprintln(w, "Fix plan generated")
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "ID: %s\n", plan.ID)
	fmt.Fprintf(w, "Source ID: %s\n", plan.SourceID)
	if plan.Branch != "" {
		fmt.Fprintf(w, "Branch: %s\n", plan.Branch)
	}
	fmt.Fprintf(w, "Workflow: %s\n", plan.Workflow)
	fmt.Fprintf(w, "Patch type: %s\n", plan.FixType)
	if len(plan.AffectedJobs) > 0 {
		fmt.Fprintln(w, "Affected runtime jobs:")
		for _, job := range plan.AffectedJobs {
			fmt.Fprintf(w, "- %s\n", job)
		}
	}
	if len(plan.PatchScope.JobsTouched) > 0 {
		fmt.Fprintln(w, "Workflow jobs touched:")
		for _, job := range plan.PatchScope.JobsTouched {
			fmt.Fprintf(w, "- %s\n", job)
		}
	}
	if len(plan.Targets) > 0 {
		fmt.Fprintln(w, "Targets:")
		for _, target := range plan.Targets {
			fmt.Fprintf(w, "- Workflow: %s", target.Workflow)
			if target.Job != "" {
				fmt.Fprintf(w, " Job: %s", target.Job)
			}
			if target.Step != "" {
				fmt.Fprintf(w, " Step: %s", target.Step)
			}
			if target.Command != "" && target.Command != target.Step {
				fmt.Fprintf(w, " Command: %s", target.Command)
			}
			if len(target.DerivedFrom) > 0 {
				fmt.Fprintf(w, " Derived from: %s", listOrNone(target.DerivedFrom))
			}
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintf(w, "Hypothesis: %s\n", plan.Hypothesis)
	fmt.Fprintf(w, "Evidence: %s\n", plan.Evidence)
	fmt.Fprintf(w, "Change: %s\n", plan.ChangeSummary)
	fmt.Fprintf(w, "Success criteria: %s\n", plan.SuccessCriteria)
	fmt.Fprintf(w, "Confidence: %s\n", plan.Confidence)
	fmt.Fprintf(w, "Patch generated: %s\n", yesNo(plan.PatchGenerated))
	fmt.Fprintf(w, "Patch: %s\n", patchStatus(plan))
	if !plan.PatchGenerated {
		fmt.Fprintln(w, "No safe patch generated.")
	}
	if plan.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", plan.Reason)
	}
	fmt.Fprintln(w, "Patch scope:")
	fmt.Fprintf(w, "- files changed: %d\n", plan.PatchScope.FilesChanged)
	fmt.Fprintf(w, "- jobs touched: %s\n", listOrNone(plan.PatchScope.JobsTouched))
	fmt.Fprintf(w, "- steps touched: %s\n", listOrNone(plan.PatchScope.StepsTouched))
	fmt.Fprintf(w, "- unrelated lines changed: %d\n", plan.PatchScope.UnrelatedLinesChanged)
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

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func patchStatus(plan fix.Plan) string {
	if plan.PatchApplied {
		return "applied"
	}
	if plan.PatchGenerated {
		return "generated"
	}
	return "not generated"
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return fmt.Sprintf("%v", values)
}

func WriteFixJSON(w io.Writer, plan fix.Plan) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}
