package cli

import (
	"fmt"
	"strings"

	"github.com/autoci-ai/autoci/internal/fix"
	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/research"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/autoci-ai/autoci/internal/state"
	stepresolver "github.com/autoci-ai/autoci/internal/workflow"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newFixCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [opportunity-id]",
		Short: "Generate and apply CI workflow fixes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := viper.GetString("fix-format")
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: expected text or json", format)
			}
			dryRun := viper.GetBool("fix-dry-run")
			limit := viper.GetInt("fix-limit")
			repo := viper.GetString("fix-repo")

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			workflow, err := scanner.SelectWorkflow(cfg.Path, workflows, viper.GetString("workflow"), "fix")
			if err != nil {
				return err
			}
			workflowName := scanner.WorkflowName(cfg.Path, workflow)
			opportunityID := viper.GetString("fix-id")
			evidence := ""
			var targetJobs []string
			occurrences := 0
			signature := ""
			artifacts := map[string][]string{}
			var candidateSteps []stepresolver.CandidateStep
			var hypotheses []fix.Hypothesis
			var logExcerpts []string
			readiness := lifecycle.Readiness("")
			if len(args) > 0 {
				if opportunityID != "" && opportunityID != args[0] {
					return fmt.Errorf("--id and positional opportunity id differ")
				}
				opportunityID = args[0]
			}
			if opportunityID != "" {
				if item, ok := state.FindItem(cfg.Path, workflowName, opportunityID); ok {
					evidence = item.Evidence
					targetJobs = item.Jobs
					occurrences = item.Occurrences
					signature = item.Signature
					artifacts = item.Artifacts
				} else {
					evidence = "Selected opportunity: " + opportunityID
				}
				if target, err := state.ReadTargetedResearch[research.TargetReport](cfg.Path, opportunityID); err == nil {
					if target.Readiness == "" {
						return fmt.Errorf("research evidence for %s is missing readiness; rerun autoci research %s", opportunityID, opportunityID)
					}
					if !target.Readiness.Valid() {
						return fmt.Errorf("research evidence for %s has invalid readiness %q", opportunityID, target.Readiness)
					}
					if target.Readiness != lifecycle.ReadinessReadyForFix {
						return fmt.Errorf("cannot generate fix.\n\nReadiness: %s\n\nMissing evidence:\n%s", target.Readiness, formatEvidenceGaps(target.Gaps))
					}
					candidateSteps = target.CandidateSteps
					hypotheses = fixHypotheses(target.RootCauseHypotheses)
					logExcerpts = target.LogExcerpts
					readiness = target.Readiness
					if len(target.Jobs) > 0 {
						targetJobs = target.Jobs
					}
					if target.Workflow != "" {
						workflowName = target.Workflow
					}
				}
			} else {
				depot := provider.DepotProvider{
					Repo:           repo,
					RepoPath:       cfg.Path,
					Limit:          limit,
					LocalWorkflows: []scanner.Workflow{workflow},
				}
				analysis, err := depot.Failures(cmd.Context(), workflowName)
				if err != nil {
					return fmt.Errorf("select fix opportunity: %w", err)
				}
				if len(analysis.FailureThemes) == 0 {
					return fmt.Errorf("no failure theme found to fix; pass an opportunity id explicitly")
				}
				theme := analysis.FailureThemes[0]
				opportunityID = theme.ID
				evidence = fmt.Sprintf("%d occurrences of %s across %d jobs.", theme.Occurrences, theme.Signature, len(theme.Jobs))
				targetJobs = theme.Jobs
				occurrences = theme.Occurrences
				signature = theme.Signature
				artifacts = map[string][]string{
					"images":      theme.Artifacts.Images,
					"packages":    theme.Artifacts.Packages,
					"modules":     theme.Artifacts.Modules,
					"urls":        theme.Artifacts.URLs,
					"hosts":       theme.Artifacts.Hosts,
					"dockerfiles": theme.Artifacts.Dockerfiles,
				}
			}

			plan, err := fix.Generate(fix.Options{
				RepoPath:       cfg.Path,
				Workflow:       workflow,
				WorkflowName:   workflowName,
				Opportunity:    opportunityID,
				DryRun:         dryRun,
				Evidence:       evidence,
				TargetJobs:     targetJobs,
				Occurrences:    occurrences,
				Signature:      signature,
				Artifacts:      artifacts,
				CandidateSteps: candidateSteps,
				Hypotheses:     hypotheses,
				LogExcerpts:    logExcerpts,
				Readiness:      readiness,
			})
			if err != nil {
				return err
			}
			_ = state.WriteFix(cfg.Path, fix.NewRecord(plan, dryRun))
			if format == "json" {
				return report.WriteFixJSON(cmd.OutOrStdout(), plan)
			}
			report.WriteFixTerminal(cmd.OutOrStdout(), plan, dryRun)
			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "generate the fix plan and diff without modifying files")
	cmd.Flags().String("format", "text", "output format: text or json")
	cmd.Flags().String("id", "", "AutoCI item ID to fix")
	cmd.Flags().Int("limit", 50, "number of recent workflow runs to inspect when selecting a fix")
	cmd.Flags().String("repo", "", "repository filter in owner/name format")
	cmd.Flags().String("workflow", "", "workflow to fix by basename or relative path")
	_ = viper.BindPFlag("fix-dry-run", cmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("fix-format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("fix-id", cmd.Flags().Lookup("id"))
	_ = viper.BindPFlag("fix-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("fix-repo", cmd.Flags().Lookup("repo"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))
	return cmd
}

func fixHypotheses(values []research.RootCauseHypothesis) []fix.Hypothesis {
	var result []fix.Hypothesis
	for _, value := range values {
		result = append(result, fix.Hypothesis{Summary: value.Summary, Confidence: value.Confidence, Evidence: value.Evidence})
	}
	return result
}

func formatEvidenceGaps(gaps []research.EvidenceGap) string {
	if len(gaps) == 0 {
		return "- No structured evidence gaps were recorded. Rerun autoci research for this ID."
	}
	var lines []string
	for _, gap := range gaps {
		if gap.Message != "" {
			lines = append(lines, "- "+gap.Message)
		}
	}
	if len(lines) == 0 {
		return "- No structured evidence gaps were recorded. Rerun autoci research for this ID."
	}
	return strings.Join(lines, "\n")
}
