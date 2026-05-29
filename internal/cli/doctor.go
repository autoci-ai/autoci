package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var errDoctorUnhealthy = errors.New("doctor found issues")

func IsDoctorUnhealthy(err error) bool {
	return errors.Is(err, errDoctorUnhealthy)
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the local AutoCI environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd)
		},
	}
}

func runDoctor(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	healthy := true

	if err := loadConfigForDoctor(); err != nil {
		fmt.Fprintf(out, "✗ Configuration failed to load: %v\n", err)
		healthy = false
	} else if cfg.Config != "" {
		fmt.Fprintln(out, "✓ Configuration loaded")
	}

	if stat, err := os.Stat(cfg.Path); err != nil || !stat.IsDir() {
		fmt.Fprintf(out, "✗ Repository path not found: %s\n", cfg.Path)
		healthy = false
	} else {
		fmt.Fprintln(out, "✓ Repository discovered")
	}

	workflowDir := filepath.Join(cfg.Path, ".depot", "workflows")
	if stat, err := os.Stat(workflowDir); err != nil || !stat.IsDir() {
		fmt.Fprintln(out, "✗ .depot/workflows not found")
		healthy = false
	} else {
		workflows, err := scanner.Scan(cfg.Path)
		if err != nil {
			fmt.Fprintf(out, "✗ Workflows could not be discovered: %v\n", err)
			healthy = false
		} else if len(workflows) == 0 {
			fmt.Fprintln(out, "✗ No workflows found")
			healthy = false
		} else {
			fmt.Fprintf(out, "✓ %d workflows found\n", len(workflows))
		}
	}

	depotPath, err := exec.LookPath("depot")
	if err != nil {
		fmt.Fprintln(out, "✗ Depot CLI not found")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Install Depot: https://depot.dev/")
		healthy = false
	} else if err := exec.Command(depotPath, "--help").Run(); err != nil {
		fmt.Fprintln(out, "✗ Depot CLI is not executable")
		healthy = false
	} else {
		fmt.Fprintln(out, "✓ Depot CLI found")
	}

	if !healthy {
		return errDoctorUnhealthy
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "AutoCI environment looks healthy.")
	return nil
}

func loadConfigForDoctor() error {
	configFile := viper.GetString("config")
	if configFile == "" {
		return nil
	}
	viper.SetConfigFile(configFile)
	if err := viper.ReadInConfig(); err != nil {
		return err
	}
	cfg.Config = configFile
	return nil
}
