//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/nativeabi"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func exceptionScalarModule(t testing.TB, nested bool) *wasm.Module {
	t.Helper()
	body := []byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0}
	tags := []byte{1, 0, 0}
	if nested {
		tags = []byte{2, 0, 0, 0, 0}
		body = append(body, 0x1f, 0x40, 1, byte(wasm.CatchTag), 1, 1)
	}
	body = append(body, 0x20, 0, 0x08, 0, 0x0b)
	if nested {
		body = append(body, 0x00, 0x0b)
	}
	body = append(body, 0x00, 0x0b, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(13, tags),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{}); err != nil {
		t.Fatal(err)
	}
	return m
}

func exceptionReferenceModule(t testing.TB, null, branchExit bool) *wasm.Module {
	t.Helper()
	body := []byte{0x02, 0x7f, 0x02, byte(wasm.HeapExn), 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0,
		0x20, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x21, 1,
		0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 1, 0x0a, 0x0b, 0x00, 0x0b, 0x0b}
	if null {
		body = []byte{0xd0, byte(wasm.HeapExn), 0x0a, 0x0b}
	}
	if branchExit {
		body = []byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x41, 7, 0x0c, 1,
			0x0b, 0x00, 0x0b, 0x1a, 0x20, 0, 0x08, 0, 0x0b}
	}
	code := append([]byte{1, 1, byte(wasm.HeapExn)}, body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(13, []byte{1, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func compileExceptionFunction(t testing.TB, m *wasm.Module, index int) ([]byte, FunctionInfo) {
	t.Helper()
	state := moduleState{production: true, exceptionModule: m, capacity: coreruntime.MaxHostArity}
	for i := range m.Types {
		sig := &m.Types[i].SubTypes[0].Comp
		state.types = append(state.types, functionSpec{signature: sig})
	}
	for i := range m.Code {
		sig, _ := m.LocalFuncType(i)
		state.functions = append(state.functions, functionSpec{signature: sig})
	}
	sig, _ := m.LocalFuncType(index)
	summary, err := walkInteger(&m.Code[index], sig, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, info, calls, err := compileIntegerMode(&m.Code[index], sig, &state, summary, emissionMode{production: true, interruptible: true, invocationContext: true, exceptionModule: m, functionIndex: uint32(index)})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatal("standalone exception fixture has calls")
	}
	return code, info
}

func TestExceptionScalarControlEmission(t *testing.T) {
	for _, nested := range []bool{false, true} {
		m := exceptionScalarModule(t, nested)
		code, info := compileExceptionFunction(t, m, 0)
		if len(code) == 0 || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes < callFrameHeader+exceptionRecordBytes {
			t.Fatalf("exception frame: %+v", info)
		}
	}
}

func TestExceptionSourceAdmission(t *testing.T) {
	m := exceptionScalarModule(t, false)
	if !AdmitExceptionHandlingModule(m) {
		projection := exceptionAdmissionProjection(m)
		t.Fatalf("exception source rejected: validation=%v shape=%v", wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true, CompactImports: true}, 1, wasm.ValidationLimits{}), admitShapeProfileFunctions(projection, true, true, true, true, false))
	}
}

func exceptionTypedOperandModule(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x20, 0, 0x20, 1, 0x02, 0x69, 0x1f, 0x40, 1, 3, 0, 0x41, 42, 0x08, 0, 0x0b, 0x00, 0x0b, 0x1a, 0x1a, 0x1a, 0x41, 42, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef, wasm.AnyRef}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func BenchmarkExceptionOrdinarySourceScan(b *testing.B) {
	for _, count := range []int{128, 1024} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := &wasm.Module{Types: make([]wasm.RecType, count), FuncTypes: make([]wasm.TypeIdx, count), Code: make([]wasm.Func, count)}
			for i := range m.Types {
				m.Types[i].SubTypes = []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}
				m.FuncTypes[i] = wasm.TypeIdx{Index: uint32(i)}
				m.Code[i].BodyBytes = []byte{0x01, 0x0b}
			}
			b.ReportAllocs()
			b.SetBytes(int64(count * 2))
			b.ResetTimer()
			for range b.N {
				if AdmitExceptionHandlingModule(m) {
					b.Fatal("ordinary module selected EH")
				}
			}
		})
	}
}

func TestExceptionFixedCollectorRootContract(t *testing.T) {
	body := []byte{0x02, byte(wasm.HeapExn), 0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0, 0xd0, 0, 0x08, 0, 0x0b, 0x00, 0x0b, 0x21, 0, 0x10, 1, 0x41, 42, 0x0b}
	code := append([]byte{1, 1, byte(wasm.HeapExn)}, body...)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))), wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...), wasmtest.Code([]byte{0x0b}))))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{}); err != nil {
		t.Fatal(err)
	}
	maps, err := BuildExceptionRootMaps(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 1 || maps[0].LocalFunction != 0 || maps[0].FrameBytes != 400 {
		t.Fatalf("EH maps=%+v", maps)
	}
	var offsets []uint32
	for _, slot := range maps[0].Slots {
		if slot.Kind != nativeabi.RootGCRef {
			t.Fatalf("EH root kind=%v", slot.Kind)
		}
		offsets = append(offsets, slot.Offset)
	}
	if want := []uint32{336, 344, 352, 360, 368, 376, 384, 392}; !slices.Equal(offsets, want) {
		t.Fatalf("EH offsets=%v,want%v", offsets, want)
	}
	plans := shared.NewGCModuleFrameRootPlan(2)
	plans.MarkFunction(0)
	plans.ReserveFunctions(1)
	plan, _ := plans.BeginFunction(0)
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 0, 1) || !plan.SetFixedOffsets(offsets) {
		t.Fatal("plan setup")
	}
	if !validateGCFrameRootPlans(m, plans) {
		t.Fatal("authentic EH fixed roots rejected")
	}
	// Replace the first GC root with a scalar lane; source authentication must fail.
	offsets[0] = 328
	if validateGCFrameRootPlans(m, plans) {
		t.Fatal("missing EH payload root admitted")
	}
	offsets[0] = 336
	owned := shared.NewGCModuleFrameRootPlan(2)
	owned.MarkFunction(0)
	owned.ReserveFunctions(1)
	ownedPlan, _ := owned.BeginFunction(0)
	*ownedPlan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, Locals: []shared.GCFrameLocal{{Index: 0, Offset: 48}}}
	if !ownedPlan.SetLiveMasks([]uint64{1, 1}, 1, 1) || !ownedPlan.SetFixedOffsets(slices.Clone(offsets)) {
		t.Fatal("owned plan setup")
	}
	if !validateGCFrameRootPlansOwned(m, owned, true) {
		t.Fatal("owned exn local/copy allocation contract rejected")
	}
	if validateGCFrameRootPlans(m, owned) {
		t.Fatal("owned copy allocation contract accepted as legacy frame copies")
	}

}

func TestExceptionWireCandidateSkipsOrdinaryImmediates(t *testing.T) {
	ordinary := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 8, 0x0b}))))
	if exceptionWireCandidate(ordinary[8:]) {
		t.Fatal("numeric immediate classified as EH")
	}
	if got := testing.AllocsPerRun(100, func() {
		if exceptionWireCandidate(ordinary[8:]) {
			panic("candidate")
		}
	}); got != 0 {
		t.Fatalf("ordinary wire scan allocations=%v", got)
	}
	tagless := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, byte(wasm.HeapExn), 0x0a, 0x0b}))))
	if !AdmitExceptionHandlingSource(tagless) {
		t.Fatal("validated tagless throw_ref source rejected")
	}
	declared := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x63, byte(wasm.HeapExn)})), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, byte(wasm.HeapExn), 0x0b}))))
	if !AdmitExceptionHandlingSource(declared) {
		t.Fatal("validated exception reference declaration rejected")
	}
	state := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec([]byte{byte(wasm.HeapExn), 0, 0xd0, byte(wasm.HeapExn), 0x0b})))
	if !AdmitExceptionHandlingSource(state) {
		t.Fatal("validated exception global without function types rejected")
	}
}

var exceptionOperandPlanSink []*wasm.CollectorOperandRoots

func BenchmarkExceptionTypedOperandPlanning(b *testing.B) {
	for _, count := range []int{16, 128} {
		m := exceptionTypedOperandModule(b)
		fn := m.Code[0]
		m.Code = make([]wasm.Func, count)
		m.FuncTypes = make([]wasm.TypeIdx, count)
		selected := make([]bool, count)
		for i := range m.Code {
			m.Code[i] = fn
			selected[i] = true
		}
		b.Run(fmt.Sprintf("per-function/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				for i := range m.Code {
					if _, err := wasm.BuildCollectorOperandRoots(m, i); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
		b.Run(fmt.Sprintf("module/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				plans, err := wasm.BuildCollectorOperandRootModule(m, selected)
				if err != nil {
					b.Fatal(err)
				}
				exceptionOperandPlanSink = plans
			}
		})
	}
}

func BenchmarkExceptionNonCollectingPreparation(b *testing.B) {
	for _, count := range []int{128, 1024} {
		m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}}}}}, FuncTypes: make([]wasm.TypeIdx, count), Code: make([]wasm.Func, count)}
		for i := range m.Code {
			m.Code[i].BodyBytes = []byte{0x20, 0, 0x0b}
		}
		opts := CompileOptions{OwnedExceptionReferences: true, GCFrameRoots: shared.NewGCModuleFrameRootPlan(count)}
		b.Run(fmt.Sprintf("skip/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := prepareCollectorOperandPlans(m, &opts); err != nil {
					b.Fatal(err)
				}
			}
		})
		selected := make([]bool, count)
		b.Run(fmt.Sprintf("catalogue-control/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				plans, err := wasm.BuildCollectorOperandRootModule(m, selected)
				if err != nil {
					b.Fatal(err)
				}
				exceptionOperandPlanSink = plans
			}
		})
	}
}
