package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
)

func TestWriteMarkdownStatusAttributesEmitters(t *testing.T) {
	metrics := &dragline.Metrics{
		Version: 18, NativeBytes: 96, PeakLiveBytes: 128,
		RailMach:   dragline.EmitterMetrics{Functions: 1, BodyBytes: 7, NativeBytes: 32, LowerNanos: 1_500_000, EmitNanos: 250_000},
		Structured: dragline.EmitterMetrics{Functions: 1, BodyBytes: 11, NativeBytes: 64, LowerNanos: 2_000_000, EmitNanos: 500_000},
		Functions: []dragline.FunctionMetrics{
			{Function: 3, BodyBytes: 7, NativeBytes: 32, LowerNanos: 1_500_000, EmitNanos: 250_000, RailMachFinalized: true},
			{Function: 4, BodyBytes: 11, NativeBytes: 64, LowerNanos: 2_000_000, EmitNanos: 500_000},
		},
	}
	var output bytes.Buffer
	if err := writeMarkdownStatus(&output, "/tmp/example.wasm", metrics); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Module: `example.wasm`", "| RailMach | 1 | 7 | 32 | 1.500 | 0.250 |", "| Structured | 1 | 11 | 64 | 2.000 | 0.500 |", "| 3 | RailMach |", "| 4 | Structured |"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("Markdown status is missing %q:\n%s", want, output.String())
		}
	}
}
