package research

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/profile"
	"github.com/autoci-ai/autoci/internal/rules"
)

const defaultOpportunityLimit = 3

type Plan struct {
	Workflow          string                `json:"workflow"`
	RunsAnalyzed      int                   `json:"runsAnalyzed"`
	TopRecommendation TopRecommendation     `json:"topRecommendation"`
	Opportunities     []ResearchOpportunity `json:"opportunities"`
	HiddenCount       int                   `json:"hiddenCount"`
}

type TopRecommendation struct {
	Title         string `json:"title"`
	Reason        string `json:"reason"`
	ExpectedValue string `json:"expectedValue"`
	WhyNow        string `json:"whyNow"`
}

type ResearchOpportunity struct {
	ID                  string                `json:"id"`
	Category            string                `json:"category"`
	Title               string                `json:"title"`
	Hypothesis          string                `json:"hypothesis"`
	Evidence            string                `json:"evidence"`
	RawEvidence         RawEvidence           `json:"rawEvidence,omitempty"`
	WhyWeBelieveThis    []string              `json:"whyWeBelieveThis,omitempty"`
	Hypotheses          []RootCauseHypothesis `json:"hypotheses,omitempty"`
	InvestigationSteps  []string              `json:"investigationSteps,omitempty"`
	SupportingArtifacts []SupportingArtifact  `json:"supportingArtifacts,omitempty"`
	Experiment          string                `json:"experiment"`
	SuccessCriteria     string                `json:"successCriteria"`
	Risk                string                `json:"risk"`
	EstimatedImpact     string                `json:"estimatedImpact"`
	SuggestedCommands   []string              `json:"suggestedCommands"`
}

type RawEvidence struct {
	FailureThemeIDs []string `json:"failureThemeIds,omitempty"`
	Workflows       []string `json:"workflows,omitempty"`
	Jobs            []string `json:"jobs,omitempty"`
	Images          []string `json:"images,omitempty"`
	Registries      []string `json:"registries,omitempty"`
	Actions         []string `json:"actions,omitempty"`
	Modules         []string `json:"modules,omitempty"`
	URLs            []string `json:"urls,omitempty"`
	Hosts           []string `json:"hosts,omitempty"`
	LogExcerpts     []string `json:"logExcerpts,omitempty"`
}

type RootCauseHypothesis struct {
	Summary    string   `json:"summary"`
	Confidence int      `json:"confidence"`
	Evidence   []string `json:"evidence,omitempty"`
}

type SupportingArtifact struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type scoredOpportunity struct {
	opportunity ResearchOpportunity
	score       int
}

func FromProfile(workflowName string, runtimeProfile *profile.Profile) Plan {
	return FromProfileWithOptions(workflowName, runtimeProfile, false)
}

func FromProfileWithOptions(workflowName string, runtimeProfile *profile.Profile, verbose bool) Plan {
	plan := Plan{Workflow: workflowName, Opportunities: []ResearchOpportunity{}}
	if runtimeProfile == nil {
		plan.TopRecommendation = topRecommendation(plan.Opportunities)
		return plan
	}
	if len(runtimeProfile.Workflows) > 0 {
		plan.RunsAnalyzed = runtimeProfile.Workflows[0].RunsAnalyzed
	}

	backlog := buildBacklog(workflowName, runtimeProfile.Findings)
	for i := range backlog {
		backlog[i].opportunity = ensureResearchBrief(workflowName, backlog[i].opportunity)
	}
	sort.SliceStable(backlog, func(i, j int) bool {
		return backlog[i].score > backlog[j].score
	})

	limit := defaultOpportunityLimit
	if verbose || len(backlog) < limit {
		limit = len(backlog)
	}
	for _, item := range backlog[:limit] {
		plan.Opportunities = append(plan.Opportunities, item.opportunity)
	}
	if !verbose && len(backlog) > limit {
		plan.HiddenCount = len(backlog) - limit
	}
	plan.TopRecommendation = topRecommendation(plan.Opportunities)
	return plan
}

func FromProfileAndFailures(workflowName string, runtimeProfile *profile.Profile, failureAnalysis *failures.Analysis, verbose bool) Plan {
	return FromSources(workflowName, runtimeProfile, failureAnalysis, nil, verbose)
}

func FromSources(workflowName string, runtimeProfile *profile.Profile, failureAnalysis *failures.Analysis, staticFindings []rules.Finding, verbose bool) Plan {
	plan := FromProfileWithOptions(workflowName, runtimeProfile, true)
	if failureAnalysis != nil {
		for _, theme := range failureAnalysis.FailureThemes {
			plan.Opportunities = append(plan.Opportunities, opportunityFromFailureTheme(workflowName, theme))
		}
	}
	for _, finding := range staticFindings {
		if opportunity, ok := opportunityFromStaticFinding(workflowName, finding); ok {
			plan.Opportunities = append(plan.Opportunities, opportunity)
		}
	}
	opportunities := uniqueOpportunities(plan.Opportunities)
	scored := make([]scoredOpportunity, 0, len(opportunities))
	for _, opportunity := range opportunities {
		opportunity = ensureResearchBrief(workflowName, opportunity)
		scored = append(scored, scoredOpportunity{opportunity: opportunity, score: opportunityScore(opportunity)})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	limit := defaultOpportunityLimit
	if verbose || len(scored) < limit {
		limit = len(scored)
	}
	result := Plan{Workflow: workflowName, RunsAnalyzed: plan.RunsAnalyzed, Opportunities: []ResearchOpportunity{}}
	for _, item := range scored[:limit] {
		result.Opportunities = append(result.Opportunities, item.opportunity)
	}
	if !verbose && len(scored) > limit {
		result.HiddenCount = len(scored) - limit
	}
	result.TopRecommendation = topRecommendation(result.Opportunities)
	return result
}

func uniqueOpportunities(opportunities []ResearchOpportunity) []ResearchOpportunity {
	seen := map[string]bool{}
	var result []ResearchOpportunity
	for _, opportunity := range opportunities {
		if opportunity.ID == "" || seen[opportunity.ID] {
			continue
		}
		seen[opportunity.ID] = true
		result = append(result, opportunity)
	}
	return result
}

func buildBacklog(workflowName string, findings []profile.Finding) []scoredOpportunity {
	var backlog []scoredOpportunity
	var flakyFindings []profile.Finding
	for _, finding := range findings {
		if findingKind(finding.ID) == "flaky-job" {
			flakyFindings = append(flakyFindings, finding)
			continue
		}
		if suppressFinding(finding) {
			continue
		}
		opportunity, score := opportunityFor(workflowName, finding)
		backlog = append(backlog, scoredOpportunity{opportunity: opportunity, score: score})
	}
	if len(flakyFindings) > 0 {
		opportunity, score := groupedFlakyOpportunity(workflowName, flakyFindings)
		backlog = append(backlog, scoredOpportunity{opportunity: opportunity, score: score})
	}
	return backlog
}

func suppressFinding(finding profile.Finding) bool {
	if findingKind(finding.ID) != "long-running-job" {
		return false
	}
	return runtimeContribution(finding.Evidence) < 5
}

func opportunityFor(workflowName string, finding profile.Finding) (ResearchOpportunity, int) {
	target := finding.Job
	if target == "" {
		target = finding.Workflow
	}
	commands := suggestedCommands(workflowName)
	switch findingKind(finding.ID) {
	case "repeated-failures":
		return ResearchOpportunity{
			ID:                "reliability-workflow-failures",
			Category:          "Reliability",
			Title:             "Investigate workflow reliability",
			Hypothesis:        "Workflow failures are concentrated in one or more recurring failure modes.",
			Evidence:          finding.Evidence,
			Experiment:        "Run failure analysis, group recent failed runs by signature, then inspect the most common group first.",
			SuccessCriteria:   "Workflow failure rate falls below 5% or the dominant failure mode is identified.",
			Risk:              "Low",
			EstimatedImpact:   "Reliability improvements are likely to provide more benefit than runtime optimization.",
			SuggestedCommands: commands,
		}, 100
	case "failure-aggregation-job":
		return ResearchOpportunity{
			ID:                "reliability-failure-aggregation",
			Category:          "Reliability",
			Title:             fmt.Sprintf("Trace upstream failures behind %s", target),
			Hypothesis:        "This job reports upstream failures rather than failing independently.",
			Evidence:          finding.Evidence,
			Experiment:        "Map failed runs to upstream failed jobs and identify the recurring source jobs.",
			SuccessCriteria:   "The upstream job or failure signature responsible for aggregation failures is identified.",
			Risk:              "Low",
			EstimatedImpact:   "Prevents wasted effort optimizing or retrying a status aggregation job.",
			SuggestedCommands: commands,
		}, 74
	case "high-leverage-slow-job":
		return ResearchOpportunity{
			ID:                "performance-" + slug(target) + "-runtime",
			Category:          "Performance",
			Title:             fmt.Sprintf("Reduce runtime for %s", target),
			Hypothesis:        "This job dominates measured runtime because of expensive setup, serial work, or unsharded tests.",
			Evidence:          finding.Evidence,
			Experiment:        "Break down the job into setup, execution, and teardown timing; prototype sharding or cache changes for the largest segment.",
			SuccessCriteria:   "Average duration or runtime contribution drops by at least 20% without increasing failure rate.",
			Risk:              "Medium",
			EstimatedImpact:   "Shorter feedback loops for the selected workflow.",
			SuggestedCommands: commands,
		}, 70 + runtimeContribution(finding.Evidence)
	case "long-running-job":
		return ResearchOpportunity{
			ID:                "performance-" + slug(target) + "-critical-path",
			Category:          "Performance",
			Title:             fmt.Sprintf("Characterize %s runtime", target),
			Hypothesis:        "This job is long-running, but may not be the highest-leverage optimization target unless it blocks the critical path.",
			Evidence:          finding.Evidence,
			Experiment:        "Measure whether the job gates workflow completion and identify its largest internal time segment.",
			SuccessCriteria:   "The team can decide whether to defer optimization or pursue a targeted 15% duration reduction.",
			Risk:              "Low",
			EstimatedImpact:   "Improved prioritization of runtime optimization work.",
			SuggestedCommands: commands,
		}, 35 + runtimeContribution(finding.Evidence)
	case "high-variance":
		return ResearchOpportunity{
			ID:                "performance-runtime-variance-" + slug(target),
			Category:          "Performance",
			Title:             fmt.Sprintf("Investigate runtime variance in %s", target),
			Hypothesis:        "Runtime variance is caused by cache misses, external dependencies, queueing, or uneven test distribution.",
			Evidence:          finding.Evidence,
			Experiment:        "Compare fast and slow runs for cache behavior, dependency fetch time, and test distribution.",
			SuccessCriteria:   "P95 duration moves within 50% of median duration or the variance source is identified.",
			Risk:              "Low",
			EstimatedImpact:   "More predictable CI duration.",
			SuggestedCommands: commands,
		}, 55
	default:
		return ResearchOpportunity{
			ID:                "ci-investigation-" + slug(finding.ID+"-"+target),
			Category:          "Workflow",
			Title:             finding.Title,
			Hypothesis:        "The observed finding points to a measurable CI improvement opportunity.",
			Evidence:          finding.Evidence,
			Experiment:        "Inspect the underlying runs and propose a targeted change before executing experiments.",
			SuccessCriteria:   "The finding is explained by runtime evidence or deprioritized.",
			Risk:              "Low",
			EstimatedImpact:   finding.Recommendation,
			SuggestedCommands: commands,
		}, 40
	}
}

func groupedFlakyOpportunity(workflowName string, findings []profile.Finding) (ResearchOpportunity, int) {
	var evidence []string
	score := 110
	for _, finding := range findings {
		name := finding.Job
		if name == "" {
			name = finding.Workflow
		}
		evidence = append(evidence, fmt.Sprintf("%s: %s", name, finding.Evidence))
		score += minInt(8, failureRate(finding.Evidence))
	}
	return ResearchOpportunity{
		ID:                "reliability-recurring-job-instability",
		Category:          "Reliability",
		Title:             "Investigate recurring job instability",
		Hypothesis:        "Multiple job failures may share recurring causes such as nondeterministic tests, timing, environment setup, or external dependencies.",
		Evidence:          strings.Join(evidence, "\n"),
		Experiment:        "Run failure analysis, group failed runs by failure signature, and identify recurring root causes.",
		SuccessCriteria:   "Failure rate for the recurring jobs falls below 2% or the dominant root cause is identified.",
		Risk:              "Low",
		EstimatedImpact:   "Improved workflow reliability and less time spent chasing repeated CI failures.",
		SuggestedCommands: suggestedCommands(workflowName),
	}, score
}

func opportunityFromFailureTheme(workflowName string, theme failures.FailureTheme) ResearchOpportunity {
	raw := rawEvidenceFromFailureTheme(workflowName, theme)
	why := whyForFailureTheme(theme, raw)
	hypotheses := hypothesesForFailureTheme(theme, raw)
	steps := investigationStepsForFailureTheme(theme, raw)
	artifacts := supportingArtifacts(raw)
	return ResearchOpportunity{
		ID:                  "reliability-" + strings.TrimPrefix(theme.ID, "failure-theme-"),
		Category:            "Reliability",
		Title:               "Investigate " + failureThemeTitle(theme.Signature),
		Hypothesis:          primaryHypothesis(hypotheses, "A recurring failure pattern has concrete shared evidence across failed runs."),
		Evidence:            failureThemeEvidence(theme),
		RawEvidence:         raw,
		WhyWeBelieveThis:    why,
		Hypotheses:          hypotheses,
		InvestigationSteps:  steps,
		SupportingArtifacts: artifacts,
		Experiment:          experimentForFailureTheme(theme, raw),
		SuccessCriteria:     "The failure theme no longer recurs in recent failed runs or the root cause is identified.",
		Risk:                "Low",
		EstimatedImpact:     "Reduces repeated CI failures caused by the same root cause.",
		SuggestedCommands:   steps,
	}
}

func failureThemeTitle(signature string) string {
	if strings.HasSuffix(signature, " failure") {
		return strings.TrimSuffix(signature, " failure") + " failures"
	}
	return signature + " failures"
}

func opportunityFromStaticFinding(workflowName string, finding rules.Finding) (ResearchOpportunity, bool) {
	commands := suggestedCommands(workflowName)
	switch finding.ID {
	case "missing-cache", "docker-cache":
		return ResearchOpportunity{
			ID:                "performance-cache-strategy",
			Category:          "Performance",
			Title:             "Improve workflow caching strategy",
			Hypothesis:        "Missing or ineffective caching is increasing workflow duration and variance.",
			Evidence:          finding.Evidence,
			Experiment:        "Add or improve cache configuration, then compare profile results.",
			SuccessCriteria:   "Average duration or runtime variance drops without increasing failure rate.",
			Risk:              "Medium",
			EstimatedImpact:   "Shorter and more predictable workflow runtime.",
			SuggestedCommands: commands,
		}, true
	case "missing-concurrency":
		return ResearchOpportunity{
			ID:                "workflow-concurrency-cancellation",
			Category:          "Workflow",
			Title:             "Add workflow concurrency or cancellation",
			Hypothesis:        "Superseded runs may waste CI capacity and delay useful feedback.",
			Evidence:          finding.Evidence,
			Experiment:        "Add cancellation or concurrency controls and compare queued/running workflow overlap.",
			SuccessCriteria:   "Superseded runs are cancelled without hiding valid failures.",
			Risk:              "Medium",
			EstimatedImpact:   "Reduced wasted CI work and clearer developer feedback.",
			SuggestedCommands: commands,
		}, true
	case "duplicate-install":
		return ResearchOpportunity{
			ID:                "performance-duplicate-dependency-install",
			Category:          "Performance",
			Title:             "Remove duplicate dependency installation",
			Hypothesis:        "Repeated dependency installation is adding avoidable runtime.",
			Evidence:          finding.Evidence,
			Experiment:        "Install dependencies once per job or cache the dependency directory, then compare job duration.",
			SuccessCriteria:   "The affected job duration decreases without changing test coverage.",
			Risk:              "Low",
			EstimatedImpact:   "Reduced repeated setup time.",
			SuggestedCommands: commands,
		}, true
	default:
		return ResearchOpportunity{}, false
	}
}

func failureThemeEvidence(theme failures.FailureTheme) string {
	if len(theme.Jobs) > 1 {
		return fmt.Sprintf("%d occurrences across %d jobs: %s.", theme.Occurrences, len(theme.Jobs), strings.Join(theme.Jobs, ", "))
	}
	if len(theme.Jobs) == 1 {
		return fmt.Sprintf("%d occurrences in %s.", theme.Occurrences, theme.Jobs[0])
	}
	return fmt.Sprintf("%d occurrences.", theme.Occurrences)
}

func experimentForFailureTheme(theme failures.FailureTheme, raw RawEvidence) string {
	switch theme.Signature {
	case "image pull failure", "image pull timeout":
		if len(raw.Images) == 0 && len(raw.Registries) == 0 {
			return "Inspect failed job logs for the affected jobs and compare them with successful runs to identify the exact failing image/container setup step before assigning a registry, tag, auth, or cache root cause."
		}
		return "Compare failed and successful runs for the evidenced image/container setup details and affected jobs before assigning a registry, tag, auth, or cache root cause."
	case "npm install failure":
		if len(raw.Modules) == 0 && len(raw.URLs) == 0 {
			return "Inspect failed dependency install logs for the affected jobs and compare them with successful runs to identify the exact package, dependency constraint, registry URL, or package-manager error before assigning root cause."
		}
		return "Compare dependency install logs across failed and successful runs using only the evidenced packages, registry URLs, and affected jobs as join keys."
	default:
		return "Compare affected jobs and failed-run evidence to identify the shared root cause before changing workflow behavior."
	}
}

func ensureResearchBrief(workflowName string, opportunity ResearchOpportunity) ResearchOpportunity {
	if len(opportunity.RawEvidence.Workflows) == 0 {
		opportunity.RawEvidence.Workflows = []string{workflowName}
	}
	if len(opportunity.RawEvidence.Jobs) == 0 {
		opportunity.RawEvidence.Jobs = jobsFromEvidenceText(opportunity.Evidence)
	}
	if opportunity.RawEvidence.LogExcerpts == nil && opportunity.Evidence != "" {
		opportunity.RawEvidence.LogExcerpts = []string{opportunity.Evidence}
	}
	if len(opportunity.WhyWeBelieveThis) == 0 && opportunity.Evidence != "" {
		opportunity.WhyWeBelieveThis = []string{opportunity.Evidence}
	}
	if len(opportunity.Hypotheses) == 0 {
		opportunity.Hypotheses = []RootCauseHypothesis{{
			Summary:    opportunity.Hypothesis,
			Confidence: confidenceForOpportunity(opportunity),
			Evidence:   compactStrings([]string{opportunity.Evidence}),
		}}
	}
	if len(opportunity.Hypotheses) == 1 {
		opportunity.Hypotheses = append(opportunity.Hypotheses, RootCauseHypothesis{
			Summary:    "The same evidence may instead reflect a correlated CI environment condition rather than a defect in the named job.",
			Confidence: maxInt(20, opportunity.Hypotheses[0].Confidence-25),
			Evidence:   compactStrings([]string{opportunity.Evidence}),
		})
	}
	if len(opportunity.InvestigationSteps) == 0 {
		opportunity.InvestigationSteps = investigationStepsForOpportunity(opportunity)
	}
	if len(opportunity.SupportingArtifacts) == 0 {
		opportunity.SupportingArtifacts = supportingArtifacts(opportunity.RawEvidence)
	}
	opportunity.SuggestedCommands = opportunity.InvestigationSteps
	return opportunity
}

func rawEvidenceFromFailureTheme(workflowName string, theme failures.FailureTheme) RawEvidence {
	raw := RawEvidence{
		FailureThemeIDs: []string{theme.ID},
		Workflows:       []string{workflowName},
		Jobs:            theme.Jobs,
	}
	for _, evidence := range theme.Evidence {
		raw.Images = append(raw.Images, evidence.Image)
		raw.Registries = append(raw.Registries, evidence.Registry, evidence.RegistryHost)
		raw.Modules = append(raw.Modules, evidence.SourceFile, evidence.MissingDependency, evidence.PackageName)
		raw.URLs = append(raw.URLs, evidence.RegistryURL)
		raw.Hosts = append(raw.Hosts, evidence.RegistryHost)
		raw.LogExcerpts = append(raw.LogExcerpts, evidence.LogExcerpt, evidence.PullError, evidence.InstallError, evidence.DependencyConflict, evidence.CompilerError, evidence.AssertionMessage)
	}
	raw.Images = append(raw.Images, theme.Artifacts.Images...)
	raw.Modules = append(raw.Modules, theme.Artifacts.Modules...)
	raw.Modules = append(raw.Modules, theme.Artifacts.Packages...)
	raw.URLs = append(raw.URLs, theme.Artifacts.URLs...)
	raw.Hosts = append(raw.Hosts, theme.Artifacts.Hosts...)
	raw.Actions = actionsFromArtifacts(theme)
	raw.FailureThemeIDs = uniqueSorted(raw.FailureThemeIDs)
	raw.Workflows = uniqueSorted(raw.Workflows)
	raw.Jobs = uniqueSorted(raw.Jobs)
	raw.Images = uniqueSorted(raw.Images)
	raw.Registries = uniqueSorted(raw.Registries)
	raw.Actions = uniqueSorted(raw.Actions)
	raw.Modules = uniqueSorted(raw.Modules)
	if theme.Signature == "npm install failure" {
		raw.Modules = cleanResearchPackages(raw.Modules)
	}
	raw.URLs = uniqueSorted(raw.URLs)
	raw.Hosts = uniqueSorted(raw.Hosts)
	raw.LogExcerpts = uniqueSorted(raw.LogExcerpts)
	return raw
}

func whyForFailureTheme(theme failures.FailureTheme, raw RawEvidence) []string {
	var reasons []string
	reasons = append(reasons, fmt.Sprintf("%s appears %d times under stable ID %s.", theme.Signature, theme.Occurrences, theme.ID))
	if len(raw.Jobs) > 0 {
		reasons = append(reasons, fmt.Sprintf("Affected jobs: %s.", strings.Join(raw.Jobs, ", ")))
	}
	if len(raw.Images) > 0 {
		reasons = append(reasons, fmt.Sprintf("Failure evidence names container image(s): %s.", strings.Join(raw.Images, ", ")))
	}
	if len(raw.Registries) > 0 {
		reasons = append(reasons, fmt.Sprintf("Pull evidence points at registry host(s): %s.", strings.Join(raw.Registries, ", ")))
	}
	if len(raw.Modules) > 0 {
		if theme.Signature == "npm install failure" {
			reasons = append(reasons, fmt.Sprintf("Referenced packages: %s.", strings.Join(raw.Modules, ", ")))
			return append(reasons, whyURLsAndLogs(theme.Signature, raw)...)
		}
		reasons = append(reasons, fmt.Sprintf("Recurring module/package/source evidence: %s.", strings.Join(raw.Modules, ", ")))
	}
	reasons = append(reasons, whyURLsAndLogs(theme.Signature, raw)...)
	return reasons
}

func whyURLsAndLogs(signature string, raw RawEvidence) []string {
	var reasons []string
	if len(raw.URLs) > 0 {
		reasons = append(reasons, fmt.Sprintf("Relevant URL evidence: %s.", strings.Join(raw.URLs, ", ")))
	}
	logs := representativeLogs(signature, raw)
	if len(logs) > 0 {
		reasons = append(reasons, fmt.Sprintf("Representative log evidence: %s.", strings.Join(limitStrings(logs, 3), " | ")))
	} else if signature == "npm install failure" {
		reasons = append(reasons, "Representative log evidence is thin; cached lines do not include a strong npm/yarn failure diagnostic.")
	}
	return reasons
}

func hypothesesForFailureTheme(theme failures.FailureTheme, raw RawEvidence) []RootCauseHypothesis {
	switch theme.Signature {
	case "image pull failure", "image pull timeout":
		return imagePullHypotheses(raw)
	case "npm install failure":
		return npmHypotheses(raw)
	case "test failure", "jest timeout":
		return testHypotheses(raw)
	case "build failure":
		return buildHypotheses(raw)
	default:
		return []RootCauseHypothesis{
			{Summary: "The affected jobs share a recurring external dependency or environment condition.", Confidence: 55, Evidence: failureEvidenceSnippets(raw)},
			{Summary: "The failure signature groups unrelated symptoms that need a narrower signature.", Confidence: 35, Evidence: failureEvidenceSnippets(raw)},
		}
	}
}

func imagePullHypotheses(raw RawEvidence) []RootCauseHypothesis {
	jobs := joinOrFallback(raw.Jobs, "the affected jobs")
	if len(raw.Images) == 0 && len(raw.Registries) == 0 && !hasImageRootCauseEvidence(raw) {
		return []RootCauseHypothesis{
			{Summary: fmt.Sprintf("Recurring image/container setup failures are affecting %s. The current evidence does not include the exact failed image or registry, so the next step is to inspect the failed job logs for pull errors, registry host, and image reference.", jobs), Confidence: 45, Evidence: failureEvidenceSnippets(raw)},
			{Summary: "The failure grouping may be correct, but the cached evidence is too thin to distinguish registry access, image tag, authentication, or job setup failures.", Confidence: 35, Evidence: failureEvidenceSnippets(raw)},
		}
	}
	var hypotheses []RootCauseHypothesis
	images := joinOrFallback(raw.Images, "the referenced images")
	registries := joinOrFallback(raw.Registries, joinOrFallback(raw.Hosts, "the registry"))
	if hasAnyEvidenceToken(raw, []string{"toomanyrequests", "rate limit"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Pulls for %s may be rate-limited by %s.", images, registries), Confidence: 75, Evidence: matchingEvidence(raw, []string{"toomanyrequests", "rate limit"})})
	}
	if hasAnyEvidenceToken(raw, []string{"i/o timeout", "timeout", "connection reset", "connection refused"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Pulls for %s may be failing because %s is intermittently unreachable from CI.", images, registries), Confidence: 70, Evidence: matchingEvidence(raw, []string{"i/o timeout", "timeout", "connection reset", "connection refused"})})
	}
	if hasAnyEvidenceToken(raw, []string{"manifest unknown"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("%s may reference a missing or mutable tag that is producing manifest lookup failures.", images), Confidence: 72, Evidence: matchingEvidence(raw, []string{"manifest unknown"})})
	}
	if hasAnyEvidenceToken(raw, []string{"pull access denied"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Pulls for %s may be failing because registry authentication or image permissions are missing.", images), Confidence: 72, Evidence: matchingEvidence(raw, []string{"pull access denied"})})
	}
	if hasAnyEvidenceToken(raw, []string{"docker.io", "ghcr.io"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("The affected jobs depend on public registry pulls for %s via %s.", images, registries), Confidence: 55, Evidence: append(raw.Images, raw.Registries...)})
	}
	if hasAnyEvidenceToken(raw, []string{"image:", "creating container for image"}) && len(hypotheses) == 0 {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("The affected jobs reference %s during image/container setup, but the cached logs do not yet prove the root cause.", images), Confidence: 50, Evidence: append(raw.Images, failureEvidenceSnippets(raw)...)})
	}
	if len(hypotheses) == 0 {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Recurring image/container setup failures are affecting %s; inspect failed logs for the exact pull error, registry host, and image reference.", jobs), Confidence: 45, Evidence: failureEvidenceSnippets(raw)})
	}
	return limitHypotheses(hypotheses, 5)
}

func npmHypotheses(raw RawEvidence) []RootCauseHypothesis {
	packages := npmPackagePhrase(raw)
	registry := joinOrFallback(raw.URLs, joinOrFallback(raw.Hosts, "the package registry"))
	jobs := joinOrFallback(raw.Jobs, "the affected jobs")
	if !hasNPMRootCauseEvidence(raw) {
		return []RootCauseHypothesis{
			{Summary: cautiousNPMHypothesis(jobs), Confidence: 45, Evidence: npmEvidenceSnippets(raw)},
			{Summary: "The failure grouping is likely useful, but the cached evidence is too thin to distinguish lockfile, registry access, package manager, or cache failures.", Confidence: 35, Evidence: failureEvidenceSnippets(raw)},
		}
	}
	var hypotheses []RootCauseHypothesis
	if hasNPMResolutionEvidence(raw) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Dependency resolution is failing for %s with explicit resolver or peer-dependency conflict evidence.", packages), Confidence: 78, Evidence: matchingEvidence(raw, npmResolutionEvidenceTokens())})
	}
	if len(raw.URLs) > 0 && hasAnyEvidenceToken(raw, []string{"econnreset", "etimedout", "timeout", "eai_again", "enotfound"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("Install failures may correlate with access to %s.", registry), Confidence: 68, Evidence: append(raw.URLs, failureEvidenceSnippets(raw)...)})
	}
	if len(raw.Modules) > 0 && hasAnyEvidenceToken(raw, []string{"lockfile", "immutable", "would have been modified"}) {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: fmt.Sprintf("The affected job may be using lockfile state that disagrees with %s.", packages), Confidence: 62, Evidence: append(raw.Modules, matchingEvidence(raw, []string{"lockfile", "immutable", "would have been modified"})...)})
	}
	if len(hypotheses) == 0 {
		hypotheses = append(hypotheses, RootCauseHypothesis{Summary: cautiousNPMHypothesis(jobs), Confidence: 45, Evidence: npmEvidenceSnippets(raw)})
	}
	return limitHypotheses(hypotheses, 5)
}

func testHypotheses(raw RawEvidence) []RootCauseHypothesis {
	modules := joinOrFallback(raw.Modules, "the failing test/module")
	return []RootCauseHypothesis{
		{Summary: fmt.Sprintf("%s has a deterministic assertion failure introduced by recent code or dependency changes.", modules), Confidence: confidenceWithEvidence(72, matchingEvidence(raw, []string{"assert", "expected", "received"})), Evidence: failureEvidenceSnippets(raw)},
		{Summary: fmt.Sprintf("%s is flaky due to timing, shared state, or external service behavior.", modules), Confidence: 52, Evidence: raw.LogExcerpts},
	}
}

func buildHypotheses(raw RawEvidence) []RootCauseHypothesis {
	modules := joinOrFallback(raw.Modules, "the affected source/module")
	return []RootCauseHypothesis{
		{Summary: fmt.Sprintf("%s is failing compilation because a symbol, package, or generated artifact is missing.", modules), Confidence: confidenceWithEvidence(75, matchingEvidence(raw, []string{"undefined", "cannot find", "missing", "module not found"})), Evidence: failureEvidenceSnippets(raw)},
		{Summary: "The build target is running with different dependency or generation steps than the successful path.", Confidence: 55, Evidence: append(raw.Modules, raw.LogExcerpts...)},
	}
}

func investigationStepsForFailureTheme(theme failures.FailureTheme, raw RawEvidence) []string {
	switch theme.Signature {
	case "image pull failure", "image pull timeout":
		return imagePullInvestigationSteps(raw)
	case "npm install failure":
		return npmInvestigationSteps(raw)
	case "test failure", "jest timeout":
		return testInvestigationSteps(raw)
	case "build failure":
		return buildInvestigationSteps(raw)
	default:
		return investigationStepsForRaw(raw)
	}
}

func imagePullInvestigationSteps(raw RawEvidence) []string {
	var steps []string
	if len(raw.Images) == 0 && len(raw.Registries) == 0 {
		steps = append(steps, fmt.Sprintf("Inspect failed job logs for %s to identify the exact failing image/container setup step.", joinOrFallback(raw.Jobs, "the affected jobs")))
		steps = append(steps, "Compare those failed logs with successful runs before assigning a registry, tag, auth, or cache root cause.")
		return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
	}
	for _, image := range raw.Images {
		steps = append(steps, fmt.Sprintf("Check failed pulls for %s in jobs %s.", image, joinOrFallback(raw.Jobs, "from the cached failure evidence")))
	}
	for _, registry := range raw.Registries {
		steps = append(steps, fmt.Sprintf("Compare failed runs against successful runs to see whether failures correlate with %s access.", registry))
	}
	for _, image := range raw.Images {
		steps = append(steps, fmt.Sprintf("Evaluate mirroring or pinning %s to an internal registry or immutable digest.", image))
	}
	return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
}

func npmInvestigationSteps(raw RawEvidence) []string {
	var steps []string
	if len(raw.Modules) == 0 && len(raw.URLs) == 0 {
		steps = append(steps, fmt.Sprintf("Inspect failed dependency install logs for %s to identify the exact package, dependency constraint, registry URL, or package-manager error.", joinOrFallback(raw.Jobs, "the affected jobs")))
		steps = append(steps, "Compare those failed install logs with successful runs before assigning a lockfile, package, registry, or cache root cause.")
		return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
	}
	for _, module := range raw.Modules {
		steps = append(steps, fmt.Sprintf("Inspect dependency resolution for %s in affected jobs %s.", module, joinOrFallback(raw.Jobs, "from the cached failure evidence")))
	}
	for _, url := range raw.URLs {
		steps = append(steps, fmt.Sprintf("Check whether failed installs correlate with registry access to %s.", url))
	}
	if len(raw.Modules) > 0 && hasAnyEvidenceToken(raw, []string{"lockfile", "immutable", "would have been modified"}) {
		steps = append(steps, "Compare package lockfile and dependency cache state between failed and successful runs for the affected jobs.")
	} else if len(raw.Modules) > 0 {
		steps = append(steps, "Compare dependency install logs between failed and successful runs for the evidenced packages.")
	}
	return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
}

func npmPackagePhrase(raw RawEvidence) string {
	if len(raw.Modules) == 0 {
		return "the dependency install"
	}
	return "referenced packages " + strings.Join(raw.Modules, ", ")
}

func hasImageRootCauseEvidence(raw RawEvidence) bool {
	return hasAnyEvidenceToken(raw, []string{"toomanyrequests", "rate limit", "i/o timeout", "connection reset", "manifest unknown", "pull access denied", "docker.io", "ghcr.io", "image:", "creating container for image"})
}

func hasNPMRootCauseEvidence(raw RawEvidence) bool {
	return hasNPMResolutionEvidence(raw) || hasAnyEvidenceToken(raw, []string{"lockfile would have been modified", "immutable install", "econnreset", "etimedout", "timeout", "eai_again", "enotfound"})
}

func hasNPMResolutionEvidence(raw RawEvidence) bool {
	return hasAnyEvidenceToken(raw, npmResolutionEvidenceTokens()) || hasYN0086PeerContext(raw)
}

func npmResolutionEvidenceTokens() []string {
	return []string{"yn0002", "yn0060", "peer requirements", "lockfile would have been modified", "immutable install", "doesn’t provide", "doesn't provide", "incorrectly met", "resolution step", "post-resolution validation"}
}

func hasYN0086PeerContext(raw RawEvidence) bool {
	for _, line := range raw.LogExcerpts {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "yn0086") && strings.Contains(lower, "peer") {
			return true
		}
	}
	return false
}

func hasAnyEvidenceToken(raw RawEvidence, tokens []string) bool {
	var evidence []string
	evidence = append(evidence, raw.LogExcerpts...)
	evidence = append(evidence, raw.Images...)
	evidence = append(evidence, raw.Registries...)
	evidence = append(evidence, raw.URLs...)
	evidence = append(evidence, raw.Hosts...)
	joined := strings.ToLower(strings.Join(evidence, "\n"))
	for _, token := range tokens {
		if strings.Contains(joined, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func limitHypotheses(values []RootCauseHypothesis, limit int) []RootCauseHypothesis {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func cautiousNPMHypothesis(jobs string) string {
	return fmt.Sprintf("Frontend dependency installation is failing in %s. Current evidence points to package manager/bootstrap or external download activity, but does not yet identify whether the root cause is dependency resolution, registry/network access, binary download failure, cache state, or lockfile drift.", jobs)
}

func representativeLogs(signature string, raw RawEvidence) []string {
	if signature != "npm install failure" {
		return raw.LogExcerpts
	}
	return npmEvidenceSnippets(raw)
}

func npmEvidenceSnippets(raw RawEvidence) []string {
	var strong []string
	for _, line := range raw.LogExcerpts {
		if isNPMRepresentativeLine(line) {
			strong = append(strong, line)
		}
	}
	return limitStrings(uniqueSorted(strong), 4)
}

func isNPMRepresentativeLine(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "passed") || strings.Contains(lower, "successfully") || strings.Contains(lower, "tests passed") {
		return false
	}
	for _, token := range []string{"yarn", "corepack", "npm", "snyk", "download", "error", "fail", "yn000", "yn001", "yn002", "yn003", "yn004", "yn005", "yn006", "yn007", "yn008", "yn009"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func cleanResearchPackages(values []string) []string {
	var result []string
	for _, value := range values {
		if cleaned := cleanResearchPackage(value); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return uniqueSorted(result)
}

func cleanResearchPackage(value string) string {
	value = strings.TrimSpace(value)
	value = stripResearchANSISuffix(value)
	if value == "" || strings.HasPrefix(value, "--") || strings.Contains(value, "\x1b") {
		return ""
	}
	lower := strings.ToLower(value)
	switch lower {
	case "-", "0m", "because", "the", "your", "you", "and", "or", "to", "from", "with", "for", "install", "failed", "failure", "error",
		"corepack", "yarn", "npm", "p-prefixed", "peer-requirements", "six-letter":
		return ""
	}
	if regexp.MustCompile(`^\d{1,4}[:/-]\d`).MatchString(value) || regexp.MustCompile(`^[0-9.]+[ms]?$`).MatchString(value) {
		return ""
	}
	if strings.HasPrefix(value, "@") {
		parts := strings.Split(value, "/")
		if len(parts) == 2 && parts[0] != "@" && parts[1] != "" {
			return value
		}
		return ""
	}
	if strings.Contains(value, "/") {
		return ""
	}
	switch lower {
	case "react", "typescript", "eslint", "webpack", "vite", "jest", "next", "snyk", "pnpm", "lodash",
		"core-js", "pact-core", "unrs-resolver", "msw", "protobufjs", "esbuild":
		return value
	default:
		return ""
	}
}

func stripResearchANSISuffix(value string) string {
	return regexp.MustCompile(`^\d{1,3}m([A-Za-z@][A-Za-z0-9._/-]*)$`).ReplaceAllString(value, "$1")
}

func testInvestigationSteps(raw RawEvidence) []string {
	var steps []string
	for _, module := range raw.Modules {
		steps = append(steps, fmt.Sprintf("Compare failed and successful logs for %s to separate deterministic assertions from timing or shared-state failures.", module))
	}
	return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
}

func buildInvestigationSteps(raw RawEvidence) []string {
	var steps []string
	for _, module := range raw.Modules {
		steps = append(steps, fmt.Sprintf("Trace build inputs for %s in the affected jobs and verify generated files or dependency steps ran before compilation.", module))
	}
	return limitStrings(uniqueSorted(append(steps, investigationStepsForRaw(raw)...)), 5)
}

func investigationStepsForOpportunity(opportunity ResearchOpportunity) []string {
	raw := opportunity.RawEvidence
	if len(raw.Jobs) > 0 {
		return []string{fmt.Sprintf("Compare recent runs for %s in workflow %s using this evidence: %s", strings.Join(raw.Jobs, ", "), joinOrFallback(raw.Workflows, "the selected workflow"), opportunity.Evidence)}
	}
	return []string{fmt.Sprintf("Inspect the cached evidence for %s and compare failed versus successful runs before choosing a workflow change.", opportunity.ID)}
}

func investigationStepsForRaw(raw RawEvidence) []string {
	var steps []string
	if len(raw.Jobs) > 0 {
		steps = append(steps, fmt.Sprintf("Use affected jobs as the first filter: %s.", strings.Join(raw.Jobs, ", ")))
	}
	if len(raw.FailureThemeIDs) > 0 {
		steps = append(steps, fmt.Sprintf("Anchor the investigation on cached failure theme ID(s): %s.", strings.Join(raw.FailureThemeIDs, ", ")))
	}
	return steps
}

func supportingArtifacts(raw RawEvidence) []SupportingArtifact {
	var artifacts []SupportingArtifact
	for _, value := range raw.FailureThemeIDs {
		artifacts = append(artifacts, SupportingArtifact{Type: "failureThemeId", Value: value})
	}
	for _, value := range raw.Jobs {
		artifacts = append(artifacts, SupportingArtifact{Type: "job", Value: value})
	}
	for _, value := range raw.Workflows {
		artifacts = append(artifacts, SupportingArtifact{Type: "workflow", Value: value})
	}
	for _, value := range raw.Images {
		artifacts = append(artifacts, SupportingArtifact{Type: "image", Value: value})
	}
	for _, value := range raw.Registries {
		artifacts = append(artifacts, SupportingArtifact{Type: "registry", Value: value})
	}
	for _, value := range raw.Actions {
		artifacts = append(artifacts, SupportingArtifact{Type: "action", Value: value})
	}
	for _, value := range raw.Modules {
		artifacts = append(artifacts, SupportingArtifact{Type: "module", Value: value})
	}
	for _, value := range raw.URLs {
		artifacts = append(artifacts, SupportingArtifact{Type: "url", Value: value})
	}
	for _, value := range raw.Hosts {
		artifacts = append(artifacts, SupportingArtifact{Type: "host", Value: value})
	}
	return artifacts
}

func primaryHypothesis(hypotheses []RootCauseHypothesis, fallback string) string {
	if len(hypotheses) == 0 || hypotheses[0].Summary == "" {
		return fallback
	}
	return hypotheses[0].Summary
}

func confidenceForOpportunity(opportunity ResearchOpportunity) int {
	switch {
	case strings.HasPrefix(opportunity.ID, "reliability-"):
		return 65
	case strings.HasPrefix(opportunity.ID, "performance-"):
		return 60
	default:
		return 50
	}
}

func confidenceWithEvidence(base int, evidence []string) int {
	if len(compactStrings(evidence)) == 0 {
		return base - 20
	}
	return base
}

func failureEvidenceSnippets(raw RawEvidence) []string {
	return limitStrings(raw.LogExcerpts, 4)
}

func matchingEvidence(raw RawEvidence, terms []string) []string {
	var result []string
	for _, value := range raw.LogExcerpts {
		lower := strings.ToLower(value)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				result = append(result, value)
				break
			}
		}
	}
	return limitStrings(uniqueSorted(result), 4)
}

func actionsFromArtifacts(theme failures.FailureTheme) []string {
	var result []string
	for _, image := range theme.Artifacts.Images {
		if strings.Contains(image, "action") || strings.Contains(image, "actions/") {
			result = append(result, image)
		}
	}
	return result
}

func jobsFromEvidenceText(evidence string) []string {
	if evidence == "" || !strings.Contains(evidence, ":") {
		return nil
	}
	prefix := strings.SplitN(evidence, ":", 2)[0]
	if strings.Contains(prefix, "occurrence") {
		return nil
	}
	return compactStrings([]string{strings.TrimSpace(prefix)})
}

func joinOrFallback(values []string, fallback string) string {
	values = compactStrings(values)
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, ", ")
}

func limitStrings(values []string, limit int) []string {
	values = compactStrings(values)
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func compactStrings(values []string) []string {
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return uniqueSorted(result)
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

func opportunityScore(opportunity ResearchOpportunity) int {
	switch {
	case strings.HasPrefix(opportunity.ID, "reliability-image-pull"):
		return 130
	case strings.HasPrefix(opportunity.ID, "reliability-npm-install"):
		return 120
	case opportunity.ID == "reliability-recurring-job-instability":
		return 110
	case opportunity.ID == "reliability-workflow-failures":
		return 100
	case strings.HasPrefix(opportunity.ID, "performance-runtime-variance"):
		return 75 + runtimeContribution(opportunity.Evidence)
	case strings.HasPrefix(opportunity.ID, "performance-"):
		return 80 + runtimeContribution(opportunity.Evidence)
	case strings.HasPrefix(opportunity.ID, "workflow-"):
		return 60
	default:
		return 50
	}
}

func topRecommendation(opportunities []ResearchOpportunity) TopRecommendation {
	if len(opportunities) == 0 {
		return TopRecommendation{
			Title:         "No research opportunity identified",
			Reason:        "The sampled execution history did not produce enough evidence-backed findings.",
			ExpectedValue: "Collect more workflow history before planning experiments.",
			WhyNow:        "There is not enough signal yet to choose a high-value investigation.",
		}
	}
	top := opportunities[0]
	return TopRecommendation{
		Title:         top.Title,
		Reason:        top.Evidence,
		ExpectedValue: top.EstimatedImpact,
		WhyNow:        whyNow(top),
	}
}

func whyNow(opportunity ResearchOpportunity) string {
	switch {
	case opportunity.ID == "reliability-workflow-failures":
		return "Workflow-level failures affect every developer waiting on this CI path."
	case opportunity.ID == "reliability-recurring-job-instability":
		return "Multiple flaky jobs are contributing to workflow failures."
	case strings.HasPrefix(opportunity.ID, "performance-runtime-variance"):
		return "High variance makes CI duration unpredictable and can hide cache or dependency problems."
	case strings.HasPrefix(opportunity.ID, "performance-"):
		return "The job consumes a large share of measured runtime, so improvements should be visible in workflow duration."
	default:
		return "This is the highest-scoring opportunity in the current evidence set."
	}
}

func suggestedCommands(workflowName string) []string {
	return []string{
		fmt.Sprintf("autoci profile --workflow %s", workflowName),
		fmt.Sprintf("autoci failures --workflow %s", workflowName),
		fmt.Sprintf("autoci research --workflow %s --verbose", workflowName),
		"depot ci workflow list --output json",
		"depot ci workflow show <workflow-id> --output json",
	}
}

func findingKind(id string) string {
	for _, kind := range []string{
		"repeated-failures",
		"flaky-job",
		"failure-aggregation-job",
		"high-leverage-slow-job",
		"long-running-job",
		"high-variance",
	} {
		if id == kind || strings.HasPrefix(id, kind+"-") {
			return kind
		}
	}
	return id
}

var percentPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%`)

func runtimeContribution(evidence string) int {
	matches := percentPattern.FindAllStringSubmatch(evidence, -1)
	if len(matches) == 0 {
		return 0
	}
	// Contribution is the last percentage in current finding evidence.
	value, _ := strconv.ParseFloat(matches[len(matches)-1][1], 64)
	return int(value)
}

func failureRate(evidence string) int {
	match := percentPattern.FindStringSubmatch(evidence)
	if len(match) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(match[1], 64)
	return int(value)
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
