//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcWrapperInlineModule(t *testing.T) *wasm.Module {
	t.Helper()
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.EqRef}, []wasm.ValType{wasm.EqRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.EqRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{2})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCReferenceWrapperInline(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := gcWrapperInlineModule(t)
	meta, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	plans := shared.NewGCModuleFrameRootPlan(3)
	if !plans.MarkFunction(2) || !plans.ReserveFunctions(1) {
		t.Fatal("reserve root plan")
	}
	plan, ok := plans.BeginFunction(2)
	if !ok {
		t.Fatal("begin root plan")
	}
	plan.Candidate = true
	if !plan.SetLiveMasks([]uint64{0}, 0, 1) {
		t.Fatal("source call mask")
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Stats: &stats, GCFrameRoots: plans,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[2].Calls; got[callKindInline] != 1 {
		t.Fatalf("consumer inline calls=%+v, want one leaf through the wrapper", got)
	}
	if !plan.Exact || plan.CallsiteCount() != 0 {
		t.Fatalf("root plan exact=%v callsites=%d", plan.Exact, plan.CallsiteCount())
	}
}

func TestGCReferenceWrapperInlineLimits(t *testing.T) {
	for _, name := range []string{"valid", "changed-argument", "extra-op", "self-call", "handler", "nullable-cast", "extra-leaf-op", "invalidated-leaf", "wrapper-chain"} {
		t.Run(name, func(t *testing.T) {
			m := gcWrapperInlineModule(t)
			hints, _, _, err := computeModuleHints(m, 0, 0, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			targets := buildInlineTargets(m, hints, currentCodegenPolicy())
			targets.data.slots[1] = 0
			leaf := targets.target(0)
			if leaf == nil {
				t.Fatal("missing original leaf")
			}
			switch name {
			case "changed-argument":
				m.Code[1].BodyBytes = []byte{0xd0, 0x6d, 0x10, 0, 0x0b}
			case "extra-op":
				m.Code[1].BodyBytes = []byte{0x20, 0, 0x10, 0, 1, 0x0b}
			case "self-call":
				m.Code[1].BodyBytes = []byte{0x20, 0, 0x10, 1, 0x0b}
			case "handler":
				hints[1].flags |= hintModuleEH
			case "nullable-cast":
				leaf.body = []byte{0x20, 0, 0xfb, 23, 0, 0x0b}
			case "extra-leaf-op":
				leaf.body = []byte{0x20, 0, 0xfb, 22, 0, 1, 0x0b}
			case "invalidated-leaf":
				targets.data.slots[0] |= inlineTargetInvalid
			case "wrapper-chain":
				// Pretend function 2 is another already aliased wrapper. Its global
				// identity differs from the original leaf and must not be followed.
				targets.data.slots[2] = targets.data.slots[0]
				m.Code[1].BodyBytes = []byte{0x20, 0, 0x10, 2, 0x0b}
			}
			aliasReferenceCastWrappers(m, hints, targets)
			if got := targets.target(1) != nil; got != (name == "valid") {
				t.Fatalf("wrapper admitted=%v", got)
			}
			if name == "valid" {
				targets.data.slots[0] |= inlineTargetInvalid
				if targets.target(1) != nil {
					t.Fatal("alias survived leaf root-safety rejection")
				}
			}
		})
	}
}
