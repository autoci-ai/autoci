package research

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/autoci-ai/autoci/internal/failures"
	"github.com/autoci-ai/autoci/internal/profile"
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
	plan := FromProfileWithOptions(workflowName, runtimeProfile, true)
	if failureAnalysis != nil {
		for _, theme := range failureAnalysis.FailureThemes {
			plan.Opportunities = append(plan.Opportunities, opportunityFromFailureTheme(workflowName, theme))
		}
	}
	scored := make([]scoredOpportunity, 0, len(plan.Opportunities))
	for _, opportunity := range plan.Opportunities {
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
			ID:                "workflow-reliability",
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
			ID:                "failure-aggregation",
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
			ID:                "high-leverage-runtime-" + slug(target),
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
			ID:                "runtime-characterization-" + slug(target),
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
			ID:                "runtime-variance-" + slug(target),
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
		ID:                "job-instability",
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
		ID:                strings.TrimPrefix(theme.ID, "failure-theme-"),
		Title:             "Investigate " + theme.Signature + " failures",
		Hypothesis:        "A recurring infrastructure or dependency failure theme is causing multiple job failures.",
		Evidence:          failureThemeEvidence(theme),
		Experiment:        experimentForFailureTheme(theme),
		SuccessCriteria:   "The failure theme no longer recurs in recent failed runs or the root cause is identified.",
		Risk:              "Low",
		EstimatedImpact:   "Reduces repeated CI failures caused by the same root cause.",
		SuggestedCommands: suggestedCommands(workflowName),
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
	case strings.HasPrefix(opportunity.ID, "image-pull"):
		return 130
	case strings.HasPrefix(opportunity.ID, "npm-install"):
		return 120
	case opportunity.ID == "job-instability":
		return 110
	case opportunity.ID == "workflow-reliability":
		return 100
	case strings.HasPrefix(opportunity.ID, "high-leverage-runtime"):
		return 80
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
	case opportunity.ID == "workflow-reliability":
		return "Workflow-level failures affect every developer waiting on this CI path."
	case opportunity.ID == "job-instability":
		return "Multiple flaky jobs are contributing to workflow failures."
	case strings.HasPrefix(opportunity.ID, "high-leverage-runtime"):
		return "The job consumes a large share of measured runtime, so improvements should be visible in workflow duration."
	case strings.HasPrefix(opportunity.ID, "runtime-variance"):
		return "High variance makes CI duration unpredictable and can hide cache or dependency problems."
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
