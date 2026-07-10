//go:build linux && amd64 && !tinygo

package wago_test

import (
	"encoding/json"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/testutil/wasmtest"
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
			name: "null funcref argument and result supported",
			cmd: specExecCmd{
				Action:   specAction{Type: "invoke", Args: []specValue{ref("funcref")}},
				Expected: []specValue{ref("funcref")},
			},
			want: specGapNone,
		},
		{
			name: "null externref argument supported",
			cmd: specExecCmd{Action: specAction{
				Type: "invoke",
				Args: []specValue{ref("externref")},
			}},
			want: specGapNone,
		},
		{
			name: "non-null externref argument supported",
			cmd: specExecCmd{Action: specAction{
				Type: "invoke",
				Args: []specValue{{Type: "externref", Value: json.RawMessage(`"1"`)}},
			}},
			want: specGapNone,
		},
		{
			name: "non-null funcref argument",
			cmd: specExecCmd{Action: specAction{
				Type: "invoke",
				Args: []specValue{{Type: "funcref", Value: json.RawMessage(`"1"`)}},
			}},
			want: specGapReferenceArgument,
		},
		{
			name: "externref expected result supported",
			cmd: specExecCmd{
				Action:   specAction{Type: "invoke"},
				Expected: []specValue{ref("externref")},
			},
			want: specGapNone,
		},
		{
			name: "null funcref global supported",
			cmd: specExecCmd{
				Action:   specAction{Type: "get"},
				Expected: []specValue{ref("funcref")},
			},
			want: specGapNone,
		},
		{
			name: "externref global supported",
			cmd: specExecCmd{
				Action:   specAction{Type: "get"},
				Expected: []specValue{{Type: "externref", Value: json.RawMessage(`"1"`)}},
			},
			want: specGapNone,
		},
		{
			name: "non-null funcref global supported",
			cmd: specExecCmd{
				Action:   specAction{Type: "get"},
				Expected: []specValue{{Type: "funcref"}},
			},
			want: specGapNone,
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

func TestSpecExecNullFuncrefValueEncoding(t *testing.T) {
	null := specValue{Type: "funcref", Value: json.RawMessage(`"null"`)}
	if slots, ok := specArgSlots(null); !ok || len(slots) != 1 || slots[0] != 0 {
		t.Fatalf("null funcref arg slots = %v, %v; want [0], true", slots, ok)
	}
	if !matchResult([]uint64{0}, null) || matchResult([]uint64{1}, null) {
		t.Fatal("null funcref result matching did not require token zero")
	}
	nonNull := specValue{Type: "funcref"}
	if matchResult([]uint64{0}, nonNull) || !matchResult([]uint64{1}, nonNull) {
		t.Fatal("non-null funcref result matching did not require a nonzero opaque token")
	}
	nullExtern := specValue{Type: "externref", Value: json.RawMessage(`"null"`)}
	if slots, ok := specArgSlots(nullExtern); !ok || len(slots) != 1 || slots[0] != 0 {
		t.Fatalf("null externref arg slots = %v, %v; want [0], true", slots, ok)
	}
	if !matchResult([]uint64{0}, nullExtern) || matchResult([]uint64{1}, nullExtern) {
		t.Fatal("null externref result matching did not require token zero")
	}
}

func TestInvokeActionExecutesNullFuncrefArgumentAndResult(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
	compiled, err := wago.Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	inst, err := wago.Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close()

	null := specValue{Type: "funcref", Value: json.RawMessage(`"null"`)}
	cmd := specExecCmd{
		Action:   specAction{Type: "invoke", Field: "id", Args: []specValue{null}},
		Expected: []specValue{null},
	}
	out := invokeAction(cmd, specModule{inst: inst, compiled: compiled}, t)
	if out.gap != specGapNone || out.harnessErr != nil || out.trap != nil || len(out.results) != 1 || out.results[0] != 0 {
		t.Fatalf("invokeAction null funcref outcome = %+v, want one zero result with no gap/error", out)
	}
	if gap, passed := runReturnAssert(t, "null_funcref", cmd, specModule{inst: inst, compiled: compiled}); gap != specGapNone || !passed {
		t.Fatalf("runReturnAssert = gap %s passed %v, want none/true", gap, passed)
	}
}

func TestInvokeActionExecutesExternrefIdentity(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x0b}))),
	)
	compiled, err := wago.Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	inst, err := wago.Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close()

	ref := specValue{Type: "externref", Value: json.RawMessage(`"42"`)}
	cmd := specExecCmd{
		Action:   specAction{Type: "invoke", Field: "id", Args: []specValue{ref}},
		Expected: []specValue{ref},
	}
	m := specModule{inst: inst, compiled: compiled, externrefs: make(map[string]wago.ExternRef)}
	out := invokeAction(cmd, m, t)
	if out.gap != specGapNone || out.harnessErr != nil || out.trap != nil || len(out.results) != 1 || out.results[0] == 0 {
		t.Fatalf("invokeAction externref outcome = %+v, want one non-null result with no gap/error", out)
	}
	if !m.matchExternref(out.results[0], ref) {
		t.Fatal("externref result did not resolve to fixture identity 42")
	}
	if gap, passed := runReturnAssert(t, "externref", cmd, m); gap != specGapNone || !passed {
		t.Fatalf("runReturnAssert = gap %s passed %v, want none/true", gap, passed)
	}
}

func TestInvokeActionExecutesExternrefGlobalIdentity(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, true, []byte{0xd0, 0x6f, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("ref", 3, 0))),
	)
	compiled, err := wago.Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	inst, err := wago.Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close()

	ref := specValue{Type: "externref", Value: json.RawMessage(`"42"`)}
	m := specModule{inst: inst, compiled: compiled, externrefs: make(map[string]wago.ExternRef)}
	token, err := m.externrefArg(ref)
	if err != nil {
		t.Fatalf("externrefArg: %v", err)
	}
	if err := inst.SetGlobalValue("ref", wago.ValueOf(wago.ValExternRef, token)); err != nil {
		t.Fatalf("SetGlobalValue: %v", err)
	}
	cmd := specExecCmd{Action: specAction{Type: "get", Field: "ref"}, Expected: []specValue{ref}}
	out := invokeAction(cmd, m, t)
	if out.gap != specGapNone || out.harnessErr != nil || out.trap != nil || len(out.results) != 1 || out.results[0] != token {
		t.Fatalf("invokeAction externref global outcome = %+v, want token %#x", out, token)
	}
	if gap, passed := runReturnAssert(t, "externref_global", cmd, m); gap != specGapNone || !passed {
		t.Fatalf("runReturnAssert = gap %s passed %v, want none/true", gap, passed)
	}
}

func TestInvokeActionClassifiesAbsentExport(t *testing.T) {
	out := invokeAction(specExecCmd{Action: specAction{Type: "invoke", Field: "missing"}}, specModule{compiled: &wago.Compiled{}}, t)
	if out.gap != specGapAbsentExport {
		t.Fatalf("gap = %s, want %s", out.gap, specGapAbsentExport)
	}
}
