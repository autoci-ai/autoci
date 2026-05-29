package rules

import (
	"testing"

	"github.com/autoci-ai/autoci/internal/scanner"
	"gopkg.in/yaml.v3"
)

func TestEvaluateFindsBasicIssues(t *testing.T) {
	doc := parseTestYAML(t, `
jobs:
  test:
    steps:
      - run: npm ci
      - run: npm ci
      - run: docker build .
      - uses: depot/setup-action@main
`)

	findings := Evaluate([]scanner.Workflow{{Path: "depot.yml", Root: doc}})
	ids := map[string]bool{}
	for _, finding := range findings {
		ids[finding.ID] = true
	}

	for _, id := range []string{"missing-timeouts", "missing-cache", "missing-concurrency", "duplicate-install", "docker-cache", "unpinned-reference"} {
		if !ids[id] {
			t.Fatalf("expected finding %q in %#v", id, findings)
		}
	}
}

func parseTestYAML(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(input), &node); err != nil {
		t.Fatal(err)
	}
	return &node
}
