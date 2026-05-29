package scanner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Workflow struct {
	Path string
	Root *yaml.Node
}

func Scan(repoPath string) ([]Workflow, error) {
	var workflows []Workflow
	err := filepath.WalkDir(repoPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor":
				if path != repoPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !isDepotCandidate(path) {
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
	return workflows, nil
}

func isDepotCandidate(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
		return false
	}
	for _, part := range strings.Split(strings.ToLower(filepath.ToSlash(filepath.Dir(path))), "/") {
		switch part {
		case ".depot", "depot", "depot-ci", "depot_ci":
			return true
		}
	}
	return strings.Contains(name, "depot")
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
