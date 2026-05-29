package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/runner"
	"github.com/autoci-ai/autoci/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newValidateCommand() *cobra.Command {
	var allowDepotRun bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate workflow changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			allowDepotRun = viper.GetBool("allow-depot-run")
			dryRun = viper.GetBool("dry-run")

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "depot ci run")
				return nil
			}
			if !allowDepotRun {
				return fmt.Errorf("Refusing to run `depot ci run` without --allow-depot-run.")
			}
			if err := runner.RunDepotCI(cmd.Context(), cfg.Path, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			if id := viper.GetString("validate-id"); id != "" {
				return state.UpdateTargetedResearchReadiness(cfg.Path, id, lifecycle.ReadinessValidated)
			}
			return nil
		},
	}

	cmd.Flags().Bool("allow-depot-run", false, "allow autoci to execute depot ci run")
	cmd.Flags().Bool("dry-run", false, "print the Depot command without executing it")
	cmd.Flags().String("id", "", "AutoCI research/fix ID to mark validated after successful validation")
	_ = viper.BindPFlag("allow-depot-run", cmd.Flags().Lookup("allow-depot-run"))
	_ = viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("validate-id", cmd.Flags().Lookup("id"))

	return cmd
}
