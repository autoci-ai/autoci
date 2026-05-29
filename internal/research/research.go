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
	ID                string   `json:"id"`
	Category          string   `json:"category"`
	Title             string   `json:"title"`
	Hypothesis        string   `json:"hypothesis"`
	Evidence          string   `json:"evidence"`
	Experiment        string   `json:"experiment"`
	SuccessCriteria   string   `json:"successCriteria"`
	Risk              string   `json:"risk"`
	EstimatedImpact   string   `json:"estimatedImpact"`
	SuggestedCommands []string `json:"suggestedCommands"`
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
	return ResearchOpportunity{
		ID:                "reliability-" + strings.TrimPrefix(theme.ID, "failure-theme-"),
		Category:          "Reliability",
		Title:             "Investigate " + failureThemeTitle(theme.Signature),
		Hypothesis:        "A recurring infrastructure or dependency failure theme is causing multiple job failures.",
		Evidence:          failureThemeEvidence(theme),
		Experiment:        experimentForFailureTheme(theme),
		SuccessCriteria:   "The failure theme no longer recurs in recent failed runs or the root cause is identified.",
		Risk:              "Low",
		EstimatedImpact:   "Reduces repeated CI failures caused by the same root cause.",
		SuggestedCommands: suggestedCommands(workflowName),
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

func experimentForFailureTheme(theme failures.FailureTheme) string {
	switch theme.Signature {
	case "image pull failure", "image pull timeout":
		return "Compare affected jobs, image sources, registry behavior, cache behavior, and image pinning strategy."
	case "npm install failure":
		return "Compare dependency install logs across failed runs and inspect registry availability, lockfile changes, and cache behavior."
	default:
		return "Compare affected jobs and failed-run logs to identify the shared root cause."
	}
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
