//go:build (linux && amd64) || ((linux || darwin) && arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

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
