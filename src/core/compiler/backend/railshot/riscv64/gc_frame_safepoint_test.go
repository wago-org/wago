//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64RecordGCFrameSafepoint(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate:     true,
		Exact:         true,
		SafepointBase: 7,
		Locals:        []shared.GCFrameLocal{{Index: 0, Offset: 48}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("set live masks")
	}
	emit := integerEmitter{
		target:       target{header: callFrameHeader, nlocals: 2},
		frameBytes:   128,
		gcFrameRoots: plan,
	}
	if id := emit.recordGCFrameSafepoint([]bool{false, true}); id != 8 {
		t.Fatalf("safepoint ID = %d, want 8", id)
	}
	if !plan.Exact || plan.SafepointCount() != 1 {
		t.Fatalf("safepoint plan = exact %t count %d", plan.Exact, plan.SafepointCount())
	}
	var got []uint32
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		got = append(got, offsets...)
		return true
	}) {
		t.Fatal("malformed safepoint stream")
	}
	// The live local occupies the first call-frame local home. The hidden
	// operand root occupies spill slot one after both local homes.
	if want := []uint32{48, 72}; !slices.Equal(got, want) {
		t.Fatalf("root offsets = %v, want %v", got, want)
	}
}

func TestRV64RecordGCFrameSafepointRejectsDuplicateHomes(t *testing.T) {
	plan := &shared.GCFrameRootPlan{
		Candidate: true,
		Exact:     true,
		Locals:    []shared.GCFrameLocal{{Index: 0, Offset: 72}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("set live masks")
	}
	emit := integerEmitter{
		target:       target{header: callFrameHeader, nlocals: 2},
		frameBytes:   128,
		gcFrameRoots: plan,
	}
	if id := emit.recordGCFrameSafepoint([]bool{false, true}); id != 1 {
		t.Fatalf("safepoint ID = %d, want 1", id)
	}
	if plan.Exact || plan.SafepointCount() != 0 {
		t.Fatalf("duplicate root homes escaped: exact %t count %d", plan.Exact, plan.SafepointCount())
	}
}

func TestRV64AllocationHelpersCarryExactSafepointIDs(t *testing.T) {
	source, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	counts := make([]int, len(m.Code))
	candidates := 0
	for function := range m.Code {
		r := wasm.NewReader(m.Code[function].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				t.Fatal(err)
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(r, op, &imm); err != nil {
				t.Fatal(err)
			}
			if op == 0xfb {
				switch imm.Subopcode {
				case 0, 1, 6, 7, 8, 9, 10:
					counts[function]++
				}
			}
		}
		if counts[function] != 0 {
			candidates++
		}
	}
	roots := shared.NewGCModuleFrameRootPlan(len(m.Code))
	for function, count := range counts {
		if count != 0 && !roots.MarkFunction(function) {
			t.Fatal("mark root function", function)
		}
	}
	if !roots.ReserveFunctions(candidates) {
		t.Fatal("reserve root functions")
	}
	base := uint32(0)
	for function, count := range counts {
		if count == 0 {
			continue
		}
		plan, ok := roots.BeginFunction(function)
		if !ok {
			t.Fatal("begin root function", function)
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, SafepointBase: base}
		if !plan.SetLiveMasks(make([]uint64, count), count, 0) {
			t.Fatal("set root masks", function)
		}
		base += uint32(count)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 2, GCRefEqSource: true,
		GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for function, count := range counts {
		plan := roots.Function(function)
		if count == 0 {
			if plan != nil {
				t.Fatal("unexpected root plan", function)
			}
			continue
		}
		if plan == nil || !plan.Exact || plan.SafepointCount() != count || plan.FrameBytes < shared.RV64CallFrameHeaderBytes {
			t.Fatalf("function %d root plan = %+v count %d", function, plan, count)
		}
		seen += plan.SafepointCount()
	}
	if seen != int(base) {
		t.Fatalf("emitted safepoints = %d, want %d", seen, base)
	}
}

func TestRV64GCFramePlanAuthenticatesCompleteConservativeLocals(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xd0, 0x00, 0x21, 0x00, // ref.null 0; local.set 0
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, // struct.new 0; drop
		0x20, 0x00, 0x1a, 0x0b, // local.get 0; drop; end
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	makePlan := func(locals []shared.GCFrameLocal, mask uint64) *shared.GCModuleFrameRootPlan {
		module := shared.NewGCModuleFrameRootPlan(1)
		if !module.MarkFunction(0) || !module.ReserveFunctions(1) {
			t.Fatal("prepare root plan")
		}
		plan, ok := module.BeginFunction(0)
		if !ok {
			t.Fatal("begin root plan")
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, Locals: locals}
		if !plan.SetLiveMasks([]uint64{mask}, 1, 0) {
			t.Fatal("set root masks")
		}
		return module
	}
	wantLocal := []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}}
	if validateGCFrameRootPlans(m, makePlan(nil, 0)) {
		t.Fatal("plan omitted a live collector local")
	}
	if validateGCFrameRootPlans(m, makePlan(wantLocal, 0)) {
		t.Fatal("plan cleared a live collector-local bit")
	}
	if !validateGCFrameRootPlans(m, makePlan(wantLocal, 1)) {
		t.Fatal("complete conservative collector-local plan rejected")
	}
}

func TestRV64SafepointHelperBridgeArities(t *testing.T) {
	for _, tc := range []struct {
		helper, admitted uint32
		want             int
	}{
		{gcStructAllocDefault, 0, 1},
		{gcStructAllocOne, 2, 2},
		{gcStructAllocOne, 3, 3},
		{gcStructAllocOne, 4, 4},
		{gcStructAllocOne, 256, 256},
		{gcArrayAllocDefault, 0, 2},
		{gcArrayAllocUniform, 0, 3},
		{gcArrayAllocFixed, 4, 4},
		{gcArrayAllocFixed, 5, 5},
		{gcArrayAllocData, 0, 4},
		{gcArrayAllocElem, 0, 4},
	} {
		got, ok := gcAllocHelperSlots(tc.helper, int(tc.admitted))
		if !ok || got != tc.want {
			t.Fatalf("helper %d/%d slots = %d, %t; want %d", tc.helper, tc.admitted, got, ok, tc.want)
		}
	}
	for _, tc := range []struct {
		helper, admitted uint32
	}{{gcStructGet, 0}, {gcStructAllocOne, 1}, {gcStructAllocOne, 257}, {gcArrayAllocFixed, 6}} {
		if got, ok := gcAllocHelperSlots(tc.helper, int(tc.admitted)); ok {
			t.Fatalf("helper %d/%d unexpectedly admitted with %d slots", tc.helper, tc.admitted, got)
		}
	}
}
