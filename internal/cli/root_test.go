package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out.String()), "autoci version 0.1.0-dev"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHelpTextIsProviderNeutral(t *testing.T) {
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{
		"Analyze, profile, and improve CI workflows.",
		"Analyze CI workflows",
		"Profile CI execution history",
		"Validate workflow changes",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
	for _, unwanted := range []string{
		"Analyze Depot CI workflows",
		"Profile Depot CI runtime history",
		"Validate Depot CI workflows",
	} {
		if strings.Contains(help, unwanted) {
			t.Fatalf("help contains provider-specific text %q:\n%s", unwanted, help)
		}
	}
}
