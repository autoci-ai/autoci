package fix

import (
	"strings"
	"testing"
)

func TestApplyImagePullFixAddsDockerTimeoutEnv(t *testing.T) {
	updated, err := applyWorkflowFix([]byte("jobs: {}\n"), "failure-theme-image-pull-failure")
	if err != nil {
		t.Fatal(err)
	}
	output := string(updated)
	if !strings.Contains(output, "DOCKER_CLIENT_TIMEOUT") || !strings.Contains(output, "COMPOSE_HTTP_TIMEOUT") {
		t.Fatalf("expected docker timeout env, got:\n%s", output)
	}
}

func TestApplyNPMInstallFixWrapsInstallCommand(t *testing.T) {
	updated, err := applyWorkflowFix([]byte("jobs:\n  test:\n    steps:\n      - run: npm ci\n"), "failure-theme-npm-install-failure")
	if err != nil {
		t.Fatal(err)
	}
	output := string(updated)
	if !strings.Contains(output, "autoci_retry npm ci") {
		t.Fatalf("expected retry wrapper, got:\n%s", output)
	}
}

func TestNormalizeResearchOpportunityID(t *testing.T) {
	if got := normalizeOpportunity("research-image-pull-failure"); got != "failure-theme-image-pull-failure" {
		t.Fatalf("normalized id = %q", got)
	}
	if got := normalizeOpportunity("reliability-image-pull-failure"); got != "failure-theme-image-pull-failure" {
		t.Fatalf("normalized id = %q", got)
	}
}
