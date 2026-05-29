package research

import (
	"fmt"
	"strings"

	"github.com/autoci-ai/autoci/internal/profile"
)

type Plan struct {
	Workflow          string                `json:"workflow"`
	RunsAnalyzed      int                   `json:"runsAnalyzed"`
	TopRecommendation TopRecommendation     `json:"topRecommendation"`
	Opportunities     []ResearchOpportunity `json:"opportunities"`
}

type TopRecommendation struct {
	Title         string `json:"title"`
	Reason        string `json:"reason"`
	ExpectedValue string `json:"expectedValue"`
}

type ResearchOpportunity struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Hypothesis      string `json:"hypothesis"`
	Evidence        string `json:"evidence"`
	Experiment      string `json:"experiment"`
	SuccessCriteria string `json:"successCriteria"`
	Risk            string `json:"risk"`
	EstimatedImpact string `json:"estimatedImpact"`
}

func FromProfile(workflowName string, runtimeProfile *profile.Profile) Plan {
	plan := Plan{Workflow: workflowName, Opportunities: []ResearchOpportunity{}}
	if len(runtimeProfile.Workflows) > 0 {
		plan.RunsAnalyzed = runtimeProfile.Workflows[0].RunsAnalyzed
	}
	for _, finding := range runtimeProfile.Findings {
		plan.Opportunities = append(plan.Opportunities, opportunityFor(finding))
	}
	plan.TopRecommendation = topRecommendation(plan.Opportunities)
	return plan
}

func opportunityFor(finding profile.Finding) ResearchOpportunity {
	target := finding.Job
	if target == "" {
		target = finding.Workflow
	}
	switch finding.ID {
	case "repeated-failures":
		return ResearchOpportunity{
			ID:              "research-workflow-reliability",
			Title:           "Investigate workflow reliability",
			Hypothesis:      "Workflow failures are concentrated in one or more recurring failure modes.",
			Evidence:        finding.Evidence,
			Experiment:      "Group recent failed runs by failed job and failure signature, then inspect the most common group first.",
			SuccessCriteria: "Workflow failure rate falls below 5% or the dominant failure mode is identified.",
			Risk:            "Low",
			EstimatedImpact: "Reliability improvements are likely to provide more benefit than runtime optimization.",
		}
	case "flaky-job":
		return ResearchOpportunity{
			ID:              "research-flaky-" + slug(target),
			Title:           fmt.Sprintf("Investigate %s instability", target),
			Hypothesis:      "Failures are caused by nondeterministic test ordering, timing, environment setup, or external dependencies.",
			Evidence:        finding.Evidence,
			Experiment:      "Run the job repeatedly in isolation and compare failing runs against passing runs.",
			SuccessCriteria: "Failure rate falls below 2% or the root cause is identified.",
			Risk:            "Low",
			EstimatedImpact: "Improved workflow reliability.",
		}
	case "failure-aggregation-job":
		return ResearchOpportunity{
			ID:              "research-upstream-failures-" + slug(target),
			Title:           fmt.Sprintf("Trace upstream failures behind %s", target),
			Hypothesis:      "This job reports upstream failures rather than failing independently.",
			Evidence:        finding.Evidence,
			Experiment:      "Map failed runs to upstream failed jobs and identify the recurring source jobs.",
			SuccessCriteria: "The upstream job or failure signature responsible for aggregation failures is identified.",
			Risk:            "Low",
			EstimatedImpact: "Prevents wasted effort optimizing or retrying a status aggregation job.",
		}
	case "high-leverage-slow-job":
		return ResearchOpportunity{
			ID:              "research-runtime-" + slug(target),
			Title:           fmt.Sprintf("Reduce runtime for %s", target),
			Hypothesis:      "This job dominates measured runtime because of expensive setup, serial work, or unsharded tests.",
			Evidence:        finding.Evidence,
			Experiment:      "Break down the job into setup, execution, and teardown timing; prototype sharding or cache changes for the largest segment.",
			SuccessCriteria: "Average duration or runtime contribution drops by at least 20% without increasing failure rate.",
			Risk:            "Medium",
			EstimatedImpact: "Shorter feedback loops for the selected workflow.",
		}
	case "long-running-job":
		return ResearchOpportunity{
			ID:              "research-long-running-" + slug(target),
			Title:           fmt.Sprintf("Characterize %s runtime", target),
			Hypothesis:      "This job is long-running, but may not be the highest-leverage optimization target unless it blocks the critical path.",
			Evidence:        finding.Evidence,
			Experiment:      "Measure whether the job gates workflow completion and identify its largest internal time segment.",
			SuccessCriteria: "The team can decide whether to defer optimization or pursue a targeted 15% duration reduction.",
			Risk:            "Low",
			EstimatedImpact: "Improved prioritization of runtime optimization work.",
		}
	case "high-variance":
		return ResearchOpportunity{
			ID:              "research-variance-" + slug(target),
			Title:           fmt.Sprintf("Investigate runtime variance in %s", target),
			Hypothesis:      "Runtime variance is caused by cache misses, external dependencies, queueing, or uneven test distribution.",
			Evidence:        finding.Evidence,
			Experiment:      "Compare fast and slow runs for cache behavior, dependency fetch time, and test distribution.",
			SuccessCriteria: "P95 duration moves within 50% of median duration or the variance source is identified.",
			Risk:            "Low",
			EstimatedImpact: "More predictable CI duration.",
		}
	default:
		return ResearchOpportunity{
			ID:              "research-" + slug(finding.ID+"-"+target),
			Title:           finding.Title,
			Hypothesis:      "The observed finding points to a measurable CI improvement opportunity.",
			Evidence:        finding.Evidence,
			Experiment:      "Inspect the underlying runs and propose a targeted change before executing experiments.",
			SuccessCriteria: "The finding is explained by runtime evidence or deprioritized.",
			Risk:            "Low",
			EstimatedImpact: finding.Recommendation,
		}
	}
}

func topRecommendation(opportunities []ResearchOpportunity) TopRecommendation {
	if len(opportunities) == 0 {
		return TopRecommendation{
			Title:         "No research opportunity identified",
			Reason:        "The sampled execution history did not produce enough evidence-backed findings.",
			ExpectedValue: "Collect more workflow history before planning experiments.",
		}
	}
	top := opportunities[0]
	return TopRecommendation{
		Title:         top.Title,
		Reason:        top.Evidence,
		ExpectedValue: top.EstimatedImpact,
	}
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
