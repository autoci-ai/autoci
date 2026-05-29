package cli

import (
	"fmt"

	"github.com/autoci-ai/autoci/internal/report"
	"github.com/autoci-ai/autoci/internal/rules"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newAnalyzeCommand() *cobra.Command {
	var reportPath string
	var format string

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze CI workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			reportPath = viper.GetString("report")
			format = viper.GetString("format")
			if format != "text" && format != "markdown" {
				return fmt.Errorf("unsupported format %q: expected text or markdown", format)
			}

			workflows, err := scanner.Scan(cfg.Path)
			if err != nil {
				return err
			}
			workflow, err := scanner.SelectWorkflow(cfg.Path, workflows, viper.GetString("workflow"), "analyze")
			if err != nil {
				return err
			}

			analysis := rules.AnalyzeWorkflow(scanner.WorkflowName(cfg.Path, workflow), workflow)
			if format == "markdown" {
				if err := report.WriteMarkdown(cmd.OutOrStdout(), analysis); err != nil {
					return err
				}
			} else {
				report.WriteTerminal(cmd.OutOrStdout(), analysis)
			}

			if reportPath != "" {
				return report.WriteMarkdownFile(reportPath, analysis)
			}
			return nil
		},
	}

	cmd.Flags().String("report", "", "write a Markdown report to this path")
	cmd.Flags().String("format", "text", "output format: text or markdown")
	cmd.Flags().String("workflow", "", "workflow to analyze by basename or relative path")
	_ = viper.BindPFlag("report", cmd.Flags().Lookup("report"))
	_ = viper.BindPFlag("format", cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("workflow", cmd.Flags().Lookup("workflow"))

	return cmd
}
