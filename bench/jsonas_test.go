package wagobench

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/wago-org/wago/src/wago"
)

const innerN = 256 // serialize/deserialize iterations per Invoke (amortizes call overhead)

// jsonModulePath is the AssemblyScript json-as bench module (serializeN/
// deserializeN/_initialize exports). Override with WAGO_JSON_MODULE; these tests
// skip when it is absent.
func jsonModulePath() string {
	if p := os.Getenv("WAGO_JSON_MODULE"); p != "" {
		return p
	}
	return os.Getenv("HOME") + "/Code/AssemblyScript/json-as/build/wago-bench.swar.wasm"
}

func loadJSON(tb testing.TB) []byte {
	b, err := os.ReadFile(jsonModulePath())
	if err != nil {
		tb.Skipf("json-as module not present (set WAGO_JSON_MODULE): %v", err)
	}
	return b
}

// timePerUnit runs fn (one Invoke doing innerN units) repeatedly for ~dur and
// returns nanoseconds per single serialize/deserialize.
func timePerUnit(fn func(), dur time.Duration) float64 {
	// warm up
	for i := 0; i < 20; i++ {
		fn()
	}
	best := 1e18
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		start := time.Now()
		const reps = 50
		for i := 0; i < reps; i++ {
			fn()
		}
		ns := float64(time.Since(start).Nanoseconds()) / float64(reps*innerN)
		if ns < best {
			best = ns
		}
	}
	return best
}

func wagoJSON(tb testing.TB, wasmBytes []byte) (ser, deser func()) {
	// Force explicit bounds so this baseline is deterministic regardless of the
	// WAGO_BOUNDS environment default.
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	c, err := wago.Compile(cfg, wasmBytes)
	if err != nil {
		tb.Fatalf("compile: %v", err)
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}})
	if err != nil {
		tb.Fatalf("instantiate: %v", err)
	}
	if _, err := in.Invoke("_initialize"); err != nil {
		tb.Fatalf("_initialize: %v", err)
	}
	ser = func() {
		if _, err := in.Invoke("serializeN", uint64(innerN)); err != nil {
			tb.Fatalf("serializeN: %v", err)
		}
	}
	deser = func() {
		if _, err := in.Invoke("deserializeN", uint64(innerN)); err != nil {
			tb.Fatalf("deserializeN: %v", err)
		}
	}
	return
}

func wazeroJSON(tb testing.TB, wasmBytes []byte) (ser, deser func(), closer func()) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	// AssemblyScript abort(msg, file, line, col) : (i32,i32,i32,i32) -> ()
	_, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(_ context.Context, _, _, _, _ int32) {}).
		Export("abort").Instantiate(ctx)
	if err != nil {
		tb.Fatalf("wazero env: %v", err)
	}
	mod, err := r.Instantiate(ctx, wasmBytes)
	if err != nil {
		tb.Fatalf("wazero instantiate: %v", err)
	}
	if init := mod.ExportedFunction("_initialize"); init != nil {
		init.Call(ctx)
	}
	sfn := mod.ExportedFunction("serializeN")
	dfn := mod.ExportedFunction("deserializeN")
	ser = func() {
		if _, err := sfn.Call(ctx, innerN); err != nil {
			tb.Fatalf("wazero serializeN: %v", err)
		}
	}
	deser = func() {
		if _, err := dfn.Call(ctx, innerN); err != nil {
			tb.Fatalf("wazero deserializeN: %v", err)
		}
	}
	return ser, deser, func() { r.Close(ctx) }
}

// json-as as real benchmarks, so the chart tooling (which parses
// Benchmark<Name>_wago / _wazero ns/op lines) picks it up alongside the compute
// benchmarks. Each Invoke runs innerN serialize/deserialize units; the loop steps
// b.N by innerN so the reported ns/op is per single serialize/deserialize (call
// overhead amortized). Skips when the json-as module is absent.
func benchJSONUnit(b *testing.B, fn func()) {
	b.ResetTimer()
	for i := 0; i < b.N; i += innerN {
		fn()
	}
}

func BenchmarkJsonAsSerialize_wago(b *testing.B) {
	ser, _ := wagoJSON(b, loadJSON(b))
	benchJSONUnit(b, ser)
}

func BenchmarkJsonAsDeserialize_wago(b *testing.B) {
	_, deser := wagoJSON(b, loadJSON(b))
	benchJSONUnit(b, deser)
}

func BenchmarkJsonAsSerialize_wazero(b *testing.B) {
	ser, _, closer := wazeroJSON(b, loadJSON(b))
	defer closer()
	benchJSONUnit(b, ser)
}

func BenchmarkJsonAsDeserialize_wazero(b *testing.B) {
	_, deser, closer := wazeroJSON(b, loadJSON(b))
	defer closer()
	benchJSONUnit(b, deser)
}

// TestJsonAsBench times json-as serialize/deserialize across the three backends.
func TestJsonAsBench(t *testing.T) {
	b := loadJSON(t)
	const dur = 800 * time.Millisecond

	xSer, xDeser := wagoJSON(t, b)
	wSer, wDeser, wClose := wazeroJSON(t, b)
	defer wClose()

	type row struct {
		name       string
		ser, deser float64
	}
	rows := []row{
		{"wago-amd64  ", timePerUnit(xSer, dur), timePerUnit(xDeser, dur)},
		{"wazero    ", timePerUnit(wSer, dur), timePerUnit(wDeser, dur)},
	}
	fmt.Printf("\njson-as (SWAR) — ns per operation (lower is better)\n")
	fmt.Printf("%-12s %12s %12s\n", "backend", "serialize", "deserialize")
	for _, r := range rows {
		fmt.Printf("%-12s %11.1f  %11.1f\n", r.name, r.ser, r.deser)
	}
	// relative to wazero
	wz := rows[1]
	fmt.Printf("\nrelative to wazero (>1 = faster than wazero):\n")
	for _, r := range rows[:1] {
		fmt.Printf("%-12s ser %.2fx  deser %.2fx\n", r.name, wz.ser/r.ser, wz.deser/r.deser)
	}
}
