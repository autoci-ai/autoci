package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/autoci-ai/autoci/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Profile CI execution history",
		RunE: func(cmd *cobra.Command, args []string) error {
			reportPath := viper.GetString("profile-report")
			format := viper.GetString("profile-format")
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: expected text or json", format)
			}
			limit := viper.GetInt("profile-limit")
			repo := viper.GetString("profile-repo")

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			workflow, err := scanner.SelectWorkflow(cfg.Path, workflows, viper.GetString("workflow"), "profile")
			if err != nil {
				return err
			}
			depot := provider.DepotProvider{
				Repo:           repo,
				RepoPath:       cfg.Path,
				Limit:          limit,
				LocalWorkflows: []scanner.Workflow{workflow},
			}
			profile, err := depot.Profile(cmd.Context())
			if err != nil {
				return fmt.Errorf("profile CI history: %w", err)
			}
			workflowName := scanner.WorkflowName(cfg.Path, workflow)
			_ = state.Write(cfg.Path, "profile", workflowName, profile)
			if format == "json" {
				return report.WriteProfileJSON(cmd.OutOrStdout(), workflowName, profile)
			}
			report.WriteProfileTerminal(cmd.OutOrStdout(), []scanner.Workflow{workflow}, profile)
			if reportPath != "" {
				return report.WriteProfileMarkdownFile(reportPath, []scanner.Workflow{workflow}, profile)
			}
			return nil
		},
	}

	cmd.Flags().String("report", "", "write a Markdown profile report to this path")
	cmd.Flags().String("format", "text", "output format: text or json")
	cmd.Flags().Int("limit", 50, "number of recent Depot workflows to inspect")
	cmd.Flags().String("repo", "", "Depot repo filter in owner/name format")
	cmd.Flags().String("workflow", "", "workflow to profile by basename or relative path")
	_ = viper.BindPFlag("profile-report", cmd.Flags().Lookup("report"))
	_ = viper.BindPFlag("profile-format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("profile-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("profile-repo", cmd.Flags().Lookup("repo"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))

	return cmd
}
