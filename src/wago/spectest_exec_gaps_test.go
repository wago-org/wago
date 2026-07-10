//go:build linux && amd64 && !tinygo

package wago_test

import (
	"encoding/json"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

func TestSpecExecGapAccounting(t *testing.T) {
	var moduleStats specExecStats
	moduleStats.skipModule(specGapCompileRejected)
	moduleStats.skipModule(specGapInstantiateRejected)
	var assertionStats specExecStats
	assertionStats.skipAssertion(specGapAbsentExport)
	assertionStats.skipAssertion(specGapReferenceArgument)
	assertionStats.skipAssertion(specGapReferenceResult)
	assertionStats.skipAssertion(specGapReferenceGlobal)

	var stats specExecStats
	stats.add(moduleStats)
	stats.add(assertionStats)
	if stats.modulesSkipped != 2 {
		t.Fatalf("modules skipped = %d, want 2", stats.modulesSkipped)
	}
	if stats.assertionsSkipped != 4 {
		t.Fatalf("assertions skipped = %d, want 4", stats.assertionsSkipped)
	}
	for _, reason := range []specExecGapReason{
		specGapCompileRejected,
		specGapInstantiateRejected,
		specGapAbsentExport,
		specGapReferenceArgument,
		specGapReferenceResult,
		specGapReferenceGlobal,
	} {
		if got := stats.gapCount(reason); got != 1 {
			t.Errorf("gap %s count = %d, want 1", reason, got)
		}
	}
}

func TestSpecExecAssertionGapClassification(t *testing.T) {
	ref := func(typ string) specValue {
		return specValue{Type: typ, Value: json.RawMessage(`"null"`)}
	}
	numeric := specValue{Type: "i32", Value: json.RawMessage(`"0"`)}

	tests := []struct {
		name string
		cmd  specExecCmd
		want specExecGapReason
	}{
		{
			name: "reference argument",
			cmd: specExecCmd{Action: specAction{
				Type: "invoke",
				Args: []specValue{ref("funcref")},
			}},
			want: specGapReferenceArgument,
		},
		{
			name: "reference expected result",
			cmd: specExecCmd{
				Action:   specAction{Type: "invoke"},
				Expected: []specValue{ref("externref")},
			},
			want: specGapReferenceResult,
		},
		{
			name: "reference global",
			cmd: specExecCmd{
				Action:   specAction{Type: "get"},
				Expected: []specValue{ref("funcref")},
			},
			want: specGapReferenceGlobal,
		},
		{
			name: "numeric assertion",
			cmd: specExecCmd{
				Action:   specAction{Type: "invoke", Args: []specValue{numeric}},
				Expected: []specValue{numeric},
			},
			want: specGapNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAssertionGap(tt.cmd); got != tt.want {
				t.Fatalf("gap = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestInvokeActionClassifiesAbsentExport(t *testing.T) {
	out := invokeAction(specExecCmd{Action: specAction{Type: "invoke", Field: "missing"}}, specModule{compiled: &wago.Compiled{}}, t)
	if out.gap != specGapAbsentExport {
		t.Fatalf("gap = %s, want %s", out.gap, specGapAbsentExport)
	}
}
