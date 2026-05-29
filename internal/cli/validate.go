package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newValidateCommand() *cobra.Command {
	var allowDepotRun bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate Depot CI workflows by running Depot",
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
			return runner.RunDepotCI(cmd.Context(), cfg.Path, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().Bool("allow-depot-run", false, "allow autoci to execute depot ci run")
	cmd.Flags().Bool("dry-run", false, "print the Depot command without executing it")
	_ = viper.BindPFlag("allow-depot-run", cmd.Flags().Lookup("allow-depot-run"))
	_ = viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	return cmd
}
