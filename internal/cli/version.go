package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "autoci version %s\n", buildinfo.Version)
			return nil
		},
	}
}
