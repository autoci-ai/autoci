package fix

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/autoci-ai/autoci/internal/scanner"
	"gopkg.in/yaml.v3"
)

type Plan struct {
	ID              string     `json:"id"`
	SourceID        string     `json:"sourceId"`
	Branch          string     `json:"branch,omitempty"`
	Workflow        string     `json:"workflow"`
	Hypothesis      string     `json:"hypothesis"`
	Evidence        string     `json:"evidence"`
	ChangeSummary   string     `json:"changeSummary"`
	SuccessCriteria string     `json:"successCriteria"`
	Confidence      string     `json:"confidence"`
	Reason          string     `json:"reason,omitempty"`
	PatchGenerated  bool       `json:"patchGenerated"`
	PatchApplied    bool       `json:"patchApplied"`
	Targets         []Target   `json:"targets,omitempty"`
	PatchScope      PatchScope `json:"patchScope"`
	Diff            string     `json:"diff,omitempty"`
	Validation      []string   `json:"validation"`
	FilesChanged    []string   `json:"filesChanged,omitempty"`
}

type Target struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job,omitempty"`
	Step     string `json:"step,omitempty"`
	Command  string `json:"command,omitempty"`
	Image    string `json:"image,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type PatchScope struct {
	FilesChanged          int      `json:"filesChanged"`
	JobsTouched           []string `json:"jobsTouched"`
	StepsTouched          []string `json:"stepsTouched"`
	UnrelatedLinesChanged int      `json:"unrelatedLinesChanged"`
}

type Options struct {
	RepoPath     string
	Workflow     scanner.Workflow
	WorkflowName string
	Opportunity  string
	DryRun       bool
	Evidence     string
	TargetJobs   []string
	Occurrences  int
	Signature    string
	Artifacts    map[string][]string
}

type Record struct {
	ID              string     `json:"id"`
	SourceItemID    string     `json:"sourceItemId"`
	Workflow        string     `json:"workflow"`
	Branch          string     `json:"branch,omitempty"`
	Hypothesis      string     `json:"hypothesis"`
	Evidence        string     `json:"evidence"`
	ChangeSummary   string     `json:"changeSummary"`
	SuccessCriteria string     `json:"successCriteria"`
	Confidence      string     `json:"confidence"`
	Reason          string     `json:"reason,omitempty"`
	PatchGenerated  bool       `json:"patchGenerated"`
	PatchApplied    bool       `json:"patchApplied"`
	Targets         []Target   `json:"targets,omitempty"`
	PatchScope      PatchScope `json:"patchScope"`
	FilesChanged    []string   `json:"filesChanged"`
	DryRun          bool       `json:"dryRun"`
}

type workflowInspection struct {
	Commands []commandTarget
	Images   []imageTarget
}

type commandTarget struct {
	Target
	LineText string
}

type imageTarget struct {
	Target
	LineText string
}

func Generate(options Options) (Plan, error) {
	sourceID := strings.TrimSpace(options.Opportunity)
	id := normalizeOpportunity(sourceID)
	if id == "" {
		id = "failure-theme-image-pull-failure"
	}
	plan := basePlan(id, sourceID, options.WorkflowName, options.Evidence)

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
		return plan, nil
	}
	if plan.Confidence == "low" {
		plan.PatchGenerated = false
		plan.Diff = ""
		plan.FilesChanged = nil
		plan.PatchScope.FilesChanged = 0
		plan.Reason = "Low-confidence fixes produce plans only. AutoCI needs a more exact target before modifying the workflow."
		return plan, nil
	}
	if options.DryRun {
		return plan, nil
	}
	if err := createBranch(options.RepoPath, plan.Branch); err != nil {
		return Plan{}, err
	}
	if err := os.WriteFile(options.Workflow.Path, []byte(applyLineDiff(string(original), plan)), 0o644); err != nil {
		return Plan{}, err
	}
	plan.PatchApplied = true
	return plan, nil
}

func NewRecord(plan Plan, dryRun bool) Record {
	return Record{
		ID:              "fix-" + trimFixPrefix(plan.ID),
		SourceItemID:    plan.SourceID,
		Workflow:        plan.Workflow,
		Branch:          plan.Branch,
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

func basePlan(id, sourceID, workflow, evidence string) Plan {
	if sourceID == "" {
		sourceID = id
	}
	plan := Plan{
		ID:         "fix-" + trimFixPrefix(id),
		SourceID:   sourceID,
		Branch:     "autoci/fix-" + trimFixPrefix(id),
		Workflow:   workflow,
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
	candidates := filterCommandsByJobs(inspection.Commands, options.TargetJobs)
	candidates = filterCommands(candidates, isDependencyInstallCommand)
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
	updated := retryCommand(candidate.Command)
	if updated == candidate.Command {
		plan.Reason = "The targeted command already appears to be wrapped or cannot be safely rewritten."
		return
	}
	plan.Confidence = "medium"
	plan.ChangeSummary = fmt.Sprintf("Wrap only `%s` in the `%s` job with bounded retry logic.", candidate.Command, candidate.Job)
	setSurgicalPatch(plan, original, candidate.Line, candidate.LineText, strings.Replace(candidate.LineText, candidate.Command, updated, 1), candidate.Target)
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
	setSurgicalPatch(plan, original, candidate.Line, candidate.LineText, strings.Replace(candidate.LineText, candidate.Command, updated, 1), candidate.Target)
}

func refuseSingleOccurrence(plan *Plan) {
	plan.Confidence = "low"
	plan.Reason = "This failure theme has only 1 occurrence. AutoCI needs more evidence before modifying the workflow."
	plan.Validation = diagnosticNextSteps(plan.Workflow)
}

func diagnosticNextSteps(workflow string) []string {
	return []string{fmt.Sprintf("autoci failures --workflow %s --verbose", workflow)}
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
	plan.Diff = lineDiff(plan.Workflow, string(original), line, oldLine, newLine)
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
		steps := mappingValue(body, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
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

func applyLineDiff(original string, plan Plan) string {
	if !plan.PatchGenerated || len(plan.Targets) == 0 {
		return original
	}
	lines := strings.SplitAfter(original, "\n")
	targetLine := plan.Targets[0].Line
	if targetLine <= 0 || targetLine > len(lines) {
		return original
	}
	for _, line := range strings.Split(plan.Diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines[targetLine-1] = strings.TrimPrefix(line, "+")
			if !strings.HasSuffix(lines[targetLine-1], "\n") {
				lines[targetLine-1] += "\n"
			}
		}
	}
	return strings.Join(lines, "")
}

func filterCommandsByJobs(commands []commandTarget, jobs []string) []commandTarget {
	if len(jobs) == 0 {
		return commands
	}
	allowed := stringSet(jobs)
	var result []commandTarget
	for _, command := range commands {
		if allowed[command.Job] {
			result = append(result, command)
		}
	}
	return result
}

func filterImagesByJobs(images []imageTarget, jobs []string) []imageTarget {
	if len(jobs) == 0 {
		return images
	}
	allowed := stringSet(jobs)
	var result []imageTarget
	for _, image := range images {
		if allowed[image.Job] {
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
	escaped := strings.ReplaceAll(command, `"`, `\"`)
	return fmt.Sprintf(`n=0; until %s; do n=$((n+1)); [ "$n" -ge 3 ] && exit 1; sleep $((n * 5)); done`, escaped)
}

func isDependencyInstallCommand(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "npm ci") ||
		strings.HasPrefix(command, "npm install") ||
		strings.HasPrefix(command, "yarn install") ||
		strings.HasPrefix(command, "pnpm install") ||
		strings.HasPrefix(command, "go mod download")
}

func signatureMatchesPackageManager(signature, command string) bool {
	signature = strings.ToLower(signature)
	command = strings.ToLower(command)
	switch {
	case signature == "" || strings.Contains(signature, "dependency install"):
		return true
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

func lineAt(lines []string, line int) string {
	if line <= 0 || line > len(lines) {
		return ""
	}
	return lines[line-1]
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
			fmt.Fprintf(&builder, "+%s\n", newLine)
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
