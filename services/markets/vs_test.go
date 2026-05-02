package markets

import (
	"testing"

	"github.com/vant-xyz/backend-code/models"
)

func TestComputeVSResolution_Mutual(t *testing.T) {
	resolved, out := computeVSResolution(models.VSModeMutual, 2, 0, 2, 0)
	if !resolved || out != string(models.VSOutcomeYes) {
		t.Fatalf("expected mutual YES resolve, got resolved=%v out=%s", resolved, out)
	}

	resolved, out = computeVSResolution(models.VSModeMutual, 2, 0, 1, 1)
	if resolved {
		t.Fatalf("expected unresolved when no unanimity")
	}
}

func TestComputeVSResolution_Consensus(t *testing.T) {
	resolved, out := computeVSResolution(models.VSModeConsensus, 5, 3, 3, 1)
	if !resolved || out != string(models.VSOutcomeYes) {
		t.Fatalf("expected consensus YES resolve, got resolved=%v out=%s", resolved, out)
	}

	resolved, out = computeVSResolution(models.VSModeConsensus, 5, 3, 2, 2)
	if resolved || out != "" {
		t.Fatalf("expected unresolved threshold case")
	}
}
