package shared

import (
	"testing"
	"testing/quick"
)

func TestPlanResidencyShadowVersionsAndBoundaries(t *testing.T) {
	events := []LocalEvent{
		{Local: 0, Kind: LocalEventRead},
		{Local: 0, Kind: LocalEventRead},
		{Local: 0, Kind: LocalEventDefine},
		{Local: 0, Kind: LocalEventRead},
		{Local: 0, Kind: LocalEventRead},
		{Local: NoLocal, Kind: LocalEventCall},
	}
	got := PlanResidencyShadow(events, 1, 1, false)
	if got.Candidates != 1 || got.Versions != 2 || got.Segments != 2 || got.Profitable != 1 {
		t.Fatalf("summary = %+v", got)
	}
	if got.LoadsAvoided != 3 || got.SyncDebt != 2 {
		t.Fatalf("physical estimate = %+v", got)
	}
	if got.Admissions|got.Evictions|got.Reloads|got.Writebacks != 0 {
		t.Fatalf("ordinary shadow unexpectedly ran transition simulation: %+v", got)
	}
}

func TestPlanResidencyTransitionShadowModelsHystereticDirtyReplacement(t *testing.T) {
	events := []LocalEvent{
		{Local: 0, Kind: LocalEventDefine},
		{Local: 0, Kind: LocalEventRead},
		{Local: 0, Kind: LocalEventRead},
		{Local: 1, Kind: LocalEventDefine},
		{Local: 1, Kind: LocalEventRead},
		{Local: 1, Kind: LocalEventRead},
		{Local: 1, Kind: LocalEventRead},
	}
	got := PlanResidencyTransitionShadow(events, 2, 1, false)
	if got.Admissions != 2 || got.Evictions != 1 || got.Reloads != 0 || got.Writebacks != 1 {
		t.Fatalf("transition shadow = %+v", got)
	}
}

func TestPlanResidencyShadowFailsSoft(t *testing.T) {
	if got := PlanResidencyShadow(nil, ResidencyShadowMaxLocals+1, 1, false); got.FailSoft != 1 {
		t.Fatalf("oversized locals = %+v", got)
	}
	if got := PlanResidencyShadow(nil, 1, 1, true); got.FailSoft != 1 {
		t.Fatalf("overflowed tape = %+v", got)
	}
}

func TestFindResidencyShadow(t *testing.T) {
	entries := []ResidencyShadowEntry{
		{LocalStart: 3, Summary: ResidencyShadowSummary{Segments: 7}},
		{LocalStart: 9, Summary: ResidencyShadowSummary{Segments: 11}},
	}
	if got := FindResidencyShadow(entries, 9).Segments; got != 11 {
		t.Fatalf("segments = %d, want 11", got)
	}
	if got := FindResidencyShadow(entries, 4); got.Active() {
		t.Fatalf("missing summary = %+v", got)
	}
}

func TestPlanResidencyShadowNeverPanics(t *testing.T) {
	f := func(raw []byte) bool {
		events := make([]LocalEvent, len(raw)/3)
		for i := range events {
			events[i] = LocalEvent{Local: uint16(raw[i*3]), Depth: raw[i*3+1], Kind: LocalEventKind(raw[i*3+2])}
		}
		_ = PlanResidencyShadow(events, ResidencyShadowMaxLocals, 18, false)
		_ = PlanResidencyTransitionShadow(events, ResidencyShadowMaxLocals, 18, false)
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}
