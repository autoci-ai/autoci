package failures

import (
	"regexp"
	"sort"
	"strings"
)

type Analysis struct {
	Workflow        string           `json:"workflow"`
	RunsAnalyzed    int              `json:"runsAnalyzed"`
	FailedRuns      int              `json:"failedRuns"`
	Findings        []Finding        `json:"findings,omitempty"`
	FailureThemes   []FailureTheme   `json:"failureThemes"`
	AggregationJobs []AggregationJob `json:"aggregationJobs,omitempty"`
}

type Finding struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Category       string            `json:"category"`
	Signature      string            `json:"signature"`
	Occurrences    int               `json:"occurrences"`
	Jobs           []string          `json:"jobs,omitempty"`
	Recommendation string            `json:"recommendation,omitempty"`
	Evidence       []FailureEvidence `json:"evidence,omitempty"`
}

type FailureTheme struct {
	ID             string            `json:"id"`
	Signature      string            `json:"signature"`
	Occurrences    int               `json:"occurrences"`
	Jobs           []string          `json:"jobs"`
	ExampleRun     string            `json:"exampleRun,omitempty"`
	Recommendation string            `json:"recommendation"`
	Artifacts      FailureArtifacts  `json:"artifacts,omitempty"`
	Evidence       []FailureEvidence `json:"evidence,omitempty"`
}

type FailureEvidence struct {
	RunID              string   `json:"runId,omitempty"`
	Job                string   `json:"job,omitempty"`
	InstrumentationID  string   `json:"instrumentationId,omitempty"`
	LogExcerpt         string   `json:"logExcerpt,omitempty"`
	Registry           string   `json:"registry,omitempty"`
	RegistryHost       string   `json:"registryHost,omitempty"`
	Image              string   `json:"image,omitempty"`
	ImageTag           string   `json:"imageTag,omitempty"`
	ImageDigest        string   `json:"imageDigest,omitempty"`
	PullError          string   `json:"pullError,omitempty"`
	PackageName        string   `json:"packageName,omitempty"`
	PackageVersion     string   `json:"packageVersion,omitempty"`
	RegistryURL        string   `json:"registryUrl,omitempty"`
	DependencyConflict string   `json:"dependencyConflict,omitempty"`
	InstallError       string   `json:"installError,omitempty"`
	TestName           string   `json:"testName,omitempty"`
	StackTrace         string   `json:"stackTrace,omitempty"`
	SourceFile         string   `json:"sourceFile,omitempty"`
	AssertionMessage   string   `json:"assertionMessage,omitempty"`
	BuildTarget        string   `json:"buildTarget,omitempty"`
	CompilerError      string   `json:"compilerError,omitempty"`
	MissingDependency  string   `json:"missingDependency,omitempty"`
	ExtractedItems     []string `json:"extractedItems,omitempty"`
}

type FailureArtifacts struct {
	Images      []string `json:"images,omitempty"`
	Packages    []string `json:"packages,omitempty"`
	Modules     []string `json:"modules,omitempty"`
	URLs        []string `json:"urls,omitempty"`
	Hosts       []string `json:"hosts,omitempty"`
	Dockerfiles []string `json:"dockerfiles,omitempty"`
}

type AggregationJob struct {
	Job         string `json:"job"`
	Occurrences int    `json:"occurrences"`
}

type Observation struct {
	Job               string
	RunID             string
	Message           string
	InstrumentationID string
	IsAggregator      bool
}

func Analyze(workflow string, runsAnalyzed, failedRuns int, observations []Observation) Analysis {
	themes := map[string]*themeBuilder{}
	aggregationJobs := map[string]int{}
	for _, observation := range observations {
		if IsAggregationJob(observation.Job) || observation.IsAggregator {
			aggregationJobs[observation.Job]++
			continue
		}
		instrumentationID := firstNonEmpty(observation.InstrumentationID, FirstInstrumentationID(observation.Message))
		signature := Signature(observation.Message)
		if instrumentationID != "" {
			signature = signatureForInstrumentationID(instrumentationID, signature)
		}
		theme := themes[signature]
		if theme == nil {
			id := "failure-theme-" + slug(signature)
			if instrumentationID != "" && strings.HasPrefix(instrumentationID, "failure-theme-") {
				id = instrumentationID
			}
			theme = &themeBuilder{
				FailureTheme: FailureTheme{
					ID:             id,
					Signature:      signature,
					ExampleRun:     observation.RunID,
					Recommendation: Recommendation(signature),
				},
				jobs: map[string]bool{},
			}
			themes[signature] = theme
		}
		theme.Occurrences++
		if observation.Job != "" {
			theme.jobs[observation.Job] = true
		}
		for _, evidence := range ExtractEvidence(signature, observation) {
			theme.Evidence = appendEvidence(theme.Evidence, evidence)
			mergeArtifacts(&theme.Artifacts, artifactsFromEvidence(evidence))
		}
	}

	analysis := Analysis{
		Workflow:        workflow,
		RunsAnalyzed:    runsAnalyzed,
		FailedRuns:      failedRuns,
		FailureThemes:   []FailureTheme{},
		AggregationJobs: []AggregationJob{},
	}
	for _, theme := range themes {
		for job := range theme.jobs {
			theme.Jobs = append(theme.Jobs, job)
		}
		sort.Strings(theme.Jobs)
		theme.FailureTheme.Artifacts = compactArtifacts(theme.Artifacts)
		analysis.FailureThemes = append(analysis.FailureThemes, theme.FailureTheme)
		analysis.Findings = append(analysis.Findings, Finding{
			ID:             theme.ID,
			Kind:           "failure",
			Category:       failureCategory(theme.Signature),
			Signature:      theme.Signature,
			Occurrences:    theme.Occurrences,
			Jobs:           theme.Jobs,
			Recommendation: theme.Recommendation,
			Evidence:       theme.Evidence,
		})
	}
	sort.SliceStable(analysis.FailureThemes, func(i, j int) bool {
		if analysis.FailureThemes[i].Occurrences == analysis.FailureThemes[j].Occurrences {
			return analysis.FailureThemes[i].ID < analysis.FailureThemes[j].ID
		}
		return analysis.FailureThemes[i].Occurrences > analysis.FailureThemes[j].Occurrences
	})
	sort.SliceStable(analysis.Findings, func(i, j int) bool {
		if analysis.Findings[i].Occurrences == analysis.Findings[j].Occurrences {
			return analysis.Findings[i].ID < analysis.Findings[j].ID
		}
		return analysis.Findings[i].Occurrences > analysis.Findings[j].Occurrences
	})
	for job, occurrences := range aggregationJobs {
		analysis.AggregationJobs = append(analysis.AggregationJobs, AggregationJob{Job: job, Occurrences: occurrences})
	}
	sort.SliceStable(analysis.AggregationJobs, func(i, j int) bool {
		if analysis.AggregationJobs[i].Occurrences == analysis.AggregationJobs[j].Occurrences {
			return analysis.AggregationJobs[i].Job < analysis.AggregationJobs[j].Job
		}
		return analysis.AggregationJobs[i].Occurrences > analysis.AggregationJobs[j].Occurrences
	})
	return analysis
}

type themeBuilder struct {
	FailureTheme
	jobs map[string]bool
}

type EvidenceExtractor interface {
	Extract(Observation) []FailureEvidence
}

func ExtractEvidence(signature string, observation Observation) []FailureEvidence {
	var extractor EvidenceExtractor
	switch signature {
	case "image pull failure", "image pull timeout":
		extractor = ImagePullFailureExtractor{}
	case "npm install failure":
		extractor = NPMFailureExtractor{}
	case "test failure", "jest timeout":
		extractor = TestFailureExtractor{}
	case "build failure":
		extractor = BuildFailureExtractor{}
	default:
		extractor = GenericFailureExtractor{}
	}
	evidence := extractor.Extract(observation)
	instrumentationID := firstNonEmpty(observation.InstrumentationID, FirstInstrumentationID(observation.Message))
	if instrumentationID == "" {
		return evidence
	}
	for i := range evidence {
		if evidence[i].InstrumentationID == "" {
			evidence[i].InstrumentationID = instrumentationID
		}
	}
	if len(evidence) == 0 {
		evidence = append(evidence, baseEvidence(observation, "AutoCI diagnostics for "+instrumentationID))
	}
	return evidence
}

func InstrumentationIDs(message string) []string {
	re := regexp.MustCompile(`AutoCI diagnostics for ([A-Za-z0-9][A-Za-z0-9._:-]*)`)
	var result []string
	seen := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(message, -1) {
		if len(match) < 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		result = append(result, match[1])
	}
	return result
}

func FirstInstrumentationID(message string) string {
	ids := InstrumentationIDs(message)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func signatureForInstrumentationID(id, fallback string) string {
	value := strings.TrimPrefix(id, "failure-theme-")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func IsAggregationJob(job string) bool {
	lower := strings.ToLower(job)
	for _, term := range []string{"gate", "required", "status", "aggregate", "summary"} {
		if lower == term || strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func Signature(message string) string {
	lower := strings.ToLower(message)
	patterns := []struct {
		contains  []string
		signature string
	}{
		{[]string{"golangci-lint", "timeout"}, "golangci-lint timeout"},
		{[]string{"golangci-lint", "timed out"}, "golangci-lint timeout"},
		{[]string{"jest", "timeout"}, "jest timeout"},
		{[]string{"npm", "install"}, "npm install failure"},
		{[]string{"npm err!"}, "npm install failure"},
		{[]string{"yarn", "install"}, "npm install failure"},
		{[]string{"yarn", "error"}, "npm install failure"},
		{[]string{"corepack", "error"}, "npm install failure"},
		{[]string{"image", "pull"}, "image pull failure"},
		{[]string{"pull", "timeout"}, "image pull timeout"},
		{[]string{"failed to solve"}, "build failure"},
		{[]string{"compiler error"}, "build failure"},
		{[]string{"undefined:"}, "build failure"},
		{[]string{"test failed"}, "test failure"},
		{[]string{"assertion"}, "test failure"},
		{[]string{"context deadline exceeded"}, "context deadline exceeded"},
		{[]string{"permission denied"}, "permission denied"},
		{[]string{"no space left"}, "disk space exhausted"},
		{[]string{"connection refused"}, "connection refused"},
		{[]string{"timed out"}, "timeout"},
		{[]string{"timeout"}, "timeout"},
	}
	for _, pattern := range patterns {
		if containsAll(lower, pattern.contains) {
			return pattern.signature
		}
	}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, 80)
		}
	}
	return "unknown failure"
}

func Recommendation(signature string) string {
	switch signature {
	case "golangci-lint timeout":
		return "Investigate timeout thresholds and runtime regressions in the lint job."
	case "jest timeout":
		return "Investigate slow or hanging tests and whether test sharding is needed."
	case "npm install failure":
		return "Investigate package registry availability, lockfile consistency, and dependency cache behavior."
	case "image pull timeout", "image pull failure":
		return "Investigate registry availability, image caching, image pinning, and network reliability."
	case "context deadline exceeded", "timeout":
		return "Investigate timeout thresholds, external dependencies, and recent runtime regressions."
	case "permission denied":
		return "Investigate credentials, file permissions, and secret availability."
	case "disk space exhausted":
		return "Investigate workspace cleanup, build artifact size, and cache growth."
	default:
		return "Inspect recent failed runs and compare logs for recurring error messages."
	}
}

func containsAll(value string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}

var (
	imageRefToken        = `([^\s"'<>]+)`
	urlPattern           = regexp.MustCompile(`https?://[^\s"'<>]+`)
	ansiPattern          = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	scopedPackagePattern = regexp.MustCompile(`(^|[^a-zA-Z0-9._/-])(@[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+)`)
	npmSpecPattern       = regexp.MustCompile(`(^|[\s"'(])(@?[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)?)@(?:npm:|patch:|workspace:|file:|link:|[0-9][a-zA-Z0-9._+~:-]*)`)
	packageJSONPattern   = regexp.MustCompile(`"(@?[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)?)"\s*:\s*"[^"]+"`)
	testNamePattern      = regexp.MustCompile(`(?:FAIL|--- FAIL:|not ok)\s+([A-Za-z0-9_./:-]+)`)
	sourceFilePattern    = regexp.MustCompile(`(?:^|\s)([A-Za-z0-9_./-]+\.(?:go|js|jsx|ts|tsx|java|py|rb|c|cc|cpp|h))(?::[0-9]+(?::[0-9]+)?)?`)
	buildTargetPattern   = regexp.MustCompile(`(?:target|building|make)\s+([A-Za-z0-9_./:-]+)`)
	imageContextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcreating\s+container\s+for\s+image\s+["']?` + imageRefToken),
		regexp.MustCompile(`(?i)\bimage:\s*["']?` + imageRefToken),
		regexp.MustCompile(`(?i)\bfor\s+image\s+["']?` + imageRefToken),
		regexp.MustCompile(`(?i)\bdocker\s+pull\s+["']?` + imageRefToken),
		regexp.MustCompile(`(?i)\b(?:failed\s+to\s+pull|failure\s+pulling|pulling|pulled|pull)\s+(?:docker\s+)?image\s+["']?` + imageRefToken),
		regexp.MustCompile(`(?i)\bimage\s+pull(?:\s+\w+)*\s+from\s+["']?` + imageRefToken),
	}
	timeLikeImagePattern = regexp.MustCompile(`^\d{1,2}:\d{2}`)
)

func matches(message string, pattern *regexp.Regexp) []string {
	var result []string
	for _, match := range pattern.FindAllString(message, -1) {
		match = strings.TrimSpace(strings.Trim(match, `"'<>.,;:`))
		if match != "" {
			result = append(result, match)
		}
	}
	return result
}

type ImagePullFailureExtractor struct{}

func (ImagePullFailureExtractor) Extract(observation Observation) []FailureEvidence {
	var result []FailureEvidence
	lines := nonEmptyLines(observation.Message)
	contextImages := imageReferences(observation.Message)
	for _, line := range lines {
		if !isImagePullErrorLine(line) {
			continue
		}
		refs := imageReferences(line)
		if len(refs) == 0 {
			refs = contextImages
		}
		if len(refs) == 0 {
			evidence := baseEvidence(observation, line)
			evidence.PullError = pullError(line)
			if evidence.PullError != "" {
				result = append(result, evidence)
			}
			continue
		}
		for _, ref := range refs {
			evidence := baseEvidence(observation, line)
			evidence.Image = ref
			evidence.Registry, evidence.RegistryHost = imageRegistry(ref)
			evidence.ImageTag = imageTag(ref)
			evidence.ImageDigest = imageDigest(ref)
			evidence.PullError = pullError(line)
			result = append(result, evidence)
		}
	}
	return result
}

type NPMFailureExtractor struct{}

func (NPMFailureExtractor) Extract(observation Observation) []FailureEvidence {
	var result []FailureEvidence
	message := stripANSI(observation.Message)
	lines := nonEmptyLines(message)
	for index, line := range lines {
		if !isPackageDiagnosticLine(strings.ToLower(line)) {
			continue
		}
		item := baseEvidence(observation, line)
		item.RegistryURL = firstURL(line)
		item.DependencyConflict = dependencyConflict(line)
		item.InstallError = installError(line)
		hasPackage := false
		for _, pkg := range npmPackages(line) {
			if isNPMErrorWarningLine(line) || adjacentNPMErrorWarningMentionsPackage(lines, index, pkg.name) {
				hasPackage = true
				next := item
				next.PackageName = pkg.name
				next.PackageVersion = pkg.version
				result = append(result, next)
			}
		}
		if !hasPackage && (item.RegistryURL != "" || item.DependencyConflict != "" || item.InstallError != "") {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		for _, line := range diagnosticLines(message, []string{"npm", "pnpm", "yarn", "corepack", "eresolve", "registry", "dependency", "package"}) {
			item := baseEvidence(observation, line)
			item.RegistryURL = firstURL(line)
			item.DependencyConflict = dependencyConflict(line)
			item.InstallError = installError(line)
			if item.RegistryURL != "" || item.DependencyConflict != "" || item.InstallError != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

type TestFailureExtractor struct{}

func (TestFailureExtractor) Extract(observation Observation) []FailureEvidence {
	var result []FailureEvidence
	for _, line := range diagnosticLines(observation.Message, []string{"fail", "assert", "expect", "panic", "stack"}) {
		evidence := baseEvidence(observation, line)
		evidence.TestName = firstSubmatch(line, testNamePattern)
		evidence.SourceFile = firstSubmatch(line, sourceFilePattern)
		evidence.AssertionMessage = assertionMessage(line)
		if strings.Contains(strings.ToLower(line), "stack") || strings.Contains(line, "\tat ") {
			evidence.StackTrace = truncate(line, 240)
		}
		if evidence.TestName != "" || evidence.SourceFile != "" || evidence.AssertionMessage != "" || evidence.StackTrace != "" {
			result = append(result, evidence)
		}
	}
	return result
}

type BuildFailureExtractor struct{}

func (BuildFailureExtractor) Extract(observation Observation) []FailureEvidence {
	var result []FailureEvidence
	for _, line := range diagnosticLines(observation.Message, []string{"error", "failed", "undefined", "missing", "target", "build"}) {
		evidence := baseEvidence(observation, line)
		evidence.SourceFile = firstSubmatch(line, sourceFilePattern)
		evidence.BuildTarget = firstSubmatch(strings.ToLower(line), buildTargetPattern)
		evidence.CompilerError = compilerError(line)
		evidence.MissingDependency = missingDependency(line)
		if evidence.SourceFile != "" || evidence.BuildTarget != "" || evidence.CompilerError != "" || evidence.MissingDependency != "" {
			result = append(result, evidence)
		}
	}
	return result
}

type GenericFailureExtractor struct{}

func (GenericFailureExtractor) Extract(observation Observation) []FailureEvidence {
	if excerpt := excerpt(observation.Message); excerpt != "" {
		return []FailureEvidence{baseEvidence(observation, excerpt)}
	}
	return nil
}

func baseEvidence(observation Observation, line string) FailureEvidence {
	return FailureEvidence{
		RunID:             observation.RunID,
		Job:               observation.Job,
		InstrumentationID: firstNonEmpty(observation.InstrumentationID, FirstInstrumentationID(observation.Message), FirstInstrumentationID(line)),
		LogExcerpt:        truncate(strings.TrimSpace(line), 240),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func diagnosticLines(message string, terms []string) []string {
	var result []string
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				result = append(result, trimmed)
				break
			}
		}
	}
	if len(result) == 0 {
		if first := excerpt(message); first != "" {
			result = append(result, first)
		}
	}
	return result
}

func nonEmptyLines(message string) []string {
	var result []string
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func imageReferences(line string) []string {
	var result []string
	for _, pattern := range imageContextPatterns {
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			if len(match) < 2 {
				continue
			}
			candidate := strings.TrimSpace(strings.Trim(match[1], `"'<>.,;:`))
			if isDiagnosticImage(candidate) {
				result = append(result, candidate)
			}
		}
	}
	return uniqueSorted(result)
}

func isDiagnosticImage(value string) bool {
	lower := strings.ToLower(value)
	if timeLikeImagePattern.MatchString(value) || strings.HasPrefix(lower, "deadline:") || strings.HasPrefix(value, "Port:") {
		return false
	}
	if strings.HasPrefix(value, "docker://") || strings.Contains(value, "@sha256:") {
		return true
	}
	for _, prefix := range []string{"ghcr.io/", "docker.io/", "quay.io/", "gcr.io/", "registry.k8s.io/"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	if strings.Contains(value, "/") {
		return strings.Contains(value, ":")
	}
	return isKnownBareImage(value)
}

func isKnownBareImage(value string) bool {
	name, tag, ok := strings.Cut(value, ":")
	if !ok || name == "" || tag == "" {
		return false
	}
	switch strings.ToLower(name) {
	case "mysql":
		return tag == "8.0"
	case "postgres", "redis", "minio":
		return true
	default:
		return false
	}
}

func imageRegistry(ref string) (string, string) {
	value := strings.TrimPrefix(ref, "docker://")
	parts := strings.Split(value, "/")
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		return parts[0], parts[0]
	}
	return "docker.io", "docker.io"
}

func imageTag(ref string) string {
	if idx := strings.LastIndex(ref, ":"); idx >= 0 && !strings.Contains(ref[idx:], "/") && !strings.Contains(ref[idx:], "@") {
		return ref[idx+1:]
	}
	return ""
}

func imageDigest(ref string) string {
	if idx := strings.Index(ref, "@"); idx >= 0 {
		return ref[idx+1:]
	}
	return ""
}

func pullError(line string) string {
	lower := strings.ToLower(line)
	for _, term := range []string{"pull access denied", "manifest unknown", "rate limit", "toomanyrequests", "too many requests", "failed to pull", "image pull", "no such host", "timeout", "connection reset", "connection refused", "not found", "unauthorized", "denied", "context deadline exceeded"} {
		if strings.Contains(lower, term) {
			return term
		}
	}
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return truncate(line, 160)
	}
	return ""
}

func isImagePullErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, term := range []string{"check started", "waiting for image pull", "waiting for container", "metadata", "container is ready", "creating container", "created container", "started container", "connected to container"} {
		if strings.Contains(lower, term) {
			return false
		}
	}
	for _, term := range []string{"pull access denied", "manifest unknown", "rate limit", "toomanyrequests", "too many requests", "failed to pull", "no such host", "timeout", "connection reset", "connection refused"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	if strings.Contains(lower, "image pull") {
		return strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "denied")
	}
	return false
}

type npmPackage struct {
	name    string
	version string
}

func npmPackages(line string) []npmPackage {
	var result []npmPackage
	lower := strings.ToLower(line)
	if !isPackageDiagnosticLine(lower) {
		return result
	}
	for _, match := range scopedPackagePattern.FindAllStringSubmatch(line, -1) {
		if len(match) < 3 {
			continue
		}
		if name := cleanPackageName(match[2]); name != "" {
			result = append(result, npmPackage{name: name})
		}
	}
	for _, match := range npmSpecPattern.FindAllStringSubmatch(line, -1) {
		if len(match) < 3 {
			continue
		}
		if name := cleanPackageName(match[2]); name != "" {
			result = append(result, npmPackage{name: name})
		}
	}
	for _, match := range packageJSONPattern.FindAllStringSubmatch(line, -1) {
		if len(match) < 2 {
			continue
		}
		if name := cleanPackageName(match[1]); name != "" {
			result = append(result, npmPackage{name: name})
		}
	}
	return uniquePackages(result)
}

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func isPackageDiagnosticLine(lower string) bool {
	for _, term := range []string{"npm", "pnpm", "yarn", "corepack", "eresolve", "peer", "package", "dependency", "lockfile"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func adjacentNPMErrorWarningMentionsPackage(lines []string, index int, pkg string) bool {
	for _, adjacent := range []int{index - 1, index + 1} {
		if adjacent < 0 || adjacent >= len(lines) || !isNPMErrorWarningLine(lines[adjacent]) {
			continue
		}
		if lineMentionsPackage(lines[adjacent], pkg) {
			return true
		}
	}
	return false
}

func lineMentionsPackage(line, pkg string) bool {
	if pkg == "" {
		return false
	}
	return strings.Contains(strings.ToLower(line), strings.ToLower(pkg))
}

func isNPMErrorWarningLine(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "snyk") && strings.Contains(lower, "stderr") && (strings.Contains(lower, "actual:") || strings.Contains(lower, "expected:")) {
		return true
	}
	for _, term := range []string{
		"npm err!", "pnpm err!", "yarn error", "error:", "failed", "failure", "stderr",
		"could not be resolved", "doesn't provide", "does not provide", "incorrectly met",
		"peer requirements", "lockfile would have been modified", "immutable install would have modified",
		"post-resolution validation",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	if strings.Contains(lower, "yn0086") && strings.Contains(lower, "peer") {
		return true
	}
	if (strings.Contains(lower, "yn0002") || strings.Contains(lower, "yn0060")) &&
		(strings.Contains(lower, "provide") || strings.Contains(lower, "peer") || strings.Contains(lower, "incorrect")) {
		return true
	}
	return false
}

func cleanPackageName(value string) string {
	value = strings.TrimSpace(strings.Trim(value, `"'<>.,;:()[]{}|`))
	value = stripANSISuffix(value)
	lower := strings.ToLower(value)
	if value == "" || strings.HasPrefix(value, "--") || strings.Contains(value, "\x1b") {
		return ""
	}
	if strings.Contains(value, "/") && !strings.HasPrefix(value, "@") {
		return ""
	}
	if strings.Contains(value, `\`) || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") {
		return ""
	}
	if isPackageNoise(lower) || isTimestampLike(value) || isNumericLike(value) {
		return ""
	}
	if strings.HasPrefix(value, "@") {
		parts := strings.Split(value, "/")
		if len(parts) != 2 || parts[0] == "@" || parts[1] == "" {
			return ""
		}
		return value
	}
	if !isKnownUnscopedPackage(lower) {
		return ""
	}
	return value
}

func isTimestampLike(value string) bool {
	return regexp.MustCompile(`^\d{1,4}[:/-]\d`).MatchString(value)
}

func stripANSISuffix(value string) string {
	return regexp.MustCompile(`^\d{1,3}m([A-Za-z@][A-Za-z0-9._/-]*)$`).ReplaceAllString(value, "$1")
}

func isNumericLike(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' && r != 'm' && r != 's' {
			return false
		}
	}
	return true
}

func isKnownUnscopedPackage(value string) bool {
	switch value {
	case "react", "typescript", "eslint", "webpack", "vite", "jest", "next", "snyk", "pnpm", "lodash",
		"core-js", "pact-core", "unrs-resolver", "msw", "protobufjs", "esbuild":
		return true
	default:
		return false
	}
}

func uniquePackages(values []npmPackage) []npmPackage {
	seen := map[string]bool{}
	var result []npmPackage
	for _, value := range values {
		key := value.name + "@" + value.version
		if value.name == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func firstURL(line string) string {
	urls := matches(line, urlPattern)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func dependencyConflict(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "eresolve") || strings.Contains(lower, "peer dep") || strings.Contains(lower, "dependency conflict") {
		return truncate(line, 200)
	}
	return ""
}

func installError(line string) string {
	lower := strings.ToLower(line)
	for _, term := range []string{"npm err!", "pnpm err!", "yarn error", "eresolve", "enoent", "e404", "e401"} {
		if strings.Contains(lower, term) {
			return truncate(line, 200)
		}
	}
	if strings.Contains(lower, "snyk") && strings.Contains(lower, "stderr") && (strings.Contains(lower, "actual:") || strings.Contains(lower, "expected:")) {
		return truncate(line, 200)
	}
	if strings.Contains(lower, "install") && (strings.Contains(lower, "failed") || strings.Contains(lower, "error")) {
		return truncate(line, 200)
	}
	return ""
}

func assertionMessage(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "assert") || strings.Contains(lower, "expected") || strings.Contains(lower, "received") {
		return truncate(line, 200)
	}
	return ""
}

func compilerError(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "undefined:") || strings.Contains(lower, "compiler error") || strings.Contains(lower, "syntax error") || strings.Contains(lower, "error:") {
		return truncate(line, 200)
	}
	return ""
}

func missingDependency(line string) string {
	lower := strings.ToLower(line)
	for _, term := range []string{"cannot find module", "module not found", "no required module provides", "package not found"} {
		if strings.Contains(lower, term) {
			return truncate(line, 200)
		}
	}
	return ""
}

func firstSubmatch(line string, pattern *regexp.Regexp) string {
	match := pattern.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func appendEvidence(items []FailureEvidence, evidence FailureEvidence) []FailureEvidence {
	key := evidenceKey(evidence)
	for _, existing := range items {
		if evidenceKey(existing) == key {
			return items
		}
	}
	return append(items, evidence)
}

func evidenceKey(evidence FailureEvidence) string {
	return strings.Join([]string{
		evidence.RunID,
		evidence.Job,
		evidence.InstrumentationID,
		evidence.LogExcerpt,
		evidence.Image,
	}, "\x00")
}

func artifactsFromEvidence(evidence FailureEvidence) FailureArtifacts {
	var artifacts FailureArtifacts
	if evidence.Image != "" {
		artifacts.Images = []string{evidence.Image}
	}
	if evidence.PackageName != "" {
		artifacts.Packages = []string{evidence.PackageName}
	}
	if evidence.RegistryURL != "" {
		artifacts.URLs = []string{evidence.RegistryURL}
	}
	if evidence.RegistryHost != "" {
		artifacts.Hosts = []string{evidence.RegistryHost}
	}
	if evidence.SourceFile != "" && (evidence.TestName != "" || evidence.CompilerError != "" || evidence.MissingDependency != "") {
		artifacts.Modules = []string{evidence.SourceFile}
	}
	return artifacts
}

func compactArtifacts(artifacts FailureArtifacts) FailureArtifacts {
	artifacts.Images = uniqueSorted(artifacts.Images)
	artifacts.Packages = uniqueSorted(artifacts.Packages)
	artifacts.Modules = uniqueSorted(artifacts.Modules)
	artifacts.URLs = uniqueSorted(artifacts.URLs)
	artifacts.Hosts = uniqueSorted(artifacts.Hosts)
	artifacts.Dockerfiles = uniqueSorted(artifacts.Dockerfiles)
	return artifacts
}

func failureCategory(signature string) string {
	switch signature {
	case "image pull failure", "image pull timeout":
		return "container-registry"
	case "npm install failure":
		return "dependency-install"
	case "test failure", "jest timeout":
		return "test"
	case "build failure":
		return "build"
	default:
		return "failure"
	}
}

func mergeArtifacts(dst *FailureArtifacts, src FailureArtifacts) {
	dst.Images = uniqueSorted(append(dst.Images, src.Images...))
	dst.Packages = uniqueSorted(append(dst.Packages, src.Packages...))
	dst.Modules = uniqueSorted(append(dst.Modules, src.Modules...))
	dst.URLs = uniqueSorted(append(dst.URLs, src.URLs...))
	dst.Hosts = uniqueSorted(append(dst.Hosts, src.Hosts...))
	dst.Dockerfiles = uniqueSorted(append(dst.Dockerfiles, src.Dockerfiles...))
}

func uniqueSorted(values []string) []string {
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

func excerpt(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, 240)
		}
	}
	return truncate(message, 240)
}

func isPackageNoise(value string) bool {
	switch strings.ToLower(value) {
	case "-", "install", "failed", "failure", "error", "warning", "warn", "err",
		"because", "the", "your", "you", "and", "or", "to", "from", "with", "for",
		"this", "that", "package", "packages", "dependency", "dependencies",
		"resolution", "step", "immutable", "cache", "completed", "done",
		"corepack", "yarn", "npm", "p-prefixed", "peer-requirements", "six-letter":
		return true
	default:
		return false
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
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
	return strings.Trim(builder.String(), "-")
}
