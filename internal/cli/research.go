package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/research"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/autoci-ai/autoci/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newResearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research",
		Short: "Generate CI optimization research plans",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			runtimeProfile, err := depot.Profile(cmd.Context())
			if err != nil {
				return fmt.Errorf("profile CI history: %w", err)
			}
			failureAnalysis, _ := depot.Failures(cmd.Context(), workflowName)
			plan := research.FromProfileAndFailures(workflowName, runtimeProfile, failureAnalysis, cfg.Verbose)
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
