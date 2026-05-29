package lifecycle

import "fmt"

type Readiness string

const (
	ReadinessNotReady          Readiness = "not_ready"
	ReadinessNeedsMoreEvidence Readiness = "needs_more_evidence"
	ReadinessReadyForFix       Readiness = "ready_for_fix"
	ReadinessFixed             Readiness = "fixed"
	ReadinessValidated         Readiness = "validated"
)

func (readiness Readiness) Valid() bool {
	switch readiness {
	case ReadinessNotReady, ReadinessNeedsMoreEvidence, ReadinessReadyForFix, ReadinessFixed, ReadinessValidated:
		return true
	default:
		return false
	}
}

func ParseReadiness(value string) (Readiness, error) {
	readiness := Readiness(value)
	if readiness.Valid() {
		return readiness, nil
	}
	return "", fmt.Errorf("invalid readiness %q", value)
}
