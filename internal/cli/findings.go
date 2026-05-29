package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/findings"
	"github.com/autoci-ai/autoci/internal/report"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newFindingsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "findings",
		Short: "Show prioritized CI improvement findings from cached state",
		RunE: func(cmd *cobra.Command, args []string) error {
			format := viper.GetString("findings-format")
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q: expected text or json", format)
			}
			limit := viper.GetInt("findings-limit")
			if viper.GetBool("findings-next") {
				limit = 1
			}
			items, err := findings.Load(cfg.Path, findings.Options{
				Workflow:               viper.GetString("workflow"),
				IncludeFailures:        viper.GetBool("findings-failures"),
				IncludeProfile:         viper.GetBool("findings-profile"),
				ReliabilityOnly:        viper.GetBool("findings-reliability"),
				OptimizationOnly:       viper.GetBool("findings-optimization"),
				ActiveOnly:             viper.GetBool("findings-active"),
				NewOnly:                viper.GetBool("findings-new"),
				AwaitingValidationOnly: viper.GetBool("findings-awaiting-validation"),
				Limit:                  limit,
			})
			if err != nil {
				return err
			}
			if format == "json" {
				return report.WriteFindingsJSON(cmd.OutOrStdout(), items)
			}
			report.WriteFindingsTerminal(cmd.OutOrStdout(), items)
			return nil
		},
	}
	cmd.Flags().Int("limit", 10, "maximum number of findings to show")
	cmd.Flags().String("workflow", "", "workflow to read by basename")
	cmd.Flags().String("format", "text", "output format: text or json")
	cmd.Flags().Bool("failures", false, "include cached failure findings")
	cmd.Flags().Bool("profile", false, "include cached profile findings")
	cmd.Flags().Bool("reliability", false, "include reliability findings")
	cmd.Flags().Bool("optimization", false, "include optimization findings")
	cmd.Flags().Bool("next", false, "show only the highest-priority next finding")
	cmd.Flags().Bool("active", false, "show findings with an actionable next step")
	cmd.Flags().Bool("new", false, "show findings that have not been researched")
	cmd.Flags().Bool("awaiting-validation", false, "show findings waiting on future CI runs")
	_ = viper.BindPFlag("findings-limit", cmd.Flags().Lookup("limit"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))
	_ = viper.BindPFlag("findings-format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("findings-failures", cmd.Flags().Lookup("failures"))
	_ = viper.BindPFlag("findings-profile", cmd.Flags().Lookup("profile"))
	_ = viper.BindPFlag("findings-reliability", cmd.Flags().Lookup("reliability"))
	_ = viper.BindPFlag("findings-optimization", cmd.Flags().Lookup("optimization"))
	_ = viper.BindPFlag("findings-next", cmd.Flags().Lookup("next"))
	_ = viper.BindPFlag("findings-active", cmd.Flags().Lookup("active"))
	_ = viper.BindPFlag("findings-new", cmd.Flags().Lookup("new"))
	_ = viper.BindPFlag("findings-awaiting-validation", cmd.Flags().Lookup("awaiting-validation"))
	return cmd
}
