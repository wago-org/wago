package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
)

func TestWriteMarkdownStatusAttributesEmitters(t *testing.T) {
	metrics := &dragline.Metrics{
		Version: dragline.MetricsVersion, NativeBytes: 96, PeakLiveBytes: 128,
		RailMach:   dragline.EmitterMetrics{Functions: 1, BodyBytes: 7, NativeBytes: 32, LowerNanos: 1_500_000, EmitNanos: 250_000},
		Structured: dragline.EmitterMetrics{Functions: 1, BodyBytes: 11, NativeBytes: 64, LowerNanos: 2_000_000, EmitNanos: 500_000},
		Functions: []dragline.FunctionMetrics{
			{Function: 3, BodyBytes: 7, NativeBytes: 32, LowerNanos: 1_500_000, EmitNanos: 250_000, RailMachFinalized: true, ScheduleKind: 2, InitialScheduleScoreCount: 2, InitialScheduleScores: [3]dragline.ScheduleCandidateMetrics{{Kind: 2, PrePostRANondominated: true, EstimatedCycles: 101, ResourceCycles: 77, SelectedBytes: 32, WeightedSpillDebt: 13, PhysicalCopies: 7, CopyCycles: 1}, {Kind: 3, EstimatedCycles: 103, ResourceCycles: 77, SelectedBytes: 32, WeightedSpillDebt: 8, PhysicalCopies: 4}}, LiveIntervals: 4, LiveSegments: 6, SegmentedRanges: 1, AllocationFragments: 2, EdgeMoves: 9, LoopBackedgeMoves: 4, FixedMoves: 2, SegmentedAttempted: true, SegmentedAdmitted: true, SegmentedBaselineDebt: 13, SegmentedCandidateDebt: 8, SegmentedBaselineCopies: 7, SegmentedCandidateCopies: 4},
			{Function: 4, BodyBytes: 11, NativeBytes: 64, LowerNanos: 2_000_000, EmitNanos: 500_000, StructuredReason: "unsupported-op:table.get"},
		},
	}
	var output bytes.Buffer
	if err := writeMarkdownStatus(&output, "/tmp/example.wasm", metrics); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Module: `example.wasm`", "| RailMach | 1 | 7 | 32 | 1.500 | 0.250 |", "| Structured | 1 | 11 | 64 | 2.000 | 0.500 |", "| 3 | RailMach | - |", "| 4 | Structured | unsupported-op:table.get |"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("Markdown status is missing %q:\n%s", want, output.String())
		}
	}
	if !strings.Contains(output.String(), "| 3 | RailMach | - | 7 | 32 | 0 | 9 | 4 | 2 | admitted | 13 → 8 | 7 → 4 | 4 | 6 | 1 | 2 | 1.500 | 0.250 |") {
		t.Fatalf("Markdown status is missing exact liveness columns:\n%s", output.String())
	}
	for _, want := range []string{"## Initial schedule candidates", "| 3 | 1 | latency/fusion | yes | yes | 101 | 77 | 32 | 13 | 7 | 1 |", "| 3 | 2 | pressure |  |  | 103 | 77 | 32 | 8 | 4 | 0 |"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("Markdown status is missing schedule candidate %q:\n%s", want, output.String())
		}
	}
}
