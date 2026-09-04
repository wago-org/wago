//go:build (linux && amd64) || ((linux || darwin) && arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestGCFrameFixedOffsetsRetainsOnlyCollectorRoots(t *testing.T) {
	rootMap := nativeabi.FunctionRootMap{Slots: []nativeabi.RootSlot{
		{Offset: 8, Kind: nativeabi.RootFuncRef},
		{Offset: 16, Kind: nativeabi.RootGCRef},
		{Offset: 24, Kind: nativeabi.RootFuncRef},
		{Offset: 32, Kind: nativeabi.RootGCRef},
	}}
	got := gcFrameFixedOffsets(&rootMap)
	if len(got) != 2 || cap(got) != 2 || got[0] != 16 || got[1] != 32 {
		t.Fatalf("collector offsets = %v (cap %d), want [16 32] with exact capacity", got, cap(got))
	}
	rootMap.Slots = rootMap.Slots[:1]
	if got := gcFrameFixedOffsets(&rootMap); got != nil {
		t.Fatalf("non-collector offsets = %v, want nil", got)
	}
}

func gcFrameRootNoneModule(tb testing.TB, functions int) *wasm.Module {
	tb.Helper()
	funcs := make([][]byte, functions)
	codes := make([][]byte, functions)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(0)
		codes[i] = wasmtest.Code([]byte{0x0b})
	}
	if functions > 1 {
		codes[functions-1] = wasmtest.Code([]byte{0x10, 0x00, 0x0b})
	}
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := frontend.DecodeValidate(binary)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

func TestGCFrameRootPlanOmitsNonCollectingFunction(t *testing.T) {
	m := gcFrameRootNoneModule(t, 2)
	plan := newGCFrameRootPlan(m, true)
	if plan == nil || plan.Diagnostic != "" {
		t.Fatalf("module root plan = %+v", plan)
	}
	if got := plan.Function(0); got != nil {
		t.Fatalf("non-collecting leaf root plan = %+v, want nil", got)
	}
	if got := plan.Function(1); got == nil || !got.Candidate || !got.Exact {
		t.Fatalf("collecting caller root plan = %+v, want exact candidate", got)
	}
}

var gcFrameRootPlanSink *shared.GCModuleFrameRootPlan

func BenchmarkGCFrameRootPlanManyNonCollectingFunctions(b *testing.B) {
	m := gcFrameRootNoneModule(b, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		gcFrameRootPlanSink = newGCFrameRootPlan(m, true)
	}
}

func gcFrameRootCollectingModule(tb testing.TB, functions int) *wasm.Module {
	tb.Helper()
	funcs := make([][]byte, functions)
	codes := make([][]byte, functions)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(1)
		body := []byte{
			0x01, 0x01, 0x63, 0x00, // one (ref null type 0) local
			0xfb, 0x01, 0x00, 0x1a, // struct.new_default 0; drop
			0x10, 0x00, // call function 0
			0x20, 0x00, 0x1a, 0x0b, // local.get 0; drop; end
		}
		codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00}, // empty struct type
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModule(binary)
	if err == nil {
		err = wasm.ValidateModule(m)
	}
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkGCFrameRootPlanManyCollectingFunctions(b *testing.B) {
	m := gcFrameRootCollectingModule(b, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		gcFrameRootPlanSink = newGCFrameRootPlan(m, true)
	}
}
