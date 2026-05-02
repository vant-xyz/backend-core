package markets

import (
	"encoding/binary"
	"testing"
	"time"

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

func TestBuildVSEventData_DiscriminatorAndLayout(t *testing.T) {
	input := CreateVSEventInput{
		Title:              "VS test",
		Mode:               models.VSModeConsensus,
		Threshold:          3,
		StakeAmount:        5.0,
		ParticipantTarget:  5,
		JoinDeadlineUTC:    time.Unix(2000, 0).UTC(),
		ResolveDeadlineUTC: time.Unix(2600, 0).UTC(),
	}
	data := buildVSEventData("VS_abc123", input)
	if len(data) == 0 {
		t.Fatalf("expected non-empty payload")
	}
	if data[0] != discriminatorCreateVS {
		t.Fatalf("unexpected discriminator: got=%d want=%d", data[0], discriminatorCreateVS)
	}
}

func TestWriteI32_LittleEndian(t *testing.T) {
	buf := make([]byte, 4)
	off := 0
	writeI32(buf, &off, 0x01020304)
	if off != 4 {
		t.Fatalf("expected offset 4, got %d", off)
	}
	got := int32(binary.LittleEndian.Uint32(buf))
	if got != 0x01020304 {
		t.Fatalf("unexpected value: got=%x", got)
	}
}
