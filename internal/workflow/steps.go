package workflow

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/autoci-ai/autoci/internal/scanner"
	"gopkg.in/yaml.v3"
)

type Step struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	Name     string `json:"step,omitempty"`
	Command  string `json:"command,omitempty"`
	Line     int    `json:"line,omitempty"`
	File     string `json:"file,omitempty"`
	Source   string `json:"source,omitempty"`
}

type CandidateStep struct {
	Step
	FindingID  string   `json:"findingId,omitempty"`
	Confidence float64  `json:"confidence"`
	Why        []string `json:"why,omitempty"`
}

type FindingContext struct {
	ID          string
	Signature   string
	Jobs        []string
	Artifacts   map[string][]string
	LogExcerpts []string
}

func Inventory(repoPath string, workflowName string, workflow scanner.Workflow) []Step {
	if workflow.Root == nil || len(workflow.Root.Content) == 0 {
		return nil
	}
	doc := workflow.Root.Content[0]
	jobs := mappingValue(doc, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}
	file := workflow.Path
	if rel, err := filepath.Rel(repoPath, workflow.Path); err == nil && !strings.HasPrefix(rel, "..") {
		file = filepath.ToSlash(rel)
	}
	var result []Step
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		job := jobs.Content[i].Value
		steps := mappingValue(jobs.Content[i+1], "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode {
				continue
			}
			name := scalarValue(mappingValue(step, "name"))
			for _, key := range []string{"run", "command", "script", "commands"} {
				for _, command := range commandValues(mappingValue(step, key)) {
					result = append(result, Step{
						Workflow: workflowName,
						Job:      job,
						Name:     name,
						Command:  command.value,
						Line:     command.line,
						File:     file,
						Source:   key,
					})
				}
			}
		}
	}
	return result
}

func CandidateSteps(steps []Step, context FindingContext) []CandidateStep {
	var result []CandidateStep
	for _, step := range steps {
		score, why := scoreStep(step, context)
		if score < 0.65 {
			continue
		}
		result = append(result, CandidateStep{Step: step, FindingID: context.ID, Confidence: score, Why: why})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Confidence == result[j].Confidence {
			return result[i].Line < result[j].Line
		}
		return result[i].Confidence > result[j].Confidence
	})
	if len(result) > 5 {
		return result[:5]
	}
	return result
}

func scoreStep(step Step, context FindingContext) (float64, []string) {
	command := strings.ToLower(step.Command)
	signature := strings.ToLower(context.Signature + " " + context.ID)
	var score float64
	var why []string
	if contains(context.Jobs, step.Job) {
		score += 0.35
		why = append(why, "job matches finding evidence")
	}
	switch {
	case strings.Contains(signature, "npm") || strings.Contains(signature, "yarn") || strings.Contains(signature, "dependency-install"):
		if isDependencyInstallCommand(command) {
			score += 0.45
			why = append(why, "command contains dependency install")
		}
		if strings.Contains(command, "yarn install") && evidenceContains(context.LogExcerpts, "yarn install") {
			score += 0.08
			why = append(why, "logs mention yarn install")
		}
		if strings.Contains(command, "--immutable") && evidenceContains(context.LogExcerpts, "--immutable") {
			score += 0.04
			why = append(why, "command includes install flag seen in evidence")
		}
		if hasArtifact(context.Artifacts, "packages") || hasArtifact(context.Artifacts, "modules") {
			score += 0.04
			why = append(why, "package evidence points at dependency installation")
		}
	case strings.Contains(signature, "image-pull") || strings.Contains(signature, "image pull"):
		if strings.Contains(command, "docker") || strings.Contains(command, "container") {
			score += 0.40
			why = append(why, "command references container tooling")
		}
	default:
		if step.Command != "" && len(context.Jobs) > 0 && contains(context.Jobs, step.Job) {
			score += 0.30
		}
	}
	if score > 0.99 {
		score = 0.99
	}
	return score, why
}

func IsDependencyInstallCommand(command string) bool {
	return isDependencyInstallCommand(strings.ToLower(command))
}

func isDependencyInstallCommand(command string) bool {
	return strings.Contains(command, "npm ci") ||
		strings.Contains(command, "npm install") ||
		strings.Contains(command, "yarn install") ||
		strings.Contains(command, "pnpm install") ||
		strings.Contains(command, "go mod download")
}

type commandValue struct {
	value string
	line  int
}

func commandValues(node *yaml.Node) []commandValue {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if value := strings.TrimSpace(node.Value); value != "" {
			return []commandValue{{value: value, line: node.Line}}
		}
	case yaml.SequenceNode:
		var result []commandValue
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode {
				if value := strings.TrimSpace(item.Value); value != "" {
					result = append(result, commandValue{value: value, line: item.Line})
				}
			}
		}
		return result
	}
	return nil
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

func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func evidenceContains(values []string, token string) bool {
	token = strings.ToLower(token)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), token) {
			return true
		}
	}
	return false
}

func hasArtifact(values map[string][]string, keys ...string) bool {
	for _, key := range keys {
		if len(values[key]) > 0 {
			return true
		}
	}
	return false
}
