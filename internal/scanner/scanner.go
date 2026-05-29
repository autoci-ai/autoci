package scanner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Workflow struct {
	Path string
	Root *yaml.Node
}

func Scan(repoPath string) ([]Workflow, error) {
	base := filepath.Join(repoPath, ".depot", "workflows")
	var workflows []Workflow
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !isYAML(path) {
			return nil
		}
		root, err := parseYAML(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if root != nil {
			workflows = append(workflows, Workflow{Path: path, Root: root})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].Path < workflows[j].Path
	})
	return workflows, nil
}

func SelectWorkflow(repoPath string, workflows []Workflow, requested, command string) (Workflow, error) {
	if len(workflows) == 0 {
		return Workflow{}, fmt.Errorf("no Depot workflows detected in .depot/workflows")
	}
	if requested == "" {
		if len(workflows) == 1 {
			return workflows[0], nil
		}
		return Workflow{}, multipleWorkflowsError(workflows, command)
	}
	normalizedRequest := normalizeWorkflowSelector(requested)
	for _, workflow := range workflows {
		if workflowMatches(repoPath, workflow, normalizedRequest) {
			return workflow, nil
		}
	}
	return Workflow{}, fmt.Errorf("Depot workflow %q was not found.\n\nAvailable workflows:\n%s", requested, workflowList(workflows))
}

func WorkflowName(repoPath string, workflow Workflow) string {
	rel, err := filepath.Rel(filepath.Join(repoPath, ".depot", "workflows"), workflow.Path)
	if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(workflow.Path)
}

func isYAML(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func parseYAML(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.TrimSpace(data) == nil {
		return nil, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	return &root, nil
}

func workflowMatches(repoPath string, workflow Workflow, normalizedRequest string) bool {
	names := []string{
		normalizeWorkflowSelector(filepath.Base(workflow.Path)),
		normalizeWorkflowSelector(WorkflowName(repoPath, workflow)),
	}
	if rel, err := filepath.Rel(repoPath, workflow.Path); err == nil {
		names = append(names, normalizeWorkflowSelector(rel))
	}
	for _, name := range names {
		if name == normalizedRequest {
			return true
		}
	}
	return false
}

func normalizeWorkflowSelector(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	return strings.ToLower(value)
}

func multipleWorkflowsError(workflows []Workflow, command string) error {
	if command == "" {
		command = "analyze"
	}
	return fmt.Errorf("multiple Depot workflows detected:\n%s\nSpecify one:\n  autoci %s --workflow %s", workflowList(workflows), command, filepath.Base(workflows[0].Path))
}

func workflowList(workflows []Workflow) string {
	names := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		names = append(names, filepath.Base(workflow.Path))
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		fmt.Fprintf(&builder, "  - %s\n", name)
	}
	return strings.TrimRight(builder.String(), "\n")
}
