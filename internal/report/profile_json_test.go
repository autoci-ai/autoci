package report

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/autoci-ai/autoci/internal/profile"
)

func TestWriteProfileJSON(t *testing.T) {
	var out bytes.Buffer
	err := WriteProfileJSON(&out, "pr.yml", &profile.Profile{
		Workflows: []profile.WorkflowProfile{{
			RunsAnalyzed: 10,
			FailureRate:  0.23,
			AvgDuration:  530 * time.Second,
			P95Duration:  800 * time.Second,
		}},
		Findings: []profile.Finding{{
			ID:             "repeated-failures",
			Severity:       "high",
			Title:          "Repeated workflow failures",
			Evidence:       "Failure rate 23% across 10 runs.",
			Recommendation: "Stabilize recurring workflow failures before optimizing speed.",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["workflow"] != "pr.yml" {
		t.Fatalf("workflow = %v", decoded["workflow"])
	}
	if decoded["runsAnalyzed"].(float64) != 10 {
		t.Fatalf("runsAnalyzed = %v", decoded["runsAnalyzed"])
	}
	findings := decoded["findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("expected one finding")
	}
	if findings[0].(map[string]any)["score"].(float64) <= 0 {
		t.Fatalf("expected positive score")
	}
}
