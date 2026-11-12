//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func staticParameterForwardModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 1},
			[]byte{0x60, 1, 0x64, 0, 1, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x0b}),
		)),
	)
}

func staticParameterEHModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 1},
			[]byte{0x60, 1, 0x64, 0, 1, 0x7f},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 2})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x1a, 0x41, 7, 0x0b}),
		)),
	)
}

func TestGCStaticParameterCastAndForwardCall(t *testing.T) {
	requireCompilerDiagnostics(t)
	m, err := wasm.DecodeModule(staticParameterForwardModule())
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[1].Peephole["gc-static-param-cast-elide"]; got != 1 {
		t.Errorf("same-type parameter cast elisions=%d want1", got)
	}
	if got := stats.Funcs[1].Calls["gcnative"]; got != 0 {
		t.Errorf("redundant parameter cast emitted%d native checks", got)
	}
	if got := stats.Funcs[2].Peephole["gc-forward-call"]; got != 1 {
		t.Errorf("caller forwarded%d calls want1", got)
	}
	if got := stats.Funcs[2].Calls[callKindWrapper]; got != 1 || stats.Funcs[2].Calls[callKindMixed] != 0 {
		t.Errorf("forwarded call must use the destination wrapper ABI: %v", stats.Funcs[2].Calls)
	}
}

func TestGCStaticParameterCastInEHModule(t *testing.T) {
	requireCompilerDiagnostics(t)
	m, err := wasm.DecodeModule(staticParameterEHModule())
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-static-param-cast-elide"]; got != 1 {
		t.Errorf("EH-module parameter cast elisions=%d want1", got)
	}
	if got := stats.Funcs[0].Calls["gcnative"]; got != 0 {
		t.Errorf("EH-module redundant parameter cast emitted%d native checks", got)
	}
}

func TestGCStaticParameterCastProofLimits(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		nullable, exactSource, final, exactCast, recursive bool
		sub                                                uint32
		want                                               bool
	}{
		{name: "nonnull", final: true, sub: 22, want: true},
		{name: "nonnull-to-nullable", final: true, sub: 23, want: true},
		{name: "nullable", nullable: true, final: true, sub: 23, want: true},
		{name: "nullable-must-check", nullable: true, final: true, sub: 22},
		{name: "open", sub: 22, want: true},
		{name: "open-exact-must-check", exactCast: true, sub: 22},
		{name: "exact-source", exactSource: true, exactCast: true, sub: 22, want: true},
		{name: "final-is-exact", final: true, exactCast: true, sub: 22, want: true},
		{name: "recursive-owner-required", final: true, recursive: true, sub: 22},
		{name: "test-is-not-cast", final: true, sub: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(staticParameterForwardModule())
			if err != nil {
				t.Fatal(err)
			}
			m.Types[0].SubTypes[0].Final = tc.final
			typ := wasm.RefVal(wasm.Ref(tc.nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: tc.recursive}), tc.exactSource))
			ft := &wasm.CompType{Params: []wasm.ValType{typ}}
			if got := staticGCParameterCast(m, ft, 0, tc.sub, 0, tc.exactCast); got != tc.want {
				t.Fatalf("proof=%t want%t", got, tc.want)
			}
			if staticGCParameterCast(m, ft, 1, tc.sub, 0, tc.exactCast) || staticGCParameterCast(m, ft, 0, tc.sub, 1, tc.exactCast) {
				t.Fatal("proof crossed parameter or type bounds")
			}
		})
	}
}

func TestGCForwardCallCompactOmittedTarget(t *testing.T) {
	m, err := wasm.DecodeModule(staticParameterForwardModule())
	if err != nil {
		t.Fatal(err)
	}
	// Entry0 must keep a real call to forwarder1. Compact inlining can omit
	// private destination2 after inlining its sole source call in forwarder1.
	m.Code[0].BodyBytes = []byte{0x20, 0, 0x10, 1, 0x0b}
	m.Code[1].BodyBytes = []byte{0x20, 0, 0xfb, 22, 0, 0x10, 2, 0x0b}
	m.Code[2].BodyBytes = []byte{0x41, 7, 0x0b}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"direct-reference-abi": false}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}}})
	if err != nil {
		t.Fatalf("valid compact forwarder must compile with an omitted inline destination: %v", err)
	}
	cm.CodeImage.Close()
}

func TestGCForwardCallBodyLimits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      []byte
		locals    bool
		eh, depth bool
		custom    int
		want      bool
	}{
		{name: "plain", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, custom: -1, want: true},
		{name: "no-cast", body: []byte{0x20, 0, 0x10, 0, 0x0b}, custom: -1},
		{name: "has-effect", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x1a, 0x41, 7, 0x0b}, custom: -1},
		{name: "self-call", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 1, 0x0b}, custom: -1},
		{name: "locals", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, locals: true, custom: -1},
		{name: "EH", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, eh: true, custom: -1},
		{name: "nested-inline", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, depth: true, custom: -1},
		{name: "custom-source", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, custom: 1},
		{name: "custom-target", body: []byte{0x20, 0, 0xfb, 22, 0, 0x10, 0, 0x0b}, custom: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(staticParameterForwardModule())
			if err != nil {
				t.Fatal(err)
			}
			m.Code[1].BodyBytes = tc.body
			if tc.locals {
				m.Code[1].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}}
			}
			ft, _ := m.LocalFuncType(1)
			f := &fn{m: m, policy: currentCodegenPolicy(), moduleEH: tc.eh}
			if tc.depth {
				f.inlineDepth = 1
			}
			if tc.custom >= 0 {
				f.customInstructions = map[uint32]CustomInstruction{uint32(tc.custom): {}}
			}
			target, gotType, ok := f.gcForwardCallTarget(1, ft)
			if ok != tc.want {
				t.Fatalf("match=%t want%t", ok, tc.want)
			}
			if ok && (target != 0 || gotType == nil) {
				t.Fatalf("wrong destination %d/%v", target, gotType)
			}
		})
	}
}
