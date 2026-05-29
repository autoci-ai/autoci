package fix

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autoci-ai/autoci/internal/lifecycle"
	"github.com/autoci-ai/autoci/internal/scanner"
	stepresolver "github.com/autoci-ai/autoci/internal/workflow"
	"gopkg.in/yaml.v3"
)

type Plan struct {
	ID              string              `json:"id"`
	SourceID        string              `json:"sourceId"`
	Branch          string              `json:"branch,omitempty"`
	Workflow        string              `json:"workflow"`
	Readiness       lifecycle.Readiness `json:"readiness,omitempty"`
	FixType         FixType             `json:"fixType"`
	AffectedJobs    []string            `json:"affectedJobs,omitempty"`
	Hypothesis      string              `json:"hypothesis"`
	Evidence        string              `json:"evidence"`
	ChangeSummary   string              `json:"changeSummary"`
	SuccessCriteria string              `json:"successCriteria"`
	Confidence      string              `json:"confidence"`
	Reason          string              `json:"reason,omitempty"`
	PatchGenerated  bool                `json:"patchGenerated"`
	PatchApplied    bool                `json:"patchApplied"`
	Targets         []Target            `json:"targets,omitempty"`
	PatchScope      PatchScope          `json:"patchScope"`
	Diff            string              `json:"diff,omitempty"`
	Validation      []string            `json:"validation"`
	FilesChanged    []string            `json:"filesChanged,omitempty"`
	Gaps            []EvidenceGap       `json:"gaps,omitempty"`
	patchedContent  string
}

type FixType string

const (
	RootCauseFix       FixType = "root_cause"
	InstrumentationFix FixType = "instrumentation"
)

type Target struct {
	Workflow    string   `json:"workflow"`
	Job         string   `json:"job,omitempty"`
	Step        string   `json:"step,omitempty"`
	Command     string   `json:"command,omitempty"`
	Image       string   `json:"image,omitempty"`
	Line        int      `json:"line,omitempty"`
	DerivedFrom []string `json:"derivedFrom,omitempty"`
}

type PatchScope struct {
	FilesChanged          int      `json:"filesChanged"`
	JobsTouched           []string `json:"jobsTouched"`
	StepsTouched          []string `json:"stepsTouched"`
	UnrelatedLinesChanged int      `json:"unrelatedLinesChanged"`
}

type Options struct {
	RepoPath       string
	Workflow       scanner.Workflow
	WorkflowName   string
	Opportunity    string
	DryRun         bool
	Evidence       string
	TargetJobs     []string
	Occurrences    int
	Signature      string
	Artifacts      map[string][]string
	CandidateSteps []stepresolver.CandidateStep
	Hypotheses     []Hypothesis
	LogExcerpts    []string
	Readiness      lifecycle.Readiness
	Gaps           []EvidenceGap
}

type EvidenceGap struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type Hypothesis struct {
	Summary    string
	Confidence int
	Evidence   []string
}

type Record struct {
	ID              string              `json:"id"`
	SourceItemID    string              `json:"sourceItemId"`
	Workflow        string              `json:"workflow"`
	Branch          string              `json:"branch,omitempty"`
	Readiness       lifecycle.Readiness `json:"readiness,omitempty"`
	FixType         FixType             `json:"fixType"`
	AffectedJobs    []string            `json:"affectedJobs,omitempty"`
	Hypothesis      string              `json:"hypothesis"`
	Evidence        string              `json:"evidence"`
	ChangeSummary   string              `json:"changeSummary"`
	SuccessCriteria string              `json:"successCriteria"`
	Confidence      string              `json:"confidence"`
	Reason          string              `json:"reason,omitempty"`
	PatchGenerated  bool                `json:"patchGenerated"`
	PatchApplied    bool                `json:"patchApplied"`
	Targets         []Target            `json:"targets,omitempty"`
	PatchScope      PatchScope          `json:"patchScope"`
	FilesChanged    []string            `json:"filesChanged"`
	Gaps            []EvidenceGap       `json:"gaps,omitempty"`
	DryRun          bool                `json:"dryRun"`
}

type workflowInspection struct {
	Commands []commandTarget
	Images   []imageTarget
	Steps    []jobStepsTarget
}

type commandTarget struct {
	Target
	LineText   string
	Confidence float64
	Why        []string
}

type imageTarget struct {
	Target
	LineText string
}

type jobStepsTarget struct {
	Workflow    string
	Job         string
	Line        int
	LineText    string
	StepIndent  string
	DerivedFrom []string
}

func Generate(options Options) (Plan, error) {
	sourceID := strings.TrimSpace(options.Opportunity)
	id := normalizeOpportunity(sourceID)
	if id == "" {
		id = "failure-theme-image-pull-failure"
	}
	plan := basePlan(id, sourceID, options.WorkflowName, options.Evidence)
	plan.Readiness = options.Readiness
	plan.Gaps = options.Gaps
	plan.AffectedJobs = uniqueStringsPreserveOrder(options.TargetJobs)

	original, err := os.ReadFile(options.Workflow.Path)
	if err != nil {
		return Plan{}, err
	}
	inspection, err := inspectWorkflow(options.WorkflowName, original)
	if err != nil {
		plan.Confidence = "low"
		plan.Reason = "AutoCI could not parse the workflow well enough to prove a target-safe edit."
		return plan, nil
	}
	if commands := commandTargetsFromInventory(options.RepoPath, options.WorkflowName, options.Workflow, original); len(commands) > 0 {
		inspection.Commands = commands
	}

	if buildInstrumentationPatch(&plan, original, inspection, options) {
		if options.DryRun {
			return normalizePlan(plan), nil
		}
		if err := createBranch(options.RepoPath, plan.Branch); err != nil {
			return Plan{}, err
		}
		if err := os.WriteFile(options.Workflow.Path, []byte(applyLineDiff(string(original), plan)), 0o644); err != nil {
			return Plan{}, err
		}
		plan.PatchApplied = true
		return normalizePlan(plan), nil
	}
	if options.Readiness == lifecycle.ReadinessNeedsMoreEvidence && plan.FixType == InstrumentationFix && plan.Reason != "" {
		plan.Validation = diagnosticNextSteps(plan.Workflow)
		return normalizePlan(plan), nil
	}

	switch {
	case isImagePull(id):
		planImagePull(&plan, inspection, options)
	case isDependencyInstall(id):
		buildDependencyInstallPatch(&plan, original, inspection, options)
	case isLintInstability(id):
		buildLintPatch(&plan, original, inspection, options)
	default:
		plan.Confidence = "low"
		plan.Reason = "No surgical fix generator is available for the selected opportunity."
	}

	if !plan.PatchGenerated {
		plan.Validation = diagnosticNextSteps(plan.Workflow)
		return normalizePlan(plan), nil
	}
	if plan.Confidence == "low" {
		plan.PatchGenerated = false
		plan.Diff = ""
		plan.FilesChanged = nil
		plan.PatchScope.FilesChanged = 0
		plan.Reason = "Low-confidence fixes produce plans only. AutoCI needs a more exact target before modifying the workflow."
		return normalizePlan(plan), nil
	}
	if options.DryRun {
		return normalizePlan(plan), nil
	}
	if err := createBranch(options.RepoPath, plan.Branch); err != nil {
		return Plan{}, err
	}
	if err := os.WriteFile(options.Workflow.Path, []byte(applyLineDiff(string(original), plan)), 0o644); err != nil {
		return Plan{}, err
	}
	plan.PatchApplied = true
	return normalizePlan(plan), nil
}

func normalizePlan(plan Plan) Plan {
	plan.PatchScope = normalizePatchScope(plan.PatchScope)
	return plan
}

func normalizePatchScope(scope PatchScope) PatchScope {
	scope.JobsTouched = emptyStrings(scope.JobsTouched)
	scope.StepsTouched = emptyStrings(scope.StepsTouched)
	return scope
}

func NewRecord(plan Plan, dryRun bool) Record {
	plan = normalizePlan(plan)
	return Record{
		ID:              "fix-" + trimFixPrefix(plan.ID),
		SourceItemID:    plan.SourceID,
		Workflow:        plan.Workflow,
		Branch:          plan.Branch,
		Readiness:       plan.Readiness,
		FixType:         plan.FixType,
		AffectedJobs:    emptyStrings(plan.AffectedJobs),
		Hypothesis:      plan.Hypothesis,
		Evidence:        plan.Evidence,
		ChangeSummary:   plan.ChangeSummary,
		SuccessCriteria: plan.SuccessCriteria,
		Confidence:      plan.Confidence,
		Reason:          plan.Reason,
		PatchGenerated:  plan.PatchGenerated,
		PatchApplied:    plan.PatchApplied,
		Targets:         emptyTargets(plan.Targets),
		PatchScope:      plan.PatchScope,
		FilesChanged:    emptyStrings(plan.FilesChanged),
		Gaps:            plan.Gaps,
		DryRun:          dryRun,
	}
}

func emptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func emptyTargets(values []Target) []Target {
	if values == nil {
		return []Target{}
	}
	return values
}

func uniqueStringsPreserveOrder(values []string) []string {
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

func basePlan(id, sourceID, workflow, evidence string) Plan {
	if sourceID == "" {
		sourceID = id
	}
	plan := Plan{
		ID:         "fix-" + trimFixPrefix(id),
		SourceID:   sourceID,
		Branch:     "autoci/fix-" + trimFixPrefix(id),
		Workflow:   workflow,
		FixType:    RootCauseFix,
		Evidence:   evidence,
		Confidence: "low",
		PatchScope: PatchScope{JobsTouched: []string{}, StepsTouched: []string{}},
		Validation: []string{"autoci validate --allow-depot-run"},
	}
	switch {
	case isImagePull(id):
		plan.Hypothesis = "Image pull failures are caused by registry latency, network instability, mutable image tags, or missing image pinning."
		plan.ChangeSummary = "No workflow edit has been selected yet."
		plan.SuccessCriteria = "Image pull failures no longer recur in future workflow runs."
	case isDependencyInstall(id):
		plan.Hypothesis = "Dependency install failures are caused by transient package registry, lockfile, or cache behavior in the affected job."
		plan.ChangeSummary = "Patch the affected package-manager install command only when AutoCI can prove the target job and command."
		plan.SuccessCriteria = "Dependency install failures no longer recur without increasing workflow failure rate."
	case isLintInstability(id):
		plan.Hypothesis = "Lint instability is caused by transient execution or dependency setup failures in the affected lint job."
		plan.ChangeSummary = "Patch the affected lint command only when AutoCI can prove the target job and command."
		plan.SuccessCriteria = "The lint job failure rate falls below 2% or the root cause is identified."
	default:
		plan.Hypothesis = "The selected opportunity points to a measurable CI improvement."
		plan.ChangeSummary = "No safe surgical patch has been selected."
		plan.SuccessCriteria = "The selected issue is explained by evidence or a targeted patch is generated later."
	}
	if plan.Evidence == "" {
		plan.Evidence = "Selected opportunity: " + sourceID
	}
	return plan
}

func planImagePull(plan *Plan, inspection workflowInspection, options Options) {
	evidenceImages := options.Artifacts["images"]
	if len(evidenceImages) > 0 {
		for _, image := range evidenceImages {
			target := Target{Workflow: options.WorkflowName, Image: image, Step: image}
			if match, ok := findImageTarget(inspection.Images, image, options.TargetJobs); ok {
				target = match.Target
				plan.Confidence = "high"
			}
			plan.Targets = append(plan.Targets, target)
		}
		plan.Reason = "Failure logs explicitly referenced image artifacts. AutoCI will not patch image references until it can resolve a safe immutable replacement."
		plan.ChangeSummary = "Patch not generated. Evidence-backed image references should be pinned manually to immutable digests or known versions."
		return
	}
	images := filterImagesByJobs(inspection.Images, options.TargetJobs)
	for _, image := range images {
		plan.Targets = append(plan.Targets, image.Target)
	}
	plan.Confidence = "low"
	if len(images) == 0 {
		plan.Reason = "AutoCI detected image pull failures but could not identify exact image references in the selected workflow."
		return
	}
	plan.Reason = "AutoCI detected image pull failures but cannot safely resolve or pin the exact image references automatically."
	plan.ChangeSummary = "Patch not generated. Candidate image references should be pinned manually to immutable digests or known versions."
}

func findImageTarget(images []imageTarget, image string, jobs []string) (imageTarget, bool) {
	candidates := filterImagesByJobs(images, jobs)
	for _, candidate := range candidates {
		if candidate.Image == image {
			return candidate, true
		}
	}
	return imageTarget{}, false
}

func buildDependencyInstallPatch(plan *Plan, original []byte, inspection workflowInspection, options Options) {
	if options.Occurrences == 1 {
		refuseSingleOccurrence(plan)
		return
	}
	if len(options.TargetJobs) == 0 {
		plan.Reason = "The selected opportunity did not identify a target job, so AutoCI cannot prove which install command should change."
		return
	}
	candidates := candidateCommandsFromResearch(options.CandidateSteps, options.WorkflowName, original)
	if len(candidates) == 0 {
		candidates = filterCommandsByJobs(inspection.Commands, options.TargetJobs)
		candidates = filterCommands(candidates, isDependencyInstallCommand)
	}
	for _, candidate := range candidates {
		plan.Targets = append(plan.Targets, candidate.Target)
	}
	if len(candidates) == 0 {
		plan.Reason = "AutoCI could not find a dependency install command in the targeted job."
		return
	}
	if len(candidates) > 1 {
		plan.Reason = "Multiple dependency install commands matched the selected opportunity; refusing to guess which one caused the failure."
		return
	}
	candidate := candidates[0]
	if !signatureMatchesPackageManager(options.Signature, candidate.Command) {
		plan.Reason = fmt.Sprintf("Failure signature %q does not match targeted command %q.", options.Signature, candidate.Command)
		return
	}
	if isSnykIntegrityContext(options) {
		plan.Confidence = "low"
		plan.Targets = []Target{candidate.Target}
		plan.Hypothesis = primaryResearchHypothesis(options, "Snyk package install is failing during binary download or integrity verification.")
		plan.ChangeSummary = "No safe patch generated. Evidence points to Snyk binary download or checksum verification, but a generic yarn install retry is not a proven mitigation."
		plan.Reason = "Evidence suggests a Snyk binary download problem, but AutoCI cannot determine whether the root cause is network instability, cache corruption, upstream Snyk availability, or a checksum verification bug. AutoCI will not generate a generic retry around the entire dependency install step."
		return
	}
	if !supportsInstallRetry(options) {
		plan.Confidence = "low"
		plan.Targets = []Target{candidate.Target}
		plan.Hypothesis = primaryResearchHypothesis(options, plan.Hypothesis)
		plan.ChangeSummary = "No safe patch generated. The selected workflow step is plausible, but cached research does not show that retrying the install command addresses the root cause."
		plan.Reason = "AutoCI found a dependency install step, but the research evidence does not contain transient network, registry, timeout, or retryable download markers. Prefer a false negative over a generic retry patch."
		return
	}
	updated := retryCommand(candidate.Command)
	if updated == candidate.Command {
		plan.Reason = "The targeted command already appears to be wrapped or cannot be safely rewritten."
		return
	}
	plan.Confidence = "medium"
	if candidate.Confidence >= 0.90 {
		plan.Confidence = "high"
	}
	plan.Reason = selectionReason(candidate)
	plan.ChangeSummary = fmt.Sprintf("Target step: %s `%s`. Proposed change: retry the dependency install command up to 3 times with exponential backoff.", candidate.Job, candidate.Command)
	setSurgicalPatch(plan, original, candidate.Line, candidate.LineText, updated, candidate.Target)
}

func buildLintPatch(plan *Plan, original []byte, inspection workflowInspection, options Options) {
	if options.Occurrences == 1 {
		refuseSingleOccurrence(plan)
		return
	}
	if len(options.TargetJobs) == 0 {
		plan.Reason = "The selected opportunity did not identify a target job, so AutoCI cannot prove which lint command should change."
		return
	}
	candidates := filterCommandsByJobs(inspection.Commands, options.TargetJobs)
	candidates = filterCommands(candidates, func(command string) bool {
		return strings.Contains(command, "golangci-lint")
	})
	for _, candidate := range candidates {
		plan.Targets = append(plan.Targets, candidate.Target)
	}
	if len(candidates) != 1 {
		plan.Reason = "AutoCI could not identify exactly one lint command in the targeted job."
		return
	}
	candidate := candidates[0]
	updated := retryCommand(candidate.Command)
	plan.Confidence = "medium"
	plan.ChangeSummary = fmt.Sprintf("Wrap only `%s` in the `%s` job with bounded retry logic.", candidate.Command, candidate.Job)
	setSurgicalPatch(plan, original, candidate.Line, candidate.LineText, updated, candidate.Target)
}

func refuseSingleOccurrence(plan *Plan) {
	plan.Confidence = "low"
	plan.Reason = "This failure theme has only 1 occurrence. AutoCI needs more evidence before modifying the workflow."
	plan.Validation = diagnosticNextSteps(plan.Workflow)
}

func diagnosticNextSteps(workflow string) []string {
	return []string{fmt.Sprintf("autoci failures --workflow %s --verbose", workflow)}
}

func buildInstrumentationPatch(plan *Plan, original []byte, inspection workflowInspection, options Options) bool {
	if options.Readiness != lifecycle.ReadinessNeedsMoreEvidence || len(options.Gaps) == 0 {
		return false
	}
	if isImagePull(plan.SourceID) || isImagePull(plan.ID) || strings.Contains(strings.ToLower(options.Signature), "image pull") {
		return buildImagePullInstrumentationPatch(plan, original, inspection, options)
	}
	return false
}

func buildImagePullInstrumentationPatch(plan *Plan, original []byte, inspection workflowInspection, options Options) bool {
	sections := filterStepsByJobs(inspection.Steps, options.TargetJobs)
	if len(options.TargetJobs) == 0 || len(sections) == 0 {
		plan.FixType = InstrumentationFix
		plan.Confidence = "low"
		plan.Reason = "Readiness is needs_more_evidence, but AutoCI could not find affected workflow jobs where image pull diagnostics can be inserted."
		return false
	}
	if strings.Contains(string(original), "AutoCI capture container diagnostics") {
		plan.FixType = InstrumentationFix
		plan.Confidence = "low"
		plan.Reason = "The selected workflow already contains the AutoCI container diagnostics step."
		return false
	}
	replacements := map[int]string{}
	var targets []Target
	var jobs []string
	for _, section := range sections {
		replacement := instrumentationStepsAppend(section.LineText, section.StepIndent, imagePullInstrumentationCommands(options.Gaps))
		if replacement == "" {
			continue
		}
		replacements[section.Line] = replacement
		jobs = append(jobs, section.Job)
		targets = append(targets, Target{
			Workflow:    section.Workflow,
			Job:         section.Job,
			Step:        "AutoCI capture container diagnostics",
			Command:     "docker version; docker info; docker images; docker ps -a; docker events",
			Line:        section.Line,
			DerivedFrom: section.DerivedFrom,
		})
	}
	if len(replacements) == 0 {
		plan.FixType = InstrumentationFix
		plan.Confidence = "low"
		plan.Reason = "AutoCI found affected jobs but could not produce a targeted diagnostics insertion."
		return false
	}
	patched := applyLineReplacements(string(original), replacements)
	plan.FixType = InstrumentationFix
	plan.Confidence = "high"
	plan.Hypothesis = "The cached evidence is missing the concrete image pull details required for a root-cause fix."
	plan.ChangeSummary = "Cannot safely generate a root-cause fix. Proposed instrumentation patch: capture image references, registry/Docker environment details, container state, and recent Docker events in the affected job."
	plan.SuccessCriteria = "The next failed run includes the exact image reference, registry host, pull error, and Docker/Testcontainers diagnostics needed to determine the root cause."
	plan.Reason = "Readiness is needs_more_evidence; generated instrumentation from gaps: " + gapSummary(options.Gaps) + "."
	plan.PatchGenerated = true
	plan.Targets = targets
	plan.FilesChanged = []string{plan.Workflow}
	plan.PatchScope = PatchScope{
		FilesChanged:          1,
		JobsTouched:           uniqueStringsPreserveOrder(jobs),
		StepsTouched:          []string{"AutoCI capture container diagnostics"},
		UnrelatedLinesChanged: 0,
	}
	plan.Diff = multiLineDiff(plan.Workflow, string(original), replacements)
	plan.patchedContent = patched
	plan.Validation = diagnosticNextSteps(plan.Workflow)
	return true
}

func imagePullInstrumentationCommands(gaps []EvidenceGap) []string {
	commands := []string{
		`echo "::group::AutoCI container diagnostics"`,
		"docker version || true",
		"docker info || true",
	}
	for _, gap := range gaps {
		switch gap.Type {
		case "missing_image":
			commands = append(commands, "docker images || true")
		case "missing_registry", "missing_pull_error":
			commands = append(commands, "docker ps -a || true")
			commands = append(commands, "docker events --since 30m || true")
		}
	}
	commands = append(commands,
		`env | sort | grep -E '^(DOCKER|TESTCONTAINERS|CI)_' || true`,
		`echo "::endgroup::"`,
	)
	return uniqueStringsPreserveOrder(commands)
}

func instrumentationStepsAppend(anchorLine, stepIndent string, commands []string) string {
	if strings.TrimSpace(anchorLine) == "" || stepIndent == "" || len(commands) == 0 {
		return ""
	}
	bodyIndent := stepIndent + "  "
	lines := []string{anchorLine}
	if strings.TrimSpace(anchorLine) == "steps: []" {
		lines[0] = strings.Replace(anchorLine, "steps: []", "steps:", 1)
	}
	lines = append(lines, stepIndent+"- name: AutoCI capture container diagnostics")
	lines = append(lines, bodyIndent+"if: failure()")
	lines = append(lines, bodyIndent+"run: |")
	for _, command := range commands {
		lines = append(lines, bodyIndent+"  "+command)
	}
	return strings.Join(lines, "\n")
}

func stepsAppendLine(lines []string, stepsLine int) int {
	if stepsLine <= 0 || stepsLine > len(lines) {
		return stepsLine
	}
	stepsIndent := indentWidth(lines[stepsLine-1])
	appendLine := stepsLine
	for line := stepsLine + 1; line <= len(lines); line++ {
		text := lines[line-1]
		if strings.TrimSpace(text) == "" {
			continue
		}
		if indentWidth(text) <= stepsIndent {
			break
		}
		appendLine = line
	}
	return appendLine
}

func gapSummary(gaps []EvidenceGap) string {
	var values []string
	for _, gap := range gaps {
		if gap.Type != "" {
			values = append(values, gap.Type)
		}
	}
	if len(values) == 0 {
		return "unspecified evidence gaps"
	}
	return strings.Join(values, ", ")
}

func setSurgicalPatch(plan *Plan, original []byte, line int, oldLine, newLine string, target Target) {
	if line <= 0 || oldLine == "" || oldLine == newLine {
		plan.Reason = "AutoCI could not produce a targeted edit without rewriting unrelated workflow content."
		return
	}
	plan.Targets = []Target{target}
	plan.PatchGenerated = true
	plan.FilesChanged = []string{plan.Workflow}
	plan.PatchScope = PatchScope{
		FilesChanged:          1,
		JobsTouched:           []string{target.Job},
		StepsTouched:          []string{target.Command},
		UnrelatedLinesChanged: 0,
	}
	plan.Diff = lineDiff(plan.Workflow, string(original), line, oldLine, yamlRunReplacement(oldLine, newLine))
}

func yamlRunReplacement(oldLine, command string) string {
	key := "run:"
	index := strings.Index(oldLine, key)
	if index < 0 || !strings.Contains(command, "\n") {
		return strings.Replace(oldLine, strings.TrimSpace(valueAfterYAMLKey(oldLine)), command, 1)
	}
	indent := oldLine[:index]
	bodyIndent := indent + "  "
	var lines []string
	lines = append(lines, indent+key+" |")
	for _, line := range strings.Split(command, "\n") {
		lines = append(lines, bodyIndent+line)
	}
	return strings.Join(lines, "\n")
}

func valueAfterYAMLKey(line string) string {
	if index := strings.Index(line, ":"); index >= 0 {
		return strings.TrimSpace(line[index+1:])
	}
	return strings.TrimSpace(line)
}

func inspectWorkflow(workflowName string, input []byte) (workflowInspection, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(input, &root); err != nil {
		return workflowInspection{}, err
	}
	if len(root.Content) == 0 {
		return workflowInspection{}, fmt.Errorf("workflow is empty")
	}
	lines := strings.Split(string(input), "\n")
	doc := root.Content[0]
	jobs := mappingValue(doc, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return workflowInspection{}, nil
	}
	var inspection workflowInspection
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		job := jobs.Content[i].Value
		body := jobs.Content[i+1]
		steps, stepsLine := mappingValueWithKeyLine(body, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		appendLine := stepsAppendLine(lines, stepsLine)
		inspection.Steps = append(inspection.Steps, jobStepsTarget{
			Workflow:   workflowName,
			Job:        job,
			Line:       appendLine,
			LineText:   lineAt(lines, appendLine),
			StepIndent: leadingWhitespace(lineAt(lines, stepsLine)) + "  ",
		})
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode {
				continue
			}
			if run := mappingValue(step, "run"); run != nil && run.Kind == yaml.ScalarNode {
				command := strings.TrimSpace(run.Value)
				if isSingleLine(command) {
					lineText := lineAt(lines, run.Line)
					inspection.Commands = append(inspection.Commands, commandTarget{
						Target:   Target{Workflow: workflowName, Job: job, Step: command, Command: command, Line: run.Line},
						LineText: lineText,
					})
				}
			}
			for _, key := range []string{"uses", "image"} {
				if value := mappingValue(step, key); value != nil && value.Kind == yaml.ScalarNode {
					ref := strings.TrimSpace(value.Value)
					if looksLikeImageReference(ref) {
						inspection.Images = append(inspection.Images, imageTarget{
							Target:   Target{Workflow: workflowName, Job: job, Step: ref, Image: ref, Line: value.Line},
							LineText: lineAt(lines, value.Line),
						})
					}
				}
			}
		}
	}
	return inspection, nil
}

func commandTargetsFromInventory(repoPath, workflowName string, workflow scanner.Workflow, original []byte) []commandTarget {
	lines := strings.Split(string(original), "\n")
	var result []commandTarget
	for _, step := range stepresolver.Inventory(repoPath, workflowName, workflow) {
		if step.Command == "" || !isSingleLine(step.Command) {
			continue
		}
		result = append(result, commandTarget{
			Target: Target{
				Workflow: step.Workflow,
				Job:      step.Job,
				Step:     step.Command,
				Command:  step.Command,
				Line:     step.Line,
			},
			LineText: lineAt(lines, step.Line),
		})
	}
	return result
}

func candidateCommandsFromResearch(steps []stepresolver.CandidateStep, workflowName string, original []byte) []commandTarget {
	lines := strings.Split(string(original), "\n")
	var result []commandTarget
	for _, step := range steps {
		if step.Workflow != "" && workflowName != "" && step.Workflow != workflowName {
			continue
		}
		if step.Confidence < 0.65 || !stepresolver.IsDependencyInstallCommand(step.Command) || !isSingleLine(step.Command) {
			continue
		}
		result = append(result, commandTarget{
			Target: Target{
				Workflow: workflowName,
				Job:      step.Job,
				Step:     step.Command,
				Command:  step.Command,
				Line:     step.Line,
			},
			LineText:   lineAt(lines, step.Line),
			Confidence: step.Confidence,
			Why:        step.Why,
		})
	}
	return result
}

func applyLineDiff(original string, plan Plan) string {
	if plan.patchedContent != "" {
		return plan.patchedContent
	}
	if !plan.PatchGenerated || len(plan.Targets) == 0 {
		return original
	}
	lines := strings.SplitAfter(original, "\n")
	targetLine := plan.Targets[0].Line
	if targetLine <= 0 || targetLine > len(lines) {
		return original
	}
	var added []string
	for _, line := range strings.Split(plan.Diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	if len(added) > 0 {
		lines[targetLine-1] = strings.Join(added, "\n")
		if !strings.HasSuffix(lines[targetLine-1], "\n") {
			lines[targetLine-1] += "\n"
		}
	}
	return strings.Join(lines, "")
}

func applyLineReplacements(original string, replacements map[int]string) string {
	if len(replacements) == 0 {
		return original
	}
	lines := strings.SplitAfter(original, "\n")
	for line, replacement := range replacements {
		if line <= 0 || line > len(lines) {
			continue
		}
		if !strings.HasSuffix(replacement, "\n") {
			replacement += "\n"
		}
		lines[line-1] = replacement
	}
	return strings.Join(lines, "")
}

func multiLineDiff(path, original string, replacements map[int]string) string {
	lines := strings.Split(original, "\n")
	var keys []int
	for line := range replacements {
		if line > 0 && line <= len(lines) {
			keys = append(keys, line)
		}
	}
	sort.Ints(keys)
	var builder strings.Builder
	rel := filepath.ToSlash(path)
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", rel, rel)
	for _, line := range keys {
		start := line - 1
		if start < 1 {
			start = 1
		}
		end := line
		oldCount := end - start + 1
		newCount := oldCount
		replacementLines := strings.Split(replacements[line], "\n")
		insertAfter := len(replacementLines) > 0 && replacementLines[0] == lines[line-1]
		if insertAfter {
			newCount += len(replacementLines) - 1
		}
		fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", start, oldCount, start, newCount)
		for i := start; i <= end; i++ {
			if i == line {
				if insertAfter {
					fmt.Fprintf(&builder, " %s\n", lines[i-1])
					for _, added := range replacementLines[1:] {
						fmt.Fprintf(&builder, "+%s\n", added)
					}
					continue
				}
				fmt.Fprintf(&builder, "-%s\n", lines[i-1])
				for _, added := range replacementLines {
					fmt.Fprintf(&builder, "+%s\n", added)
				}
				continue
			}
			fmt.Fprintf(&builder, " %s\n", lines[i-1])
		}
	}
	return builder.String()
}

func filterCommandsByJobs(commands []commandTarget, jobs []string) []commandTarget {
	if len(jobs) == 0 {
		return commands
	}
	allowed := workflowJobDerivations(jobs)
	var result []commandTarget
	for _, command := range commands {
		if len(allowed[command.Job]) > 0 {
			result = append(result, command)
		}
	}
	return result
}

func filterStepsByJobs(steps []jobStepsTarget, jobs []string) []jobStepsTarget {
	if len(jobs) == 0 {
		return nil
	}
	derived := workflowJobDerivations(jobs)
	var result []jobStepsTarget
	for _, step := range steps {
		runtimeJobs := derived[step.Job]
		if len(runtimeJobs) == 0 {
			continue
		}
		step.DerivedFrom = runtimeJobs
		result = append(result, step)
	}
	return result
}

func workflowJobDerivations(runtimeJobs []string) map[string][]string {
	result := map[string][]string{}
	for _, runtimeJob := range runtimeJobs {
		runtimeJob = strings.TrimSpace(runtimeJob)
		if runtimeJob == "" {
			continue
		}
		workflowJob := workflowJobForRuntimeJob(runtimeJob)
		result[workflowJob] = append(result[workflowJob], runtimeJob)
	}
	for workflowJob, values := range result {
		result[workflowJob] = uniqueStringsPreserveOrder(values)
	}
	return result
}

func workflowJobForRuntimeJob(runtimeJob string) string {
	runtimeJob = strings.TrimSpace(runtimeJob)
	marker := ":matrix-"
	index := strings.LastIndex(runtimeJob, marker)
	if index <= 0 {
		return runtimeJob
	}
	suffix := runtimeJob[index+len(marker):]
	if suffix == "" {
		return runtimeJob
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return runtimeJob
		}
	}
	return runtimeJob[:index]
}

func filterImagesByJobs(images []imageTarget, jobs []string) []imageTarget {
	if len(jobs) == 0 {
		return images
	}
	allowed := workflowJobDerivations(jobs)
	var result []imageTarget
	for _, image := range images {
		if len(allowed[image.Job]) > 0 {
			result = append(result, image)
		}
	}
	return result
}

func filterCommands(commands []commandTarget, match func(string) bool) []commandTarget {
	var result []commandTarget
	for _, command := range commands {
		if match(command.Command) {
			result = append(result, command)
		}
	}
	return result
}

func retryCommand(command string) string {
	if strings.Contains(command, "autoci_retry") || strings.Contains(command, "until ") {
		return command
	}
	prefix, install := splitInstallCommand(command)
	retried := strings.Join([]string{
		"n=0",
		fmt.Sprintf("until %s; do", install),
		"  n=$((n+1))",
		`  [ "$n" -ge 3 ] && exit 1`,
		"  sleep $((n * 5))",
		"done",
	}, "\n")
	if prefix != "" {
		return prefix + "\n" + retried
	}
	return retried
}

func splitInstallCommand(command string) (string, string) {
	lower := strings.ToLower(command)
	index := -1
	for _, token := range []string{"yarn install", "npm ci", "npm install", "pnpm install", "go mod download"} {
		if found := strings.Index(lower, token); found >= 0 && (index == -1 || found < index) {
			index = found
		}
	}
	if index <= 0 {
		return "", command
	}
	prefix := strings.TrimSpace(command[:index])
	prefix = strings.TrimSuffix(prefix, "&&")
	prefix = strings.TrimSpace(prefix)
	return prefix, strings.TrimSpace(command[index:])
}

func isSnykIntegrityContext(options Options) bool {
	text := researchText(options)
	return strings.Contains(text, "snyk") &&
		(strings.Contains(text, "actual:") || strings.Contains(text, "expected:") || strings.Contains(text, "checksum") || strings.Contains(text, "integrity verification"))
}

func supportsInstallRetry(options Options) bool {
	text := researchText(options)
	for _, token := range []string{
		"econnreset", "etimedout", "timeout", "eai_again", "enotfound", "connection reset",
		"connection refused", "503", "502", "504", "temporary failure", "network", "retry",
		"rate limit", "toomanyrequests",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func researchText(options Options) string {
	var parts []string
	parts = append(parts, options.Evidence, options.Signature, string(options.Readiness))
	parts = append(parts, options.LogExcerpts...)
	for key, values := range options.Artifacts {
		parts = append(parts, key)
		parts = append(parts, values...)
	}
	for _, hypothesis := range options.Hypotheses {
		parts = append(parts, hypothesis.Summary)
		parts = append(parts, hypothesis.Evidence...)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func primaryResearchHypothesis(options Options, fallback string) string {
	if len(options.Hypotheses) > 0 && options.Hypotheses[0].Summary != "" {
		return options.Hypotheses[0].Summary
	}
	return fallback
}

func isDependencyInstallCommand(command string) bool {
	return stepresolver.IsDependencyInstallCommand(strings.TrimSpace(command))
}

func signatureMatchesPackageManager(signature, command string) bool {
	signature = strings.ToLower(signature)
	command = strings.ToLower(command)
	switch {
	case signature == "" || strings.Contains(signature, "dependency install"):
		return true
	case strings.HasPrefix(signature, "npm install failure"):
		return isDependencyInstallCommand(command)
	case strings.Contains(signature, "npm"):
		return strings.HasPrefix(command, "npm ")
	case strings.Contains(signature, "yarn"):
		return strings.HasPrefix(command, "yarn ")
	case strings.Contains(signature, "pnpm"):
		return strings.HasPrefix(command, "pnpm ")
	case strings.Contains(signature, "go mod"):
		return strings.HasPrefix(command, "go mod download")
	case strings.Contains(signature, "install failure"):
		return isDependencyInstallCommand(command)
	default:
		return true
	}
}

func selectionReason(candidate commandTarget) string {
	if candidate.Confidence <= 0 {
		return fmt.Sprintf("Selected `%s` in job `%s` because it is the only dependency install command in the targeted job.", candidate.Command, candidate.Job)
	}
	reason := fmt.Sprintf("Selected `%s` in job `%s` with workflow-step confidence %.2f", candidate.Command, candidate.Job, candidate.Confidence)
	if len(candidate.Why) > 0 {
		reason += " because " + strings.Join(candidate.Why, ", ")
	}
	return reason + "."
}

func isImagePull(id string) bool {
	return strings.Contains(id, "image-pull")
}

func isDependencyInstall(id string) bool {
	return strings.Contains(id, "npm-install") || strings.Contains(id, "dependency-install") || strings.Contains(id, "yarn-install") || strings.Contains(id, "pnpm-install")
}

func isLintInstability(id string) bool {
	return strings.Contains(id, "flaky-job") || strings.Contains(id, "go-lint") || strings.Contains(id, "golangci-lint")
}

func isSingleLine(value string) bool {
	return value != "" && !strings.Contains(value, "\n")
}

func looksLikeImageReference(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "docker://") ||
		strings.Contains(lower, ".pkg.dev/") ||
		strings.Contains(lower, ".amazonaws.com/") ||
		strings.Contains(lower, "ghcr.io/") ||
		strings.Contains(lower, "docker.io/")
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	value, _ := mappingValueWithKeyLine(node, key)
	return value
}

func mappingValueWithKeyLine(node *yaml.Node, key string) (*yaml.Node, int) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, 0
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], node.Content[i].Line
		}
	}
	return nil, 0
}

func lineAt(lines []string, line int) string {
	if line <= 0 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func leadingWhitespace(value string) string {
	for i, r := range value {
		if r != ' ' && r != '\t' {
			return value[:i]
		}
	}
	return value
}

func indentWidth(value string) int {
	return len(leadingWhitespace(value))
}

func lineDiff(path, original string, line int, oldLine, newLine string) string {
	lines := strings.Split(original, "\n")
	start := line - 3
	if start < 1 {
		start = 1
	}
	end := line + 3
	if end > len(lines) {
		end = len(lines)
	}
	var builder strings.Builder
	rel := filepath.ToSlash(path)
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", rel, rel)
	fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", start, end-start+1, start, end-start+1)
	for i := start; i <= end; i++ {
		switch i {
		case line:
			fmt.Fprintf(&builder, "-%s\n", oldLine)
			for _, added := range strings.Split(newLine, "\n") {
				fmt.Fprintf(&builder, "+%s\n", added)
			}
		default:
			fmt.Fprintf(&builder, " %s\n", lines[i-1])
		}
	}
	return builder.String()
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

func normalizeOpportunity(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "research-")
	value = strings.TrimPrefix(value, "reliability-")
	if strings.HasPrefix(value, "image-pull") {
		return "failure-theme-image-pull-failure"
	}
	if strings.HasPrefix(value, "npm-install") {
		return "failure-theme-npm-install-failure"
	}
	return value
}

func trimFixPrefix(value string) string {
	value = strings.TrimPrefix(value, "fix-")
	value = strings.TrimPrefix(value, "failure-theme-")
	value = strings.TrimPrefix(value, "theme-")
	return slug(value)
}

func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result[value] = true
		}
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
		return "selected-opportunity"
	}
	return result
}
