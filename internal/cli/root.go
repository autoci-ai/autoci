package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/autoci-ai/autoci/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfg config.Config

func Execute() error {
	root := newRootCommand()
	return root.Execute()
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "autoci",
		Short:         "Analyze, profile, and improve CI workflows.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if configFile := viper.GetString("config"); configFile != "" {
				viper.SetConfigFile(configFile)
				if err := viper.ReadInConfig(); err != nil {
					return err
				}
			}
			if err := viper.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			if err := viper.BindPFlags(cmd.Root().PersistentFlags()); err != nil {
				return err
			}
			cfg.Path = viper.GetString("path")
			cfg.Config = viper.GetString("config")
			cfg.Verbose = viper.GetBool("verbose")
			return nil
		},
	}

	root.PersistentFlags().String("path", ".", "repository path to inspect")
	root.PersistentFlags().String("config", "", "optional config file")
	root.PersistentFlags().Bool("verbose", false, "enable verbose logging")

	_ = viper.BindPFlag("path", root.PersistentFlags().Lookup("path"))
	_ = viper.BindPFlag("config", root.PersistentFlags().Lookup("config"))
	_ = viper.BindPFlag("verbose", root.PersistentFlags().Lookup("verbose"))
	viper.SetEnvPrefix("AUTOCI")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	root.AddCommand(newAnalyzeCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newProfileCommand())
	root.AddCommand(newResearchCommand())
	root.AddCommand(newValidateCommand())
	root.AddCommand(newVersionCommand())

	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w", err)
	})

	return root
}
