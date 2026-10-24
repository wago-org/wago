//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_codegenstats

package wago

import (
	"fmt"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDirectReferenceABIUsesRegisterCall(t *testing.T) {
	m, err := wasm.DecodeModule(directReferenceABIRootsModule())
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	plan := newGCFrameRootPlan(m, true, true, &diagnostic, nil)
	if plan == nil || diagnostic != "" {
		t.Fatalf("root plan: %s", diagnostic)
	}
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, GCFrameRoots: plan, Stats: &stats, Optimizations: map[string]bool{"inline": false, "dead-gc-new": false}})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[1].Calls[shared.CallMixed]; got != 1 {
		t.Fatalf("direct reference call uses %d register calls, want 1; calls=%v", got, stats.Funcs[1].Calls)
	}
	if cm.Entry[6] == cm.InternalEntry[6] {
		t.Fatal("reference target lost its wrapper entry")
	}
	caller := plan.Function(1)
	if !caller.Exact || caller.CallMaskCount() != 1 || caller.CallsiteCount() != 1 {
		t.Fatalf("caller root accounting: %+v", caller)
	}
	caller.VisitCallsites(func(_ int, site shared.GCFrameCallsite) bool {
		if got := site.Offsets(); len(got) != 1 {
			t.Fatalf("call must preserve exactly its hidden reference: %v", got)
		}
		return true
	})
	target := plan.Function(6)
	if !target.Exact || len(target.Locals) != 1 || target.AdapterReturnOffset == 0 {
		t.Fatalf("reference parameter/adapter roots: %+v", target)
	}
}

func TestDirectReferenceABIShapeSelection(t *testing.T) {
	for _, shape := range directReferenceABIShapes() {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled%t", shape.name, enabled), func(t *testing.T) {
				data, _ := directReferenceABIShapeModule(shape)
				m, err := wasm.DecodeModule(data)
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				var stats backend.ModuleStats
				cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, Stats: &stats, Optimizations: map[string]bool{"inline": false, "direct-reference-abi": enabled}})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				wantRegister := 0
				if enabled && shape.register {
					wantRegister = 1
				}
				if got := stats.Funcs[1].Calls[shared.CallMixed]; got != wantRegister {
					t.Fatalf("register calls=%d, want%d: %v", got, wantRegister, stats.Funcs[1].Calls)
				}
				if wantRegister == 1 && cm.Entry[0] == cm.InternalEntry[0] {
					t.Fatal("private reference target lost its wrapper adapter")
				}
			})
		}
	}
}

func directReferenceABIScopeModule(scope string) []byte {
	guard := []byte{0x0b}
	switch scope {
	case "tail":
		guard = []byte{0x12, 3, 0x0b}
	case "eh":
		guard = []byte{0x1f, 0x40, 0, 0x01, 0x0b, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 1, 0x63, 0, 1, 0x63, 0}, directReferenceABIFuncType(nil, []wasm.ValType{wasm.I32}), directReferenceABIFuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0xfb, 0, 0, 0x10, 0, 0xfb, 2, 0, 0, 0x0b}), wasmtest.Code(guard), wasmtest.Code([]byte{0x0b}))),
	)
}

func TestDirectReferenceABIModuleScope(t *testing.T) {
	for _, scope := range []string{"ordinary", "tail", "eh"} {
		for _, registerABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/regABI%t", scope, registerABI), func(t *testing.T) {
				m, err := wasm.DecodeModule(directReferenceABIScopeModule(scope))
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				var stats backend.ModuleStats
				cm, err := backend.CompileModuleWith(m, backend.CompileOptions{GCStructHelpers: true, Stats: &stats, Optimizations: map[string]bool{"inline": false, "reg-abi": registerABI, "direct-reference-abi": true}})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				want := 0
				if registerABI && scope == "ordinary" {
					want = 1
				}
				if got := stats.Funcs[1].Calls[shared.CallMixed]; got != want {
					t.Fatalf("scope=%s register calls=%d, want%d: %v", scope, got, want, stats.Funcs[1].Calls)
				}
			})
		}
	}
}
