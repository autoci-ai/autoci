package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/provider"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Profile Depot CI runtime history",
		RunE: func(cmd *cobra.Command, args []string) error {
			reportPath := viper.GetString("profile-report")
			limit := viper.GetInt("profile-limit")
			repo := viper.GetString("profile-repo")

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			depot := provider.DepotProvider{
				Repo:           repo,
				RepoPath:       cfg.Path,
				Limit:          limit,
				LocalWorkflows: workflows,
			}
			profile, err := depot.Profile(cmd.Context())
			if err != nil {
				return fmt.Errorf("profile Depot CI history: %w", err)
			}
			report.WriteProfileTerminal(cmd.OutOrStdout(), workflows, profile)
			if reportPath != "" {
				return report.WriteProfileMarkdownFile(reportPath, workflows, profile)
			}
			return nil
		},
	}

	cmd.Flags().String("report", "", "write a Markdown profile report to this path")
	cmd.Flags().Int("limit", 50, "number of recent Depot workflows to inspect")
	cmd.Flags().String("repo", "", "Depot repo filter in owner/name format")
	_ = viper.BindPFlag("profile-report", cmd.Flags().Lookup("report"))
	_ = viper.BindPFlag("profile-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("profile-repo", cmd.Flags().Lookup("repo"))

	return cmd
}
