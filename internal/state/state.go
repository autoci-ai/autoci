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

type SnapshotRecord struct {
	Command  string
	Workflow string
	Path     string
	Snapshot Snapshot
}

type StoredItem struct {
	ID             string
	Evidence       string
	Source         string
	Jobs           []string
	Occurrences    int
	Signature      string
	ExtractedItems []string
	Artifacts      map[string][]string
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

func WriteTargetedResearch(repoPath, id string, evidence any, markdown []byte) (string, string, error) {
	dir := filepath.Join(repoPath, ".autoci", "research", slug(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	payload, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return "", "", err
	}
	evidencePath := filepath.Join(dir, "evidence.json")
	reportPath := filepath.Join(dir, "research.md")
	if err := os.WriteFile(evidencePath, append(payload, '\n'), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(reportPath, markdown, 0o644); err != nil {
		return "", "", err
	}
	return evidencePath, reportPath, nil
}

func ReadTargetedResearch[T any](repoPath, id string) (*T, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, ".autoci", "research", slug(id), "evidence.json"))
	if err != nil {
		return nil, err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func UpdateTargetedResearchReadiness(repoPath, id string, readiness any) error {
	path := filepath.Join(repoPath, ".autoci", "research", slug(id), "evidence.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	object["readiness"] = readiness
	if notes, ok := object["fixNotes"].(map[string]any); ok {
		notes["readiness"] = readiness
	}
	encoded, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func WriteFix(repoPath string, data any) error {
	id := extractID(data)
	if id == "" {
		id = "fix"
	}
	key := extractSourceID(data)
	if key == "" {
		key = id
	}
	dir := filepath.Join(repoPath, ".autoci", "fixes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, slug(key)+".json")
	data = mergeFixState(path, data)
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
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	legacyPath := filepath.Join(dir, slug(id)+".json")
	if legacyPath != path {
		_ = os.Remove(legacyPath)
	}
	return nil
}

func ReadFix(repoPath, sourceID string) (Snapshot, error) {
	dir := filepath.Join(repoPath, ".autoci", "fixes")
	if snapshot, err := readSnapshotFile(filepath.Join(dir, slug(sourceID)+".json")); err == nil {
		return snapshot, nil
	}
	return readSnapshotFile(filepath.Join(dir, slug(legacyFixID(sourceID))+".json"))
}

func mergeFixState(path string, next any) any {
	merged := mergeFixChangeHistory(nil, next)
	existing, err := readSnapshotFile(path)
	if err != nil {
		return merged
	}
	var existingData map[string]any
	if !decodeData(existing.Data, &existingData) {
		return merged
	}
	var nextData map[string]any
	if !decodeData(next, &nextData) {
		return next
	}
	nextData["changes"] = mergedFixChanges(existingData, nextData)
	if !boolValue(existingData["patchApplied"]) {
		return nextData
	}
	if boolValue(nextData["patchApplied"]) || boolValue(nextData["patchGenerated"]) {
		return nextData
	}
	existingData["lastRun"] = map[string]any{
		"patchGenerated": boolValue(nextData["patchGenerated"]),
		"patchApplied":   boolValue(nextData["patchApplied"]),
		"reason":         stringValue(nextData["reason"]),
		"idempotent":     isIdempotentFixRun(nextData),
	}
	existingData["changes"] = nextData["changes"]
	return existingData
}

func mergeFixChangeHistory(existing any, next any) any {
	var nextData map[string]any
	if !decodeData(next, &nextData) {
		return next
	}
	var existingData map[string]any
	if existing != nil {
		_ = decodeData(existing, &existingData)
	}
	nextData["changes"] = mergedFixChanges(existingData, nextData)
	return nextData
}

func mergedFixChanges(existingData, nextData map[string]any) []map[string]any {
	var result []map[string]any
	seen := map[string]bool{}
	appendChange := func(change map[string]any) {
		normalized := normalizeChange(change)
		if len(normalized) == 0 {
			return
		}
		key := stringValue(normalized["id"]) + "|" + stringValue(normalized["branch"]) + "|" + stringValue(normalized["changeType"])
		if key == "||" || seen[key] {
			return
		}
		seen[key] = true
		result = append(result, normalized)
	}
	for _, change := range changesFromFixData(existingData) {
		appendChange(change)
	}
	for _, change := range changesFromFixData(nextData) {
		appendChange(change)
	}
	if result == nil {
		return []map[string]any{}
	}
	return result
}

func changesFromFixData(data map[string]any) []map[string]any {
	if len(data) == 0 {
		return nil
	}
	var result []map[string]any
	if values, ok := data["changes"].([]any); ok {
		for _, value := range values {
			if change, ok := value.(map[string]any); ok {
				result = append(result, change)
			}
		}
	}
	if change, ok := data["change"].(map[string]any); ok {
		result = append(result, change)
	}
	if boolValue(data["patchGenerated"]) || boolValue(data["patchApplied"]) {
		if synthesized := synthesizeChange(data); len(synthesized) > 0 {
			result = append(result, synthesized)
		}
	}
	return result
}

func normalizeChange(change map[string]any) map[string]any {
	id := stringValue(change["id"])
	findingID := firstNonEmptyString(stringValue(change["findingId"]), stringValue(change["sourceItemId"]), stringValue(change["sourceId"]))
	changeType := stringValue(change["changeType"])
	branch := firstNonEmptyString(stringValue(change["branch"]), stringValue(change["branchName"]))
	if id == "" && changeType == "" && branch == "" {
		return nil
	}
	return map[string]any{
		"id":         id,
		"findingId":  findingID,
		"changeType": changeType,
		"branch":     branch,
	}
}

func synthesizeChange(data map[string]any) map[string]any {
	changeType := stringValue(data["fixType"])
	if changeType == "root_cause" {
		changeType = "root_cause"
	}
	id := stringValue(data["changeId"])
	if id == "" {
		id = changeIDForState(changeType)
	}
	branch := stringValue(data["branch"])
	if branch == "" {
		return nil
	}
	return map[string]any{
		"id":         id,
		"findingId":  firstNonEmptyString(stringValue(data["sourceItemId"]), stringValue(data["sourceId"])),
		"changeType": changeType,
		"branch":     branch,
	}
}

func changeIDForState(changeType string) string {
	switch changeType {
	case "instrumentation":
		return "instrumentation-001"
	case "instrumentation_update":
		return "instrumentation-update-001"
	case "root_cause":
		return "root-cause-001"
	default:
		if changeType == "" {
			return ""
		}
		return changeType + "-001"
	}
}

func isIdempotentFixRun(data map[string]any) bool {
	reason := strings.ToLower(stringValue(data["reason"]))
	return strings.Contains(reason, "already contains") || strings.Contains(reason, "already applied")
}

func FindItem(repoPath, workflow, id string) (StoredItem, bool) {
	var result StoredItem
	found := false
	for _, command := range []string{"research", "failures", "profile"} {
		snapshot, err := Read(repoPath, command, workflow)
		if err != nil {
			continue
		}
		if item, ok := findInSnapshot(command, snapshot, id); ok {
			if !found {
				result = item
				found = true
				continue
			}
			result = mergeStoredItems(result, item)
		}
	}
	return result, found
}

func Read(repoPath, command, workflow string) (Snapshot, error) {
	return readSnapshotFile(filepath.Join(repoPath, ".autoci", command, WorkflowFile(workflow)))
}

func readSnapshotFile(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func List(repoPath, command string) ([]SnapshotRecord, error) {
	dir := filepath.Join(repoPath, ".autoci", command)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var result []SnapshotRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var snapshot Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return nil, err
		}
		result = append(result, SnapshotRecord{
			Command:  command,
			Workflow: snapshot.Workflow,
			Path:     path,
			Snapshot: snapshot,
		})
	}
	return result, nil
}

func ReadData[T any](repoPath, command, workflow string) (*T, error) {
	snapshot, err := Read(repoPath, command, workflow)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return nil, err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
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
		if item, ok := findInArray(command, root["failureThemes"], id); ok {
			return item, ok
		}
		return findInArray(command, root["findings"], id)
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
		if !ok || !idsMatch(stringValue(object["id"]), id) {
			continue
		}
		evidence := stringValue(object["evidence"])
		if evidence == "" {
			evidence = stringValue(object["signature"])
		}
		jobs := stringSlice(object["jobs"])
		if job := stringValue(object["job"]); job != "" {
			jobs = append(jobs, job)
		}
		return StoredItem{
			ID:             id,
			Evidence:       evidence,
			Source:         source,
			Jobs:           jobs,
			Occurrences:    intValue(object["occurrences"]),
			Signature:      stringValue(object["signature"]),
			ExtractedItems: extractedItems(object),
			Artifacts:      mergeArtifactMaps(artifactMap(object["artifacts"]), evidenceArtifactMap(object["evidence"])),
		}, true
	}
	return StoredItem{}, false
}

func mergeStoredItems(base, extra StoredItem) StoredItem {
	if base.Evidence == "" {
		base.Evidence = extra.Evidence
	}
	if base.Signature == "" {
		base.Signature = extra.Signature
	}
	if base.Occurrences == 0 {
		base.Occurrences = extra.Occurrences
	}
	base.Jobs = mergeStringSlices(base.Jobs, extra.Jobs)
	base.ExtractedItems = mergeStringSlices(base.ExtractedItems, extra.ExtractedItems)
	if base.Artifacts == nil {
		base.Artifacts = map[string][]string{}
	}
	for key, items := range extra.Artifacts {
		base.Artifacts[key] = mergeStringSlices(base.Artifacts[key], items)
	}
	return base
}

func idsMatch(stored, requested string) bool {
	if stored == requested {
		return true
	}
	return normalizeItemID(stored) == normalizeItemID(requested)
}

func normalizeItemID(id string) string {
	id = strings.TrimPrefix(id, "research-")
	id = strings.TrimPrefix(id, "reliability-")
	if strings.HasPrefix(id, "failure-theme-") {
		id = strings.TrimPrefix(id, "failure-theme-")
	}
	id = strings.TrimPrefix(id, "failure-")
	return id
}

func extractID(data any) string {
	var object map[string]any
	if !decodeData(data, &object) {
		return ""
	}
	return stringValue(object["id"])
}

func extractSourceID(data any) string {
	var object map[string]any
	if !decodeData(data, &object) {
		return ""
	}
	if sourceID := stringValue(object["sourceId"]); sourceID != "" {
		return sourceID
	}
	return stringValue(object["sourceItemId"])
}

func decodeData(data any, target any) bool {
	encoded, err := json.Marshal(data)
	if err != nil {
		return false
	}
	return json.Unmarshal(encoded, target) == nil
}

func legacyFixID(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return "fix"
	}
	if strings.HasPrefix(sourceID, "fix-") {
		return sourceID
	}
	sourceID = strings.TrimPrefix(sourceID, "failure-theme-")
	sourceID = strings.TrimPrefix(sourceID, "theme-")
	return "fix-" + sourceID
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

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range items {
		if str := stringValue(item); str != "" {
			result = append(result, str)
		}
	}
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func extractedItems(object map[string]any) []string {
	seen := map[string]bool{}
	var result []string
	for _, items := range artifactMap(object["artifacts"]) {
		for _, item := range items {
			if !seen[item] {
				seen[item] = true
				result = append(result, item)
			}
		}
	}
	evidenceItems, ok := object["evidence"].([]any)
	if !ok {
		return result
	}
	for _, item := range evidenceItems {
		evidence, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"image", "packageName", "registryUrl", "registryHost", "registry", "testName", "sourceFile", "buildTarget", "missingDependency"} {
			if extracted := stringValue(evidence[key]); extracted != "" && !seen[extracted] {
				seen[extracted] = true
				result = append(result, extracted)
			}
		}
		for _, extracted := range stringSlice(evidence["extractedItems"]) {
			if !seen[extracted] {
				seen[extracted] = true
				result = append(result, extracted)
			}
		}
	}
	return result
}

func artifactMap(value any) map[string][]string {
	result := map[string][]string{}
	object, ok := value.(map[string]any)
	if !ok {
		return result
	}
	for key, raw := range object {
		items := stringSlice(raw)
		if len(items) > 0 {
			result[key] = items
		}
	}
	return result
}

func evidenceArtifactMap(value any) map[string][]string {
	result := map[string][]string{}
	items, ok := value.([]any)
	if !ok {
		return result
	}
	for _, item := range items {
		evidence, ok := item.(map[string]any)
		if !ok {
			continue
		}
		addArtifact(result, "images", stringValue(evidence["image"]))
		addArtifact(result, "packages", stringValue(evidence["packageName"]))
		addArtifact(result, "urls", stringValue(evidence["registryUrl"]))
		if host := stringValue(evidence["registryHost"]); host != "" {
			addArtifact(result, "hosts", host)
		} else {
			addArtifact(result, "hosts", stringValue(evidence["registry"]))
		}
		if sourceFile := stringValue(evidence["sourceFile"]); sourceFile != "" && typedSourceEvidence(evidence) {
			addArtifact(result, "modules", sourceFile)
		}
	}
	return result
}

func typedSourceEvidence(evidence map[string]any) bool {
	for _, key := range []string{"testName", "compilerError", "missingDependency", "assertionMessage"} {
		if stringValue(evidence[key]) != "" {
			return true
		}
	}
	return false
}

func addArtifact(items map[string][]string, key, value string) {
	if value == "" {
		return
	}
	items[key] = mergeStringSlices(items[key], []string{value})
}

func mergeArtifactMaps(a, b map[string][]string) map[string][]string {
	result := map[string][]string{}
	for key, items := range a {
		result[key] = mergeStringSlices(result[key], items)
	}
	for key, items := range b {
		result[key] = mergeStringSlices(result[key], items)
	}
	return result
}

func mergeStringSlices(a, b []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range append(a, b...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
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
