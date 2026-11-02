//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDynamicImportUnwindLocalJoinARM64(t *testing.T) {
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, []byte{2, 3, 'e', 'n', 'v', 1, 'a', 0, 0, 3, 'e', 'n', 'v', 1, 'b', 0, 0}),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x20, 0, 0x6a, 0x0b}))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	plans := shared.NewGCModuleFrameRootPlan(1)
	if !plans.MarkFunction(0) || !plans.ReserveFunctions(1) {
		t.Fatal("prepare plans")
	}
	plan, ok := plans.BeginFunction(0)
	if !ok {
		t.Fatal("begin plan")
	}
	plan.Candidate, plan.Exact, plan.UnwindOnly = true, true, true
	if !plan.SetLiveMasks([]uint64{0}, 0, 1) {
		t.Fatal("set masks")
	}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, SyncHostCalls: true, GCFrameRoots: plans, ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if !plan.Exact {
		t.Fatal("lost exact unwind maps")
	}
	// The tagged Go-host and cross-instance paths must both retain return maps.
	if got := plan.CallsiteCount(); got != 2 {
		t.Fatalf("return maps = %d, want 2", got)
	}
	seen := make(map[uint32]bool)
	if !plan.VisitCallsites(func(_ int, site shared.GCFrameCallsite) bool {
		if site.ReturnOffset() == 0 || seen[site.ReturnOffset()] || len(site.Offsets()) != 0 {
			t.Errorf("invalid zero-root return map: offset=%d roots=%v", site.ReturnOffset(), site.Offsets())
		}
		seen[site.ReturnOffset()] = true
		return true
	}) {
		t.Fatal("invalid callsite stream")
	}
}

func TestNativeLeafContextShortcutGCPolicyARM64(t *testing.T) {
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	f := &fn{m: &wasm.Module{Memories: []wasm.MemType{{}}}}
	b := ImportBinding{Dynamic: true}
	if !f.nativeLeafContextShortcut(b, ft, false) {
		t.Fatal("ordinary native shortcut disabled")
	}
	if f.nativeLeafContextShortcut(b, ft, true) {
		t.Fatal("recorded call bypasses return map")
	}
	f.gcFrameRoots = &shared.GCFrameRootPlan{Candidate: true, Exact: true, UnwindOnly: true}
	if f.nativeLeafContextShortcut(b, ft, false) {
		t.Fatal("unwind-only call bypasses return map")
	}
	f.gcFrameRoots.Candidate = false
	if !f.nativeLeafContextShortcut(b, ft, false) {
		t.Fatal("noncandidate native shortcut disabled")
	}
}
