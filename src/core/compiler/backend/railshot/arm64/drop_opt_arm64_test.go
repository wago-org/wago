//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func pureDeferredDropModuleArm64(tb testing.TB, n int) *wasm.Module {
	tb.Helper()
	body := []byte{0x00}
	for range n {
		body = append(body,
			0x20, 0x00,
			0x20, 0x01,
			0x6a,
			0x41, 0x03,
			0x6c,
			0x1a,
		)
	}
	body = append(body, 0x0b)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode: %v", err)
	}
	return m
}

func TestPureDeferredDropEliminationArm64(t *testing.T) {
	// (local.get 0 + local.get 1) * 3 is a side-effect-free deferred tree whose
	// result is immediately dropped. It should emit no ALU instructions.
	m := pureDeferredDropModuleArm64(t, 1)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["pure-drop"]; got != 1 {
		t.Fatalf("pure-drop count = %d, want 1", got)
	}
	if got := stats.Funcs[0].Condenses; got != 0 {
		t.Fatalf("discarded pure tree condenses = %d, want 0", got)
	}
}

func BenchmarkPureDeferredDropsArm64(b *testing.B) {
	m := pureDeferredDropModuleArm64(b, 512)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(cm.Code)), "code-B")
	}
}
