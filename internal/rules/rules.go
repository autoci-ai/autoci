package rules

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autoci-ai/autoci/internal/scanner"
	"gopkg.in/yaml.v3"
)

type Finding struct {
	ID             string
	Title          string
	Severity       string
	File           string
	Line           int
	Evidence       string
	Recommendation string
}

func Evaluate(workflows []scanner.Workflow) []Finding {
	var findings []Finding
	for _, workflow := range workflows {
		doc := document(workflow.Root)
		if doc == nil {
			continue
		}
		steps := collectSteps(doc)
		jobs := mappingValue(doc, "jobs")

		if !hasAnyKey(doc, "timeout", "timeout-minutes", "timeouts") && !hasAnyStepKey(steps, "timeout", "timeout-minutes") {
			findings = append(findings, finding("missing-timeouts", "Missing explicit timeouts", "medium", workflow.Path, doc.Line, "No timeout field was found in the workflow.", "Set explicit job or step timeouts so stuck builds fail predictably."))
		}
		if !hasAnyKey(doc, "cache", "caches") && !stepsContainAny(steps, "cache", "depot cache") {
			findings = append(findings, finding("missing-cache", "No obvious caching strategy", "medium", workflow.Path, doc.Line, "No cache configuration or cache step was found.", "Add dependency and Docker layer caching where the workflow spends repeated setup time."))
		}
		if !hasAnyKey(doc, "concurrency", "cancel-in-progress", "cancel") {
			findings = append(findings, finding("missing-concurrency", "Missing concurrency or cancellation behavior", "low", workflow.Path, doc.Line, "No concurrency or cancellation setting was found.", "Cancel superseded branch builds or configure concurrency where Depot supports it."))
		}
		findings = append(findings, unpinnedReferences(workflow.Path, steps)...)
		findings = append(findings, duplicateInstalls(workflow.Path, steps)...)
		findings = append(findings, dockerCacheFindings(workflow.Path, steps)...)
		if countSteps(steps) > 20 || countJobs(jobs) > 8 {
			findings = append(findings, finding("complex-workflow", "Workflow is long or complex", "low", workflow.Path, doc.Line, fmt.Sprintf("Found %d jobs and %d steps.", countJobs(jobs), countSteps(steps)), "Consider splitting independent workflows or moving repeated logic into scripts."))
		}
		if countJobs(jobs) <= 1 && countSteps(steps) >= 6 {
			findings = append(findings, finding("serial-workflow", "Workflow may be more serial than necessary", "low", workflow.Path, doc.Line, fmt.Sprintf("Found %d job and %d steps.", countJobs(jobs), countSteps(steps)), "Look for independent test, lint, and build work that can run in parallel."))
		}
	}
	return findings
}

func finding(id, title, severity, file string, line int, evidence, recommendation string) Finding {
	return Finding{ID: id, Title: title, Severity: severity, File: file, Line: line, Evidence: evidence, Recommendation: recommendation}
}

func document(root *yaml.Node) *yaml.Node {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	return root.Content[0]
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

func hasAnyKey(node *yaml.Node, keys ...string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			for _, key := range keys {
				if strings.EqualFold(node.Content[i].Value, key) {
					return true
				}
			}
			if hasAnyKey(node.Content[i+1], keys...) {
				return true
			}
		}
		return false
	}
	for _, child := range node.Content {
		if hasAnyKey(child, keys...) {
			return true
		}
	}
	return false
}

func hasAnyStepKey(steps []*yaml.Node, keys ...string) bool {
	for _, step := range steps {
		if hasAnyKey(step, keys...) {
			return true
		}
	}
	return false
}

func collectSteps(node *yaml.Node) []*yaml.Node {
	var steps []*yaml.Node
	if node == nil {
		return steps
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if strings.EqualFold(node.Content[i].Value, "steps") && node.Content[i+1].Kind == yaml.SequenceNode {
				steps = append(steps, node.Content[i+1].Content...)
			}
			steps = append(steps, collectSteps(node.Content[i+1])...)
		}
		return steps
	}
	for _, child := range node.Content {
		steps = append(steps, collectSteps(child)...)
	}
	return steps
}

func stepsContainAny(steps []*yaml.Node, terms ...string) bool {
	for _, step := range steps {
		text := strings.ToLower(nodeText(step))
		for _, term := range terms {
			if strings.Contains(text, strings.ToLower(term)) {
				return true
			}
		}
	}
	return false
}

func unpinnedReferences(file string, steps []*yaml.Node) []Finding {
	var findings []Finding
	for _, step := range steps {
		for _, value := range stepStrings(step) {
			if looksUnpinned(value) {
				findings = append(findings, finding("unpinned-reference", "Likely unpinned external reference", "medium", file, step.Line, value, "Pin external actions, images, and install URLs to immutable versions or digests where possible."))
			}
		}
	}
	return findings
}

func duplicateInstalls(file string, steps []*yaml.Node) []Finding {
	seen := map[string]*yaml.Node{}
	var findings []Finding
	for _, step := range steps {
		for _, line := range strings.Split(nodeText(step), "\n") {
			normalized := normalizeInstall(line)
			if normalized == "" {
				continue
			}
			if first := seen[normalized]; first != nil {
				findings = append(findings, finding("duplicate-install", "Duplicate dependency install step", "low", file, step.Line, strings.TrimSpace(line), "Install dependencies once per job or cache the installed dependency directory."))
				break
			}
			seen[normalized] = step
		}
	}
	return findings
}

func dockerCacheFindings(file string, steps []*yaml.Node) []Finding {
	var findings []Finding
	for _, step := range steps {
		text := strings.ToLower(nodeText(step))
		if strings.Contains(text, "docker build") && !strings.Contains(text, "depot build") && !strings.Contains(text, "cache-from") && !strings.Contains(text, "cache-to") {
			findings = append(findings, finding("docker-cache", "Docker build may not use Depot cache effectively", "medium", file, step.Line, strings.TrimSpace(firstLine(nodeText(step))), "Use Depot build or configure cache-from/cache-to for Docker layer reuse."))
		}
	}
	return findings
}

func countJobs(jobs *yaml.Node) int {
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return 0
	}
	return len(jobs.Content) / 2
}

func countSteps(steps []*yaml.Node) int {
	return len(steps)
}

func nodeText(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	var parts []string
	for _, child := range node.Content {
		parts = append(parts, nodeText(child))
	}
	return strings.Join(parts, "\n")
}

func stepStrings(step *yaml.Node) []string {
	var values []string
	if step == nil {
		return values
	}
	if step.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(step.Content); i += 2 {
			key := strings.ToLower(step.Content[i].Value)
			if key == "uses" || key == "image" || key == "run" || key == "command" || key == "commands" {
				values = append(values, scalarStrings(step.Content[i+1])...)
			}
		}
		return values
	}
	return scalarStrings(step)
}

func scalarStrings(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	var values []string
	for _, child := range node.Content {
		values = append(values, scalarStrings(child)...)
	}
	return values
}

func looksUnpinned(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.Contains(lower, "@main") || strings.Contains(lower, "@master") || strings.Contains(lower, ":latest") {
		return true
	}
	if strings.Contains(lower, "curl ") && (strings.Contains(lower, " | sh") || strings.Contains(lower, "| bash")) {
		return true
	}
	if strings.HasPrefix(lower, "docker pull ") && !strings.Contains(lower, "@sha256:") {
		image := strings.TrimSpace(strings.TrimPrefix(lower, "docker pull "))
		return !strings.Contains(filepath.Base(image), ":")
	}
	return false
}

func normalizeInstall(line string) string {
	line = strings.TrimSpace(strings.ToLower(line))
	installPrefixes := []string{"npm install", "npm ci", "yarn install", "pnpm install", "go mod download", "pip install", "bundle install", "composer install"}
	for _, prefix := range installPrefixes {
		if strings.HasPrefix(line, prefix) {
			return prefix
		}
	}
	return ""
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
