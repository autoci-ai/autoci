package cli

import (
	"fmt"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/research"
	"github.com/autoci-ai/autoci/internal/rules"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/autoci-ai/autoci/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newResearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research [id]",
		Short: "Generate CI optimization research plans",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				target, err := research.Targeted(cfg.Path, viper.GetString("workflow"), args[0])
				if err != nil {
					return err
				}
				evidencePath, reportPath, err := state.WriteTargetedResearch(cfg.Path, target.ID, target, research.WriteTargetMarkdown(target))
				if err != nil {
					return err
				}
				relEvidence, _ := filepath.Rel(cfg.Path, evidencePath)
				relReport, _ := filepath.Rel(cfg.Path, reportPath)
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote targeted research for %s\n", target.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", relEvidence)
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", relReport)
				return nil
			}
			format := viper.GetString("research-format")
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: expected text or json", format)
			}
			reportPath := viper.GetString("research-report")
			limit := viper.GetInt("research-limit")
			repo := viper.GetString("research-repo")

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			workflow, err := scanner.SelectWorkflow(cfg.Path, workflows, viper.GetString("workflow"), "research")
			if err != nil {
				return err
			}
			workflowName := scanner.WorkflowName(cfg.Path, workflow)
			depot := provider.DepotProvider{
				Repo:           repo,
				RepoPath:       cfg.Path,
				Limit:          limit,
				LocalWorkflows: []scanner.Workflow{workflow},
			}
			runtimeProfile, err := state.ReadData[profile.Profile](cfg.Path, "profile", workflowName)
			if err != nil {
				runtimeProfile, err = depot.Profile(cmd.Context())
				if err != nil {
					return fmt.Errorf("profile CI history: %w", err)
				}
				_ = state.Write(cfg.Path, "profile", workflowName, runtimeProfile)
			}
			failureAnalysis, err := state.ReadData[failures.Analysis](cfg.Path, "failures", workflowName)
			if err != nil {
				failureAnalysis, _ = depot.Failures(cmd.Context(), workflowName)
				if failureAnalysis != nil {
					_ = state.Write(cfg.Path, "failures", workflowName, failureAnalysis)
				}
			}
			staticAnalysis := rules.AnalyzeWorkflow(workflowName, workflow)
			_ = state.Write(cfg.Path, "analyze", workflowName, staticAnalysis)
			plan := research.FromSources(workflowName, runtimeProfile, failureAnalysis, staticAnalysis.Findings, cfg.Verbose)
			_ = state.Write(cfg.Path, "research", workflowName, plan)
			if format == "json" {
				return report.WriteResearchJSON(cmd.OutOrStdout(), plan)
			}
			report.WriteResearchTerminal(cmd.OutOrStdout(), plan)
			if reportPath != "" {
				return report.WriteResearchMarkdownFile(reportPath, plan)
			}
			return nil
		},
	}

	cmd.Flags().String("format", "text", "output format: text or json")
	cmd.Flags().Int("limit", 50, "number of recent workflow runs to inspect")
	cmd.Flags().String("report", "", "write a Markdown research plan to this path")
	cmd.Flags().String("repo", "", "repository filter in owner/name format")
	cmd.Flags().String("workflow", "", "workflow to research by basename or relative path")
	_ = viper.BindPFlag("research-format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("research-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("research-report", cmd.Flags().Lookup("report"))
	_ = viper.BindPFlag("research-repo", cmd.Flags().Lookup("repo"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))

	return cmd
}
