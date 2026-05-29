package findings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/state"
)

type Options struct {
	Workflow               string
	IncludeFailures        bool
	IncludeProfile         bool
	ReliabilityOnly        bool
	OptimizationOnly       bool
	ActiveOnly             bool
	NewOnly                bool
	AwaitingValidationOnly bool
	Limit                  int
}

type Finding struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Category    string   `json:"category"`
	Priority    string   `json:"priority"`
	Workflow    string   `json:"workflow,omitempty"`
	Jobs        []string `json:"jobs,omitempty"`
	Evidence    string   `json:"evidence,omitempty"`
	Gaps        []Gap    `json:"gaps,omitempty"`
	NextCommand string   `json:"nextCommand"`
	NextAction  string   `json:"nextAction"`
	Status      string   `json:"status"`

	Occurrences int     `json:"occurrences,omitempty"`
	FailureRate float64 `json:"failureRate,omitempty"`
	Confidence  int     `json:"confidence,omitempty"`
	State       string  `json:"state,omitempty"`

	kindRank   int
	statusRank int
}

type Gap struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type researchState struct {
	ID        string              `json:"id"`
	Readiness lifecycle.Readiness `json:"readiness"`
	Gaps      []Gap               `json:"gaps"`
	NextSteps []string            `json:"recommendedInvestigation"`
}

type fixState struct {
	ID             string              `json:"id"`
	SourceItemID   string              `json:"sourceItemId"`
	SourceID       string              `json:"sourceId"`
	Readiness      lifecycle.Readiness `json:"readiness"`
	FixType        string              `json:"fixType"`
	PatchGenerated bool                `json:"patchGenerated"`
	PatchApplied   bool                `json:"patchApplied"`
}

func Load(repoPath string, options Options) ([]Finding, error) {
	if !options.IncludeFailures && !options.IncludeProfile {
		options.IncludeFailures = true
		options.IncludeProfile = true
	}
	if !options.ReliabilityOnly && !options.OptimizationOnly {
		options.ReliabilityOnly = true
		options.OptimizationOnly = true
	}
	var result []Finding
	if options.IncludeFailures {
		findings, err := loadFailures(repoPath, options.Workflow)
		if err != nil {
			return nil, err
		}
		result = append(result, findings...)
	}
	if options.IncludeProfile {
		findings, err := loadProfile(repoPath, options.Workflow)
		if err != nil {
			return nil, err
		}
		result = append(result, findings...)
	}
	enrichWithState(repoPath, result)
	result = filterCategory(result, options)
	result = filterStatus(result, options)
	sortFindings(result)
	if options.Limit > 0 && len(result) > options.Limit {
		result = result[:options.Limit]
	}
	return result, nil
}

func loadFailures(repoPath, workflow string) ([]Finding, error) {
	records, err := state.List(repoPath, "failures")
	if err != nil {
		return nil, err
	}
	var result []Finding
	for _, record := range records {
		if !workflowMatches(record.Workflow, workflow) {
			continue
		}
		var analysis failures.Analysis
		if !decodeSnapshot(record.Snapshot.Data, &analysis) {
			continue
		}
		recordWorkflow := firstNonEmpty(record.Workflow, analysis.Workflow)
		rate := failureRate(analysis)
		for _, theme := range analysis.FailureThemes {
			result = append(result, Finding{
				ID:          theme.ID,
				Source:      "failures",
				Category:    "reliability",
				Priority:    "high",
				Workflow:    recordWorkflow,
				Jobs:        uniqueStrings(theme.Jobs),
				Evidence:    failureEvidence(theme),
				NextCommand: "autoci research " + theme.ID,
				NextAction:  "research",
				Status:      "new",
				Occurrences: theme.Occurrences,
				FailureRate: rate,
				Confidence:  confidenceFromOccurrences(theme.Occurrences),
				kindRank:    kindRank(theme.ID),
			})
		}
		for _, aggregation := range analysis.AggregationJobs {
			id := "repeated-failures-workflow"
			result = append(result, Finding{
				ID:          id,
				Source:      "failures",
				Category:    "reliability",
				Priority:    "high",
				Workflow:    recordWorkflow,
				Jobs:        []string{aggregation.Job},
				Evidence:    fmt.Sprintf("%d repeated failures reported by %s.", aggregation.Occurrences, aggregation.Job),
				NextCommand: "autoci research " + id,
				NextAction:  "research",
				Status:      "new",
				Occurrences: aggregation.Occurrences,
				FailureRate: rate,
				Confidence:  confidenceFromOccurrences(aggregation.Occurrences),
				kindRank:    kindRank(id),
			})
		}
	}
	return result, nil
}

func loadProfile(repoPath, workflow string) ([]Finding, error) {
	records, err := state.List(repoPath, "profile")
	if err != nil {
		return nil, err
	}
	var result []Finding
	for _, record := range records {
		if !workflowMatches(record.Workflow, workflow) {
			continue
		}
		var prof profile.Profile
		if !decodeSnapshot(record.Snapshot.Data, &prof) {
			continue
		}
		for _, finding := range prof.Findings {
			findingWorkflow := firstNonEmpty(finding.Workflow, record.Workflow)
			if workflow != "" && !workflowMatches(findingWorkflow, workflow) {
				continue
			}
			category := categoryForID(finding.ID)
			result = append(result, Finding{
				ID:          finding.ID,
				Source:      "profile",
				Category:    category,
				Priority:    priorityFor(category, finding.Severity),
				Workflow:    findingWorkflow,
				Jobs:        uniqueStrings(compactStrings([]string{finding.Job})),
				Evidence:    finding.Evidence,
				NextCommand: "autoci research " + finding.ID,
				NextAction:  "research",
				Status:      "new",
				FailureRate: parsedFailureRate(finding.Evidence),
				Confidence:  confidenceFromSeverity(finding.Severity),
				kindRank:    kindRank(finding.ID),
			})
		}
	}
	return result, nil
}

func enrichWithState(repoPath string, items []Finding) {
	researchByID := readResearchState(repoPath)
	fixByID := readFixState(repoPath)
	for i := range items {
		item := &items[i]
		applyResearchState(item, researchByID[item.ID])
		applyFixState(item, fixByID[item.ID])
		item.statusRank = statusRank(item.Status)
	}
}

func readResearchState(repoPath string) map[string]researchState {
	result := map[string]researchState{}
	base := filepath.Join(repoPath, ".autoci", "research")
	entries, err := filepath.Glob(filepath.Join(base, "*", "evidence.json"))
	if err != nil {
		return result
	}
	for _, path := range entries {
		snapshot, err := readJSONFile[researchState](path)
		if err != nil || snapshot.ID == "" {
			continue
		}
		result[snapshot.ID] = snapshot
	}
	return result
}

func readJSONFile[T any](path string) (T, error) {
	var result T
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func readFixState(repoPath string) map[string]fixState {
	result := map[string]fixState{}
	records, err := state.List(repoPath, "fixes")
	if err != nil {
		return result
	}
	for _, record := range records {
		var fix fixState
		if !decodeSnapshot(record.Snapshot.Data, &fix) {
			continue
		}
		sourceID := firstNonEmpty(fix.SourceID, fix.SourceItemID)
		if sourceID == "" {
			continue
		}
		result[sourceID] = fix
	}
	return result
}

func applyResearchState(item *Finding, research researchState) {
	if research.ID == "" {
		item.Status = "new"
		item.NextAction = "research"
		item.NextCommand = "autoci research " + item.ID
		return
	}
	item.Gaps = research.Gaps
	switch research.Readiness {
	case lifecycle.ReadinessReadyForFix:
		item.Status = "ready_for_fix"
		item.NextAction = "fix"
		item.NextCommand = "autoci fix " + item.ID
	case lifecycle.ReadinessNeedsMoreEvidence, lifecycle.ReadinessNotReady:
		item.Status = "needs_more_evidence"
		item.NextAction = "inspect_evidence_gaps"
		item.NextCommand = nextStepFromResearch(research)
	case lifecycle.ReadinessFixed:
		item.Status = "fix_applied"
		item.NextAction = "wait_for_validation"
		item.NextCommand = "wait for validation after the applied fix"
	case lifecycle.ReadinessValidated:
		item.Status = "validated"
		item.NextAction = "none"
		item.NextCommand = ""
	default:
		item.Status = "researched"
		item.NextAction = "review_research"
		item.NextCommand = "autoci research " + item.ID
	}
}

func nextStepFromResearch(research researchState) string {
	for _, step := range research.NextSteps {
		step = strings.TrimSpace(step)
		if step != "" {
			return step
		}
	}
	if len(research.Gaps) > 0 {
		return "inspect evidence gaps"
	}
	return "review targeted research"
}

func applyFixState(item *Finding, fix fixState) {
	if fix.SourceID == "" && fix.SourceItemID == "" {
		return
	}
	if fix.Readiness == lifecycle.ReadinessValidated {
		item.Status = "validated"
		item.NextAction = "none"
		item.NextCommand = ""
		return
	}
	if fix.PatchApplied {
		item.Status = "awaiting_validation"
		item.NextAction = "wait_for_runs"
		item.NextCommand = "wait for future CI runs to collect diagnostics"
		return
	}
	if fix.PatchGenerated {
		item.Status = "fix_applied"
		item.NextAction = "apply_fix"
		item.NextCommand = "autoci fix " + item.ID
	}
}

func filterStatus(items []Finding, options Options) []Finding {
	var result []Finding
	for _, item := range items {
		if options.NewOnly && item.Status != "new" {
			continue
		}
		if options.AwaitingValidationOnly && item.Status != "awaiting_validation" {
			continue
		}
		if options.ActiveOnly && !isActiveStatus(item.Status) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func isActiveStatus(status string) bool {
	switch status {
	case "new", "researched", "needs_more_evidence", "ready_for_fix", "fix_applied":
		return true
	default:
		return false
	}
}

func filterCategory(items []Finding, options Options) []Finding {
	var result []Finding
	for _, item := range items {
		if item.Category == "reliability" && options.ReliabilityOnly {
			result = append(result, item)
		}
		if item.Category == "optimization" && options.OptimizationOnly {
			result = append(result, item)
		}
	}
	return result
}

func sortFindings(items []Finding) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.statusRank != b.statusRank {
			return a.statusRank < b.statusRank
		}
		if a.kindRank != b.kindRank {
			return a.kindRank < b.kindRank
		}
		if a.FailureRate != b.FailureRate {
			return a.FailureRate > b.FailureRate
		}
		if a.Occurrences != b.Occurrences {
			return a.Occurrences > b.Occurrences
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		return a.ID < b.ID
	})
}

func statusRank(status string) int {
	switch status {
	case "new":
		return 1
	case "ready_for_fix":
		return 2
	case "needs_more_evidence", "researched":
		return 3
	case "fix_applied", "awaiting_validation":
		return 4
	case "validated":
		return 5
	default:
		return 50
	}
}

func failureEvidence(theme failures.FailureTheme) string {
	switch len(theme.Jobs) {
	case 0:
		return fmt.Sprintf("%d occurrences.", theme.Occurrences)
	case 1:
		return fmt.Sprintf("%d occurrences in %s.", theme.Occurrences, theme.Jobs[0])
	default:
		return fmt.Sprintf("%d occurrences across %s.", theme.Occurrences, strings.Join(theme.Jobs, ", "))
	}
}

func failureRate(analysis failures.Analysis) float64 {
	if analysis.RunsAnalyzed <= 0 {
		return 0
	}
	return float64(analysis.FailedRuns) / float64(analysis.RunsAnalyzed)
}

func parsedFailureRate(evidence string) float64 {
	lower := strings.ToLower(evidence)
	index := strings.Index(lower, "failure rate ")
	if index < 0 {
		return 0
	}
	start := index + len("failure rate ")
	end := start
	for end < len(lower) && ((lower[end] >= '0' && lower[end] <= '9') || lower[end] == '.') {
		end++
	}
	if end == start || end >= len(lower) || lower[end] != '%' {
		return 0
	}
	var value float64
	if _, err := fmt.Sscanf(lower[start:end], "%f", &value); err != nil {
		return 0
	}
	return value / 100
}

func categoryForID(id string) string {
	switch findingKind(id) {
	case "failure-theme", "flaky-job", "repeated-failures-workflow", "repeated-failures":
		return "reliability"
	default:
		return "optimization"
	}
}

func priorityFor(category, severity string) string {
	if category == "reliability" {
		return "high"
	}
	if severity == "high" {
		return "medium"
	}
	return "medium"
}

func kindRank(id string) int {
	switch findingKind(id) {
	case "failure-theme":
		return 1
	case "flaky-job":
		return 2
	case "repeated-failures-workflow", "repeated-failures":
		return 3
	case "high-variance":
		return 4
	case "long-running-job":
		return 5
	default:
		return 50
	}
}

func findingKind(id string) string {
	for _, prefix := range []string{
		"failure-theme",
		"flaky-job",
		"repeated-failures-workflow",
		"repeated-failures",
		"high-variance",
		"long-running-job",
		"high-leverage-slow-job",
		"critical-path-blocker",
	} {
		if id == prefix || strings.HasPrefix(id, prefix+"-") {
			return prefix
		}
	}
	return id
}

func confidenceFromOccurrences(occurrences int) int {
	switch {
	case occurrences >= 5:
		return 90
	case occurrences >= 3:
		return 75
	case occurrences >= 2:
		return 60
	default:
		return 45
	}
}

func confidenceFromSeverity(severity string) int {
	switch severity {
	case "high":
		return 85
	case "medium":
		return 70
	case "low":
		return 55
	default:
		return 50
	}
}

func workflowMatches(stored, requested string) bool {
	if requested == "" {
		return true
	}
	stored = strings.TrimSuffix(stored, ".yml")
	stored = strings.TrimSuffix(stored, ".yaml")
	requested = strings.TrimSuffix(requested, ".yml")
	requested = strings.TrimSuffix(requested, ".yaml")
	return stored == requested
}

func decodeSnapshot(value any, target any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	var result []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
