package fix

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/autoci-ai/autoci/internal/scanner"
	"gopkg.in/yaml.v3"
)

type Plan struct {
	ID              string   `json:"id"`
	Branch          string   `json:"branch"`
	Workflow        string   `json:"workflow"`
	Hypothesis      string   `json:"hypothesis"`
	Evidence        string   `json:"evidence"`
	ChangeSummary   string   `json:"changeSummary"`
	SuccessCriteria string   `json:"successCriteria"`
	Confidence      string   `json:"confidence"`
	Diff            string   `json:"diff,omitempty"`
	Validation      []string `json:"validation"`
}

type Options struct {
	RepoPath     string
	Workflow     scanner.Workflow
	WorkflowName string
	Opportunity  string
	DryRun       bool
	Evidence     string
}

func Generate(options Options) (Plan, error) {
	id := normalizeOpportunity(options.Opportunity)
	if id == "" {
		id = "failure-theme-image-pull-failure"
	}
	plan := basePlan(id, options.WorkflowName, options.Evidence)
	original, err := os.ReadFile(options.Workflow.Path)
	if err != nil {
		return Plan{}, err
	}
	updated, err := applyWorkflowFix(original, id)
	if err != nil {
		return Plan{}, err
	}
	if bytes.Equal(original, updated) {
		return Plan{}, fmt.Errorf("no safe workflow modification found for %q", id)
	}
	plan.Diff = unifiedDiff(options.Workflow.Path, original, updated)
	if options.DryRun {
		return plan, nil
	}
	if err := createBranch(options.RepoPath, plan.Branch); err != nil {
		return Plan{}, err
	}
	if err := os.WriteFile(options.Workflow.Path, updated, 0o644); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func basePlan(id, workflow, evidence string) Plan {
	plan := Plan{
		ID:         id,
		Branch:     "autoci/fix-" + trimFixPrefix(strings.TrimPrefix(id, "failure-theme-")),
		Workflow:   workflow,
		Evidence:   evidence,
		Confidence: "medium",
		Validation: []string{"autoci validate --allow-depot-run"},
	}
	switch {
	case strings.Contains(id, "image-pull"):
		plan.Hypothesis = "Image pull failures are caused by registry latency, network instability, or mutable remote image availability."
		plan.ChangeSummary = "Added Docker client timeout defaults to make image pulls more tolerant of slow registry responses."
		plan.SuccessCriteria = "Image pull failures no longer recur in future workflow runs."
	case strings.Contains(id, "npm-install"):
		plan.Hypothesis = "Dependency install failures are caused by transient registry or network failures."
		plan.ChangeSummary = "Wrapped npm install commands with bounded retry logic."
		plan.SuccessCriteria = "npm install failures no longer recur without increasing workflow failure rate."
	case strings.Contains(id, "flaky-job") || strings.Contains(id, "go-lint") || strings.Contains(id, "golangci-lint"):
		plan.Hypothesis = "Lint instability is caused by transient execution or dependency setup failures."
		plan.ChangeSummary = "Wrapped golangci-lint execution with bounded retry logic where present."
		plan.SuccessCriteria = "The lint job failure rate falls below 2% or the root cause is identified."
	default:
		plan.Hypothesis = "The selected opportunity points to a measurable CI improvement."
		plan.ChangeSummary = "Applied the safest available workflow hardening for this opportunity."
		plan.SuccessCriteria = "The targeted failure mode no longer recurs in future workflow runs."
		plan.Confidence = "low"
	}
	if plan.Evidence == "" {
		plan.Evidence = "Selected opportunity: " + id
	}
	return plan
}

func applyWorkflowFix(input []byte, id string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(input, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("workflow is empty")
	}
	doc := root.Content[0]
	changed := false
	switch {
	case strings.Contains(id, "image-pull"):
		changed = ensureEnv(doc, "DOCKER_CLIENT_TIMEOUT", "300") || changed
		changed = ensureEnv(doc, "COMPOSE_HTTP_TIMEOUT", "300") || changed
	case strings.Contains(id, "npm-install"):
		changed = rewriteRunScalars(doc, isNPMInstallLine) || changed
	case strings.Contains(id, "flaky-job") || strings.Contains(id, "go-lint") || strings.Contains(id, "golangci-lint"):
		changed = rewriteRunScalars(doc, isGolangCILintLine) || changed
	default:
		return nil, fmt.Errorf("no fix generator available for %q", id)
	}
	if !changed {
		return input, nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ensureEnv(doc *yaml.Node, key, value string) bool {
	if doc.Kind != yaml.MappingNode {
		return false
	}
	env := mappingValue(doc, "env")
	if env == nil {
		envKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "env"}
		env = &yaml.Node{Kind: yaml.MappingNode}
		doc.Content = append(doc.Content, envKey, env)
	}
	if env.Kind != yaml.MappingNode {
		return false
	}
	if existing := mappingValue(env, key); existing != nil {
		return false
	}
	env.Content = append(env.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str"},
	)
	return true
}

func rewriteRunScalars(node *yaml.Node, match func(string) bool) bool {
	changed := false
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "run" && node.Content[i+1].Kind == yaml.ScalarNode {
				updated, ok := wrapMatchingLines(node.Content[i+1].Value, match)
				if ok {
					node.Content[i+1].Value = updated
					node.Content[i+1].Style = yaml.LiteralStyle
					changed = true
				}
			}
			if rewriteRunScalars(node.Content[i+1], match) {
				changed = true
			}
		}
		return changed
	}
	for _, child := range node.Content {
		if rewriteRunScalars(child, match) {
			changed = true
		}
	}
	return changed
}

func wrapMatchingLines(script string, match func(string) bool) (string, bool) {
	if strings.Contains(script, "autoci_retry()") {
		return script, false
	}
	lines := strings.Split(script, "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match(trimmed) {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = prefix + "autoci_retry " + trimmed
			changed = true
		}
	}
	if !changed {
		return script, false
	}
	header := "autoci_retry() {\n  n=0\n  until \"$@\"; do\n    n=$((n+1))\n    if [ \"$n\" -ge 3 ]; then\n      return 1\n    fi\n    sleep $((n * 5))\n  done\n}\n"
	return header + strings.Join(lines, "\n"), true
}

func isNPMInstallLine(line string) bool {
	return strings.HasPrefix(line, "npm ci") || strings.HasPrefix(line, "npm install")
}

func isGolangCILintLine(line string) bool {
	return strings.Contains(line, "golangci-lint")
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func createBranch(repoPath, branch string) error {
	cmd := exec.Command("git", "switch", "-c", branch)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(out), "already exists") {
		cmd = exec.Command("git", "switch", branch)
		cmd.Dir = repoPath
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("create branch %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

func unifiedDiff(path string, original, updated []byte) string {
	oldLines := strings.Split(strings.TrimRight(string(original), "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(string(updated), "\n"), "\n")
	var builder strings.Builder
	rel := filepath.ToSlash(path)
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", rel, rel)
	fmt.Fprintln(&builder, "@@")
	for _, line := range oldLines {
		fmt.Fprintf(&builder, "-%s\n", line)
	}
	for _, line := range newLines {
		fmt.Fprintf(&builder, "+%s\n", line)
	}
	return builder.String()
}

func normalizeOpportunity(value string) string {
	return strings.TrimSpace(value)
}

func trimFixPrefix(value string) string {
	value = strings.TrimPrefix(value, "fix-")
	value = strings.TrimPrefix(value, "theme-")
	return slug(value)
}

func slug(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
