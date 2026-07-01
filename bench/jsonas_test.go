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

func loadJSON(t *testing.T) []byte {
	b, err := os.ReadFile(jsonModulePath())
	if err != nil {
		t.Skipf("json-as module not present (set WAGO_JSON_MODULE): %v", err)
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

func wagoJSON(t *testing.T, wasmBytes []byte) (ser, deser func()) {
	c, err := wago.CompileWithConfig(wago.NewRuntimeConfig(), wasmBytes)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := wago.Instantiate(c, wago.Imports{"env.abort": wago.HostFunc(func(int32) {})})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if _, err := in.Invoke("_initialize"); err != nil {
		t.Fatalf("_initialize: %v", err)
	}
	ser = func() { in.Invoke("serializeN", uint64(innerN)) }
	deser = func() { in.Invoke("deserializeN", uint64(innerN)) }
	return
}

func wazeroJSON(t *testing.T, wasmBytes []byte) (ser, deser func(), closer func()) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	// AssemblyScript abort(msg, file, line, col) : (i32,i32,i32,i32) -> ()
	_, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(func(_ context.Context, _, _, _, _ int32) {}).
		Export("abort").Instantiate(ctx)
	if err != nil {
		t.Fatalf("wazero env: %v", err)
	}
	mod, err := r.Instantiate(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("wazero instantiate: %v", err)
	}
	if init := mod.ExportedFunction("_initialize"); init != nil {
		init.Call(ctx)
	}
	sfn := mod.ExportedFunction("serializeN")
	dfn := mod.ExportedFunction("deserializeN")
	ser = func() { sfn.Call(ctx, innerN) }
	deser = func() { dfn.Call(ctx, innerN) }
	return ser, deser, func() { r.Close(ctx) }
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
		{"wago-x64  ", timePerUnit(xSer, dur), timePerUnit(xDeser, dur)},
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
