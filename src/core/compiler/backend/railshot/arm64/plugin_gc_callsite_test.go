//go:build arm64

package arm64

import (
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/arm64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCustomInstructionConsumesGCFrameCallsite(t *testing.T) {
	// A lowered import followed by a real call must consume two source masks,
	// while only the real call contributes a native return address.
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("inline")...), 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x10, 2, 0x0b}), wasmtest.Code([]byte{0x0b}))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	plans := shared.NewGCModuleFrameRootPlan(2)
	if !plans.MarkFunction(0) || !plans.ReserveFunctions(1) {
		t.Fatal("prepare plans")
	}
	plan, ok := plans.BeginFunction(0)
	if !ok {
		t.Fatal("begin plan")
	}
	plan.Candidate, plan.Exact = true, true
	if !plan.SetLiveMasks([]uint64{0, 0}, 0, 2) {
		t.Fatal("set masks")
	}
	lowering := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityManaged,
		Managed:       func(plugincodegen.ManagedContext) error { return nil },
	}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, GCFrameRoots: plans, CustomInstructions: map[uint32]railcore.CustomInstruction{0: {Codegen: lowering}}})
	if cm != nil && cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Exact {
		t.Fatal("custom instruction left a source call mask unconsumed; exact GC roots were lost")
	}
	if plan.CallsiteCount() != 1 {
		t.Fatalf("native callsites = %d, want 1", plan.CallsiteCount())
	}
}
