package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autoci-ai/autoci/internal/buildinfo"
)

type Snapshot struct {
	Workflow    string `json:"workflow"`
	GeneratedAt string `json:"generatedAt"`
	Command     string `json:"command"`
	Version     string `json:"version"`
	Data        any    `json:"data"`
}

type StoredItem struct {
	ID       string
	Evidence string
	Source   string
}

func Write(repoPath, command, workflow string, data any) error {
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	snapshot := Snapshot{
		Workflow:    workflow,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Command:     command,
		Version:     buildinfo.Version,
		Data:        json.RawMessage(payload),
	}
	out, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(repoPath, ".autoci", command)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, WorkflowFile(workflow)), append(out, '\n'), 0o644)
}

func WriteFix(repoPath string, data any) error {
	id := extractID(data)
	if id == "" {
		id = "fix"
	}
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	snapshot := Snapshot{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Command:     "fixes",
		Version:     buildinfo.Version,
		Data:        json.RawMessage(payload),
	}
	out, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(repoPath, ".autoci", "fixes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, slug(id)+".json"), append(out, '\n'), 0o644)
}

func FindItem(repoPath, workflow, id string) (StoredItem, bool) {
	for _, command := range []string{"research", "failures", "profile"} {
		snapshot, err := Read(repoPath, command, workflow)
		if err != nil {
			continue
		}
		if item, ok := findInSnapshot(command, snapshot, id); ok {
			return item, true
		}
	}
	return StoredItem{}, false
}

func Read(repoPath, command, workflow string) (Snapshot, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, ".autoci", command, WorkflowFile(workflow)))
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func WorkflowFile(workflow string) string {
	return slug(strings.TrimSuffix(filepath.Base(workflow), filepath.Ext(workflow))) + ".json"
}

func findInSnapshot(command string, snapshot Snapshot, id string) (StoredItem, bool) {
	var root map[string]any
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return StoredItem{}, false
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return StoredItem{}, false
	}
	switch command {
	case "research":
		return findInArray(command, root["opportunities"], id)
	case "failures":
		return findInArray(command, root["failureThemes"], id)
	case "profile":
		return findInArray(command, root["findings"], id)
	default:
		return StoredItem{}, false
	}
}

func findInArray(source string, value any, id string) (StoredItem, bool) {
	items, ok := value.([]any)
	if !ok {
		return StoredItem{}, false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object["id"]) != id {
			continue
		}
		evidence := stringValue(object["evidence"])
		if evidence == "" {
			evidence = stringValue(object["signature"])
		}
		return StoredItem{ID: id, Evidence: evidence, Source: source}, true
	}
	return StoredItem{}, false
}

func extractID(data any) string {
	encoded, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return ""
	}
	return stringValue(object["id"])
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return ""
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
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "default"
	}
	return result
}

func IsMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
