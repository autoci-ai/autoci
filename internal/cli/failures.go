package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newFailuresCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "failures",
		Short: "Analyze recurring CI failures",
		RunE: func(cmd *cobra.Command, args []string) error {
			format := viper.GetString("failures-format")
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: expected text or json", format)
			}
			reportPath := viper.GetString("failures-report")
			limit := viper.GetInt("failures-limit")
			repo := viper.GetString("failures-repo")

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			workflow, err := scanner.SelectWorkflow(cfg.Path, workflows, viper.GetString("workflow"), "failures")
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
			analysis, err := depot.Failures(cmd.Context(), workflowName)
			if err != nil {
				return fmt.Errorf("analyze CI failures: %w", err)
			}
			if format == "json" {
				return report.WriteFailuresJSON(cmd.OutOrStdout(), analysis)
			}
			report.WriteFailuresTerminal(cmd.OutOrStdout(), analysis)
			if reportPath != "" {
				return report.WriteFailuresMarkdownFile(reportPath, analysis)
			}
			return nil
		},
	}

	cmd.Flags().String("format", "text", "output format: text or json")
	cmd.Flags().Int("limit", 50, "number of recent workflow runs to inspect")
	cmd.Flags().String("report", "", "write a Markdown failure analysis to this path")
	cmd.Flags().String("repo", "", "repository filter in owner/name format")
	cmd.Flags().String("workflow", "", "workflow to analyze by basename or relative path")
	_ = viper.BindPFlag("failures-format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("failures-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("failures-report", cmd.Flags().Lookup("report"))
	_ = viper.BindPFlag("failures-repo", cmd.Flags().Lookup("repo"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))
	return cmd
}
