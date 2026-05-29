package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/scanner"
	"github.com/autoci-ai/autoci/internal/state"
	stepresolver "github.com/autoci-ai/autoci/internal/workflow"
)

type TargetReport struct {
	ID                       string                       `json:"id"`
	Type                     string                       `json:"type"`
	Workflow                 string                       `json:"workflow,omitempty"`
	Jobs                     []string                     `json:"jobs,omitempty"`
	Occurrences              int                          `json:"occurrences,omitempty"`
	Impact                   string                       `json:"impact,omitempty"`
	Confidence               int                          `json:"confidence,omitempty"`
	FailureTheme             *failures.FailureTheme       `json:"failureTheme,omitempty"`
	ProfileFinding           *profile.Finding             `json:"profileFinding,omitempty"`
	ResearchOpportunity      *ResearchOpportunity         `json:"researchOpportunity,omitempty"`
	SourceCommands           []SourceCommandData          `json:"sourceCommands,omitempty"`
	RunIDs                   []string                     `json:"runIds,omitempty"`
	LogExcerpts              []string                     `json:"logExcerpts,omitempty"`
	Artifacts                map[string][]string          `json:"artifacts,omitempty"`
	WorkflowContext          *WorkflowContext             `json:"workflowContext,omitempty"`
	CandidateSteps           []stepresolver.CandidateStep `json:"candidateSteps,omitempty"`
	CurrentFinding           CurrentFinding               `json:"currentFinding"`
	Evidence                 []string                     `json:"evidence"`
	WhatWeKnow               []string                     `json:"whatWeKnow"`
	WhatWeDoNotKnowYet       []string                     `json:"whatWeDoNotKnowYet"`
	Gaps                     []EvidenceGap                `json:"gaps,omitempty"`
	RootCauseHypotheses      []RootCauseHypothesis        `json:"rootCauseHypotheses"`
	RecommendedInvestigation []string                     `json:"recommendedInvestigation"`
	CandidateFixes           []string                     `json:"candidateFixes"`
	Readiness                lifecycle.Readiness          `json:"readiness"`
	FixNotes                 FixNotes                     `json:"fixNotes"`
}

type SourceCommandData struct {
	Command     string          `json:"command"`
	Workflow    string          `json:"workflow,omitempty"`
	GeneratedAt string          `json:"generatedAt,omitempty"`
	Item        json.RawMessage `json:"item"`
}

type WorkflowContext struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type CurrentFinding struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Workflow    string   `json:"workflow,omitempty"`
	Jobs        []string `json:"jobs,omitempty"`
	Occurrences int      `json:"occurrences,omitempty"`
	Impact      string   `json:"impact,omitempty"`
	Confidence  int      `json:"confidence,omitempty"`
}

type FixNotes struct {
	ID             string                       `json:"id"`
	Readiness      lifecycle.Readiness          `json:"readiness"`
	Workflow       string                       `json:"workflow,omitempty"`
	Jobs           []string                     `json:"jobs,omitempty"`
	Artifacts      map[string][]string          `json:"artifacts,omitempty"`
	Hypotheses     []RootCauseHypothesis        `json:"hypotheses,omitempty"`
	NextSteps      []string                     `json:"nextSteps,omitempty"`
	CandidateSteps []stepresolver.CandidateStep `json:"candidateSteps,omitempty"`
}

type EvidenceGap struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type targetAccumulator struct {
	id          string
	workflow    string
	theme       *failures.FailureTheme
	profile     *profile.Finding
	opportunity *ResearchOpportunity
	sources     []SourceCommandData
}

func Targeted(repoPath, requestedWorkflow, id string) (TargetReport, error) {
	acc := targetAccumulator{id: id}
	for _, command := range []string{"failures", "profile", "research", "bottlenecks"} {
		records, err := state.List(repoPath, command)
		if err != nil {
			return TargetReport{}, err
		}
		for _, record := range records {
			if requestedWorkflow != "" && !workflowMatches(record.Workflow, requestedWorkflow) {
				continue
			}
			acc.consume(record)
		}
	}
	if acc.theme == nil && acc.profile == nil && acc.opportunity == nil {
		return TargetReport{}, fmt.Errorf("research target %q was not found in .autoci state", id)
	}
	return acc.report(repoPath), nil
}

func (acc *targetAccumulator) consume(record state.SnapshotRecord) {
	switch record.Command {
	case "failures":
		var analysis failures.Analysis
		if !snapshotData(record.Snapshot, &analysis) {
			return
		}
		for _, theme := range analysis.FailureThemes {
			if idsEquivalent(theme.ID, acc.id) {
				item := theme
				acc.theme = &item
				acc.setWorkflow(record.Workflow, analysis.Workflow)
				acc.addSource(record, item)
			}
		}
		for _, finding := range analysis.Findings {
			if idsEquivalent(finding.ID, acc.id) {
				theme := failures.FailureTheme{
					ID:          finding.ID,
					Signature:   finding.Signature,
					Occurrences: finding.Occurrences,
					Jobs:        finding.Jobs,
					Evidence:    finding.Evidence,
				}
				acc.theme = &theme
				acc.setWorkflow(record.Workflow, analysis.Workflow)
				acc.addSource(record, finding)
			}
		}
	case "profile", "bottlenecks":
		var prof profile.Profile
		if !snapshotData(record.Snapshot, &prof) {
			return
		}
		for _, finding := range prof.Findings {
			if idsEquivalent(finding.ID, acc.id) {
				item := finding
				acc.profile = &item
				acc.setWorkflow(record.Workflow, finding.Workflow)
				acc.addSource(record, item)
			}
		}
	case "research":
		var plan Plan
		if !snapshotData(record.Snapshot, &plan) {
			return
		}
		for _, opportunity := range plan.Opportunities {
			if idsEquivalent(opportunity.ID, acc.id) {
				item := opportunity
				acc.opportunity = &item
				acc.setWorkflow(record.Workflow, plan.Workflow)
				acc.addSource(record, item)
			}
		}
	}
}

func (acc *targetAccumulator) report(repoPath string) TargetReport {
	workflow := acc.workflow
	report := TargetReport{
		ID:                  stableTargetID(acc.id, acc.theme, acc.profile, acc.opportunity),
		Workflow:            workflow,
		Artifacts:           map[string][]string{},
		SourceCommands:      acc.sources,
		FailureTheme:        acc.theme,
		ProfileFinding:      acc.profile,
		ResearchOpportunity: acc.opportunity,
	}
	if acc.theme != nil {
		report.Type = "failure_theme"
		report.Jobs = uniqueSortedCopy(acc.theme.Jobs)
		report.Occurrences = acc.theme.Occurrences
		report.LogExcerpts = failureLogExcerpts(acc.theme.Evidence)
		report.RunIDs = failureRunIDs(acc.theme.Evidence)
		report.Artifacts = failureArtifacts(acc.theme)
		if report.ResearchOpportunity == nil {
			opportunity := opportunityFromFailureTheme(workflow, *acc.theme)
			report.ResearchOpportunity = &opportunity
		}
	} else if acc.profile != nil {
		report.Type = "profile_finding"
		report.Jobs = uniqueSortedCopy(compactStrings([]string{acc.profile.Job}))
		report.Impact = acc.profile.Evidence
		report.Confidence = confidenceFromSeverity(acc.profile.Severity)
		if report.ResearchOpportunity == nil {
			opportunity, _ := opportunityFor(workflow, *acc.profile)
			report.ResearchOpportunity = &opportunity
		}
	} else if acc.opportunity != nil {
		report.Type = "research_opportunity"
		report.Jobs = uniqueSortedCopy(acc.opportunity.RawEvidence.Jobs)
		report.Impact = acc.opportunity.EstimatedImpact
		report.Confidence = confidenceForOpportunity(*acc.opportunity)
		report.Artifacts = rawArtifacts(acc.opportunity.RawEvidence)
		report.LogExcerpts = acc.opportunity.RawEvidence.LogExcerpts
	}
	if report.ResearchOpportunity != nil {
		report.Confidence = maxInt(report.Confidence, confidenceFromHypotheses(report.ResearchOpportunity.Hypotheses))
		report.Artifacts = mergeArtifactsMap(report.Artifacts, rawArtifacts(report.ResearchOpportunity.RawEvidence))
		report.LogExcerpts = uniqueSortedCopy(append(report.LogExcerpts, report.ResearchOpportunity.RawEvidence.LogExcerpts...))
	}
	report.WorkflowContext = workflowContext(repoPath, workflow)
	report.CandidateSteps = candidateStepsForReport(repoPath, report)
	report.CurrentFinding = CurrentFinding{ID: report.ID, Type: report.Type, Workflow: report.Workflow, Jobs: report.Jobs, Occurrences: report.Occurrences, Impact: report.Impact, Confidence: report.Confidence}
	report.Evidence = targetedEvidence(report)
	report.WhatWeKnow = targetedKnownFacts(report)
	report.Gaps = targetedGaps(report)
	report.WhatWeDoNotKnowYet = gapMessages(report.Gaps)
	report.RootCauseHypotheses = targetedHypotheses(report)
	report.RecommendedInvestigation = targetedInvestigation(report)
	report.CandidateFixes = targetedFixes(report)
	report.Readiness = targetedReadiness(report)
	report.FixNotes = FixNotes{ID: report.ID, Readiness: report.Readiness, Workflow: report.Workflow, Jobs: report.Jobs, Artifacts: report.Artifacts, Hypotheses: report.RootCauseHypotheses, NextSteps: report.RecommendedInvestigation, CandidateSteps: report.CandidateSteps}
	return report
}

func WriteTargetMarkdown(report TargetReport) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# AutoCI Research Report: %s\n\n", report.ID)
	fmt.Fprintln(&out, "## Summary")
	fmt.Fprintf(&out, "%s\n\n", summaryForTarget(report))
	fmt.Fprintln(&out, "## Current finding")
	fmt.Fprintf(&out, "- ID: `%s`\n", report.CurrentFinding.ID)
	fmt.Fprintf(&out, "- Type: `%s`\n", report.CurrentFinding.Type)
	fmt.Fprintf(&out, "- Workflow: `%s`\n", fallback(report.CurrentFinding.Workflow, "unknown"))
	fmt.Fprintf(&out, "- Jobs: %s\n", markdownListValue(report.CurrentFinding.Jobs, "unknown"))
	fmt.Fprintf(&out, "- Occurrences / impact: %s\n", occurrencesImpact(report))
	fmt.Fprintf(&out, "- Confidence: `%d`\n\n", report.CurrentFinding.Confidence)
	writeMarkdownSection(&out, "Evidence", report.Evidence)
	writeCandidateSteps(&out, report.CandidateSteps)
	writeMarkdownSection(&out, "What we know", report.WhatWeKnow)
	writeMarkdownSection(&out, "What we do not know yet", report.WhatWeDoNotKnowYet)
	fmt.Fprintln(&out, "## Root-cause hypotheses")
	if len(report.RootCauseHypotheses) == 0 {
		fmt.Fprintln(&out, "- No root-cause hypothesis is justified by the cached evidence.")
	} else {
		for i, hypothesis := range report.RootCauseHypotheses {
			fmt.Fprintf(&out, "%d. %s Confidence: `%d`.\n", i+1, hypothesis.Summary, hypothesis.Confidence)
			for _, evidence := range hypothesis.Evidence {
				fmt.Fprintf(&out, "   - Evidence: %s\n", evidence)
			}
		}
	}
	fmt.Fprintln(&out)
	writeMarkdownSection(&out, "Recommended investigation", report.RecommendedInvestigation)
	writeMarkdownSection(&out, "Candidate fixes", report.CandidateFixes)
	fmt.Fprintln(&out, "## Fix readiness")
	fmt.Fprintf(&out, "`%s`\n\n", report.Readiness)
	fmt.Fprintln(&out, "## Notes for `autoci fix`")
	encoded, _ := json.MarshalIndent(report.FixNotes, "", "  ")
	fmt.Fprintln(&out, "```json")
	fmt.Fprintln(&out, string(encoded))
	fmt.Fprintln(&out, "```")
	return out.Bytes()
}

func targetedEvidence(report TargetReport) []string {
	var evidence []string
	if report.FailureTheme != nil {
		if report.FailureTheme.Signature != "" {
			evidence = append(evidence, "Failure signature: "+report.FailureTheme.Signature)
		}
		if report.FailureTheme.Occurrences > 0 {
			evidence = append(evidence, fmt.Sprintf("Occurrences: %d", report.FailureTheme.Occurrences))
		}
	}
	for key, values := range report.Artifacts {
		for _, value := range values {
			evidence = append(evidence, fmt.Sprintf("%s: %s", key, value))
		}
	}
	for _, runID := range report.RunIDs {
		evidence = append(evidence, "run: "+runID)
	}
	for _, line := range limitStrings(report.LogExcerpts, 6) {
		evidence = append(evidence, "log: "+line)
	}
	if len(evidence) == 0 && report.ResearchOpportunity != nil && report.ResearchOpportunity.Evidence != "" {
		evidence = append(evidence, report.ResearchOpportunity.Evidence)
	}
	return uniqueSortedCopy(evidence)
}

func targetedKnownFacts(report TargetReport) []string {
	var facts []string
	if report.Workflow != "" {
		facts = append(facts, "The finding is associated with workflow "+report.Workflow+".")
	}
	if len(report.Jobs) > 0 {
		facts = append(facts, "Affected jobs: "+strings.Join(report.Jobs, ", ")+".")
	}
	if report.FailureTheme != nil && report.FailureTheme.Signature != "" {
		facts = append(facts, "AutoCI grouped the failure as "+report.FailureTheme.Signature+".")
	}
	for _, key := range artifactKeys(report.Artifacts) {
		values := report.Artifacts[key]
		if len(values) > 0 {
			facts = append(facts, fmt.Sprintf("Cached evidence includes %s: %s.", key, strings.Join(values, ", ")))
		}
	}
	if report.WorkflowContext != nil {
		facts = append(facts, "Local workflow file context was available at "+report.WorkflowContext.Path+".")
	}
	if len(report.CandidateSteps) > 0 {
		step := report.CandidateSteps[0]
		facts = append(facts, fmt.Sprintf("Best candidate workflow step is job %s command %q with confidence %.2f.", step.Job, step.Command, step.Confidence))
	}
	if len(facts) == 0 {
		facts = append(facts, "AutoCI found the ID in cached state, but the cached item contains limited structured evidence.")
	}
	return facts
}

func targetedGaps(report TargetReport) []EvidenceGap {
	var gaps []EvidenceGap
	signature := ""
	if report.FailureTheme != nil {
		signature = report.FailureTheme.Signature
	}
	switch signature {
	case "image pull failure", "image pull timeout":
		if len(report.Artifacts["images"]) == 0 {
			gaps = append(gaps, EvidenceGap{Type: "missing_image", Message: "Exact failing image reference not identified"})
		}
		if len(report.Artifacts["registries"]) == 0 && len(report.Artifacts["hosts"]) == 0 {
			gaps = append(gaps, EvidenceGap{Type: "missing_registry", Message: "Registry host could not be determined from cached evidence"})
		}
		if !anyLogContains(report.LogExcerpts, []string{"manifest unknown", "pull access denied", "toomanyrequests", "rate limit", "timeout", "connection reset", "connection refused", "no such host"}) {
			gaps = append(gaps, EvidenceGap{Type: "missing_pull_error", Message: "Exact pull error not present in cached evidence"})
		}
	case "npm install failure":
		if len(report.Artifacts["packages"]) == 0 && len(report.Artifacts["modules"]) == 0 {
			gaps = append(gaps, EvidenceGap{Type: "missing_package", Message: "Exact package or dependency constraint not identified"})
		}
		if !anyLogContains(report.LogExcerpts, []string{"yn0002", "yn0060", "yn0086", "actual:", "expected:", "lockfile would have been modified", "doesn't provide", "incorrectly met"}) {
			gaps = append(gaps, EvidenceGap{Type: "missing_root_cause", Message: "Cached logs do not include a resolver, peer dependency, lockfile, or integrity marker"})
		}
	default:
		if len(report.LogExcerpts) == 0 {
			gaps = append(gaps, EvidenceGap{Type: "missing_logs", Message: "Representative log excerpts are not present in cached evidence"})
		}
	}
	return gaps
}

func gapMessages(gaps []EvidenceGap) []string {
	if len(gaps) == 0 {
		return []string{"No major evidence gap is visible in the cached AutoCI state."}
	}
	var result []string
	for _, gap := range gaps {
		result = append(result, gap.Message)
	}
	return result
}

func targetedHypotheses(report TargetReport) []RootCauseHypothesis {
	if report.ResearchOpportunity != nil && len(report.ResearchOpportunity.Hypotheses) > 0 {
		return report.ResearchOpportunity.Hypotheses
	}
	if report.ResearchOpportunity != nil && report.ResearchOpportunity.Hypothesis != "" {
		return []RootCauseHypothesis{{Summary: report.ResearchOpportunity.Hypothesis, Confidence: report.Confidence, Evidence: report.Evidence}}
	}
	if report.FailureTheme != nil {
		opportunity := opportunityFromFailureTheme(report.Workflow, *report.FailureTheme)
		return opportunity.Hypotheses
	}
	if report.ProfileFinding != nil {
		return []RootCauseHypothesis{{Summary: report.ProfileFinding.Title, Confidence: report.Confidence, Evidence: compactStrings([]string{report.ProfileFinding.Evidence})}}
	}
	return nil
}

func targetedInvestigation(report TargetReport) []string {
	if report.ResearchOpportunity != nil && len(report.ResearchOpportunity.InvestigationSteps) > 0 {
		return report.ResearchOpportunity.InvestigationSteps
	}
	if report.FailureTheme != nil {
		opportunity := opportunityFromFailureTheme(report.Workflow, *report.FailureTheme)
		return opportunity.InvestigationSteps
	}
	if report.ProfileFinding != nil && report.ProfileFinding.Job != "" {
		return []string{
			fmt.Sprintf("Inspect workflow steps for job %s in %s and identify the command or action consuming the measured time.", report.ProfileFinding.Job, fallback(report.Workflow, "the workflow")),
			"Compare recent slow and fast runs for the same job before changing cache keys, matrix shape, or runner sizing.",
		}
	}
	return []string{"Inspect the cached source command data in evidence.json before assigning a root cause."}
}

func targetedFixes(report TargetReport) []string {
	signature := ""
	if report.FailureTheme != nil {
		signature = report.FailureTheme.Signature
	}
	switch signature {
	case "image pull failure", "image pull timeout":
		if len(report.Artifacts["images"]) == 0 {
			return []string{"No image-specific workflow change is ready yet; first capture the failing image reference and pull error."}
		}
		return []string{
			"Pin the failing image to an immutable digest if the tag is mutable.",
			"Mirror the evidenced image into a registry controlled by the project if public registry access is the confirmed failure mode.",
			"Add registry authentication only if logs show pull access denied or authorization failures.",
		}
	case "npm install failure":
		if anyLogContains(report.LogExcerpts, []string{"actual:", "expected:"}) && anyLogContains(report.LogExcerpts, []string{"snyk"}) {
			return []string{
				"Pin or update the Snyk npm package version that downloads the wrapper binary.",
				"Clear or isolate the package-manager cache used by the Snyk binary download step if checksum mismatches correlate with cached downloads.",
				"Temporarily replace the postinstall binary download path only after confirming the Snyk package is the failing package.",
			}
		}
		if hasAnyArtifact(report.Artifacts, "packages", "modules") {
			return []string{"Adjust the evidenced dependency, lockfile, or package-manager configuration after confirming the specific install error."}
		}
		return []string{"No dependency-specific change is ready yet; first capture the failing package or install diagnostic."}
	default:
		if report.ProfileFinding != nil {
			return []string{"Treat this as an optimization candidate; change workflow structure only after identifying the slow step or repeated setup work."}
		}
	}
	return []string{"No concrete fix is ready from the cached evidence alone."}
}

func targetedReadiness(report TargetReport) lifecycle.Readiness {
	if report.ProfileFinding != nil && report.FailureTheme == nil {
		return lifecycle.ReadinessNotReady
	}
	if report.FailureTheme == nil {
		return lifecycle.ReadinessNeedsMoreEvidence
	}
	signature := report.FailureTheme.Signature
	switch signature {
	case "image pull failure", "image pull timeout":
		if len(report.Artifacts["images"]) > 0 && anyLogContains(report.LogExcerpts, []string{"manifest unknown", "pull access denied", "toomanyrequests", "rate limit", "timeout", "connection reset", "connection refused", "no such host"}) {
			return lifecycle.ReadinessReadyForFix
		}
	case "npm install failure":
		if anyLogContains(report.LogExcerpts, []string{"actual:", "expected:"}) && anyLogContains(report.LogExcerpts, []string{"snyk"}) {
			return lifecycle.ReadinessReadyForFix
		}
		if len(report.CandidateSteps) > 0 && anyLogContains(report.LogExcerpts, []string{"econnreset", "etimedout", "timeout", "eai_again", "enotfound", "connection reset", "connection refused", "temporary failure", "network", "rate limit", "toomanyrequests"}) {
			return lifecycle.ReadinessReadyForFix
		}
		if hasAnyArtifact(report.Artifacts, "packages", "modules") && anyLogContains(report.LogExcerpts, []string{"yn0002", "yn0060", "yn0086", "lockfile would have been modified", "doesn't provide", "incorrectly met"}) {
			return lifecycle.ReadinessReadyForFix
		}
	}
	return lifecycle.ReadinessNeedsMoreEvidence
}

func summaryForTarget(report TargetReport) string {
	if report.FailureTheme != nil {
		switch report.FailureTheme.Signature {
		case "image pull failure", "image pull timeout":
			if len(report.Artifacts["images"]) == 0 && len(report.Artifacts["registries"]) == 0 && len(report.Artifacts["hosts"]) == 0 {
				return fmt.Sprintf("AutoCI found a recurring image/container setup failure for %s, but the cached evidence does not include the exact failed image or registry.", markdownListValue(report.Jobs, "the affected jobs"))
			}
		case "npm install failure":
			if anyLogContains(report.LogExcerpts, []string{"snyk", "actual:", "expected:"}) {
				return fmt.Sprintf("AutoCI found an npm/yarn install failure for %s with evidence pointing to Snyk binary download or integrity verification.", markdownListValue(report.Jobs, "the affected jobs"))
			}
		}
		return fmt.Sprintf("AutoCI found %s affecting %s and prepared an offline investigation brief from cached CI evidence.", report.FailureTheme.Signature, markdownListValue(report.Jobs, "the affected jobs"))
	}
	if report.ProfileFinding != nil {
		return fmt.Sprintf("AutoCI found a profile finding for %s and prepared an offline investigation brief from cached runtime data.", markdownListValue(report.Jobs, fallback(report.Workflow, "the workflow")))
	}
	if report.ResearchOpportunity != nil {
		return report.ResearchOpportunity.Hypothesis
	}
	return "AutoCI resolved the requested ID from cached state and prepared an offline investigation brief."
}

func writeMarkdownSection(out *bytes.Buffer, title string, values []string) {
	fmt.Fprintf(out, "## %s\n", title)
	if len(values) == 0 {
		fmt.Fprintln(out, "- No concrete evidence available.")
	} else {
		for _, value := range values {
			fmt.Fprintf(out, "- %s\n", value)
		}
	}
	fmt.Fprintln(out)
}

func writeCandidateSteps(out *bytes.Buffer, steps []stepresolver.CandidateStep) {
	fmt.Fprintln(out, "## Candidate workflow steps")
	if len(steps) == 0 {
		fmt.Fprintln(out, "- No workflow step matched the finding with enough confidence.")
		fmt.Fprintln(out)
		return
	}
	for _, step := range steps {
		fmt.Fprintf(out, "- `%s` `%s` line `%d` confidence `%.2f`: `%s`\n", step.Job, fallback(step.Name, step.Source), step.Line, step.Confidence, step.Command)
		for _, why := range step.Why {
			fmt.Fprintf(out, "  - Why: %s\n", why)
		}
	}
	fmt.Fprintln(out)
}

func occurrencesImpact(report TargetReport) string {
	if report.CurrentFinding.Occurrences > 0 {
		return fmt.Sprintf("`%d` occurrences", report.CurrentFinding.Occurrences)
	}
	if report.CurrentFinding.Impact != "" {
		return report.CurrentFinding.Impact
	}
	return "unknown"
}

func markdownListValue(values []string, fallbackValue string) string {
	if len(values) == 0 {
		return fallbackValue
	}
	return "`" + strings.Join(values, "`, `") + "`"
}

func snapshotData(snapshot state.Snapshot, target any) bool {
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

func (acc *targetAccumulator) setWorkflow(values ...string) {
	if acc.workflow != "" {
		return
	}
	for _, value := range values {
		if value != "" {
			acc.workflow = value
			return
		}
	}
}

func (acc *targetAccumulator) addSource(record state.SnapshotRecord, item any) {
	data, err := json.Marshal(item)
	if err != nil {
		return
	}
	acc.sources = append(acc.sources, SourceCommandData{Command: record.Command, Workflow: record.Workflow, GeneratedAt: record.Snapshot.GeneratedAt, Item: json.RawMessage(data)})
}

func idsEquivalent(stored, requested string) bool {
	return stored == requested || normalizeTargetID(stored) == normalizeTargetID(requested)
}

func normalizeTargetID(id string) string {
	id = strings.TrimPrefix(id, "research-")
	id = strings.TrimPrefix(id, "reliability-")
	id = strings.TrimPrefix(id, "performance-")
	id = strings.TrimPrefix(id, "bottleneck-")
	id = strings.TrimPrefix(id, "failure-theme-")
	id = strings.TrimPrefix(id, "failure-")
	return id
}

func stableTargetID(requested string, theme *failures.FailureTheme, finding *profile.Finding, opportunity *ResearchOpportunity) string {
	if theme != nil && theme.ID == requested {
		return theme.ID
	}
	if finding != nil && finding.ID == requested {
		return finding.ID
	}
	if opportunity != nil && opportunity.ID == requested {
		return opportunity.ID
	}
	if theme != nil && theme.ID != "" {
		return theme.ID
	}
	if finding != nil && finding.ID != "" {
		return finding.ID
	}
	if opportunity != nil && opportunity.ID != "" {
		return opportunity.ID
	}
	return requested
}

func workflowMatches(stored, requested string) bool {
	if requested == "" {
		return true
	}
	return normalizeWorkflowName(stored) == normalizeWorkflowName(requested)
}

func normalizeWorkflowName(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, filepath.Ext(value))
	return strings.TrimPrefix(value, ".depot/workflows/")
}

func workflowContext(repoPath, workflow string) *WorkflowContext {
	if workflow == "" {
		return nil
	}
	path := filepath.Join(repoPath, ".depot", "workflows", filepath.FromSlash(workflow))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return &WorkflowContext{Path: path, Content: string(data)}
}

func candidateStepsForReport(repoPath string, report TargetReport) []stepresolver.CandidateStep {
	if report.Workflow == "" {
		return nil
	}
	workflows, err := scanner.Scan(repoPath)
	if err != nil {
		return nil
	}
	for _, item := range workflows {
		name := scanner.WorkflowName(repoPath, item)
		if !workflowMatches(name, report.Workflow) {
			continue
		}
		steps := stepresolver.Inventory(repoPath, name, item)
		return stepresolver.CandidateSteps(steps, stepresolver.FindingContext{
			ID:          report.ID,
			Signature:   findingSignature(report),
			Jobs:        report.Jobs,
			Artifacts:   report.Artifacts,
			LogExcerpts: report.LogExcerpts,
		})
	}
	return nil
}

func findingSignature(report TargetReport) string {
	if report.FailureTheme != nil {
		return report.FailureTheme.Signature
	}
	if report.ResearchOpportunity != nil {
		return report.ResearchOpportunity.Hypothesis
	}
	if report.ProfileFinding != nil {
		return report.ProfileFinding.Title
	}
	return report.ID
}

func failureLogExcerpts(items []failures.FailureEvidence) []string {
	var result []string
	for _, item := range items {
		result = append(result, item.LogExcerpt, item.PullError, item.InstallError, item.DependencyConflict, item.CompilerError, item.AssertionMessage)
	}
	return uniqueSortedCopy(compactStrings(result))
}

func failureRunIDs(items []failures.FailureEvidence) []string {
	var result []string
	for _, item := range items {
		result = append(result, item.RunID)
	}
	return uniqueSortedCopy(compactStrings(result))
}

func failureArtifacts(theme *failures.FailureTheme) map[string][]string {
	result := map[string][]string{}
	addArtifactValues(result, "images", theme.Artifacts.Images...)
	addArtifactValues(result, "packages", theme.Artifacts.Packages...)
	addArtifactValues(result, "modules", theme.Artifacts.Modules...)
	addArtifactValues(result, "urls", theme.Artifacts.URLs...)
	addArtifactValues(result, "hosts", theme.Artifacts.Hosts...)
	addArtifactValues(result, "dockerfiles", theme.Artifacts.Dockerfiles...)
	for _, evidence := range theme.Evidence {
		addArtifactValues(result, "images", evidence.Image)
		addArtifactValues(result, "packages", evidence.PackageName)
		addArtifactValues(result, "urls", evidence.RegistryURL)
		addArtifactValues(result, "hosts", fallback(evidence.RegistryHost, evidence.Registry))
	}
	return compactArtifactMap(result)
}

func rawArtifacts(raw RawEvidence) map[string][]string {
	result := map[string][]string{}
	addArtifactValues(result, "images", raw.Images...)
	addArtifactValues(result, "registries", raw.Registries...)
	addArtifactValues(result, "actions", raw.Actions...)
	addArtifactValues(result, "modules", raw.Modules...)
	addArtifactValues(result, "urls", raw.URLs...)
	addArtifactValues(result, "hosts", raw.Hosts...)
	return compactArtifactMap(result)
}

func addArtifactValues(target map[string][]string, key string, values ...string) {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			target[key] = append(target[key], value)
		}
	}
}

func mergeArtifactsMap(base, extra map[string][]string) map[string][]string {
	if base == nil {
		base = map[string][]string{}
	}
	for key, values := range extra {
		base[key] = append(base[key], values...)
	}
	return compactArtifactMap(base)
}

func compactArtifactMap(values map[string][]string) map[string][]string {
	for key, items := range values {
		values[key] = uniqueSortedCopy(compactStrings(items))
		if len(values[key]) == 0 {
			delete(values, key)
		}
	}
	return values
}

func artifactKeys(values map[string][]string) []string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasAnyArtifact(values map[string][]string, keys ...string) bool {
	for _, key := range keys {
		if len(values[key]) > 0 {
			return true
		}
	}
	return false
}

func anyLogContains(lines, tokens []string) bool {
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, token := range tokens {
			if strings.Contains(lower, strings.ToLower(token)) {
				return true
			}
		}
	}
	return false
}

func confidenceFromHypotheses(hypotheses []RootCauseHypothesis) int {
	best := 0
	for _, hypothesis := range hypotheses {
		best = maxInt(best, hypothesis.Confidence)
	}
	return best
}

func confidenceFromSeverity(severity string) int {
	switch strings.ToLower(severity) {
	case "high", "critical":
		return 75
	case "medium":
		return 60
	case "low":
		return 45
	default:
		return 50
	}
}

func fallback(value, fallbackValue string) string {
	if value != "" {
		return value
	}
	return fallbackValue
}

func uniqueSortedCopy(values []string) []string {
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
	sort.Strings(result)
	return result
}
