//go:build wago_guardpage

package wagobench

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

// wagoJSONGuard sets up x64 with signals-based (guard-page) bounds — the inline
// bounds check is elided and out-of-bounds accesses fault into a trap handler.
func wagoJSONGuard(t *testing.T, wasmBytes []byte) (ser, deser func()) {
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksSignalsBased)
	c, err := wago.CompileWithConfig(cfg, wasmBytes)
	if err != nil {
		t.Fatalf("compile (guard): %v", err)
	}
	in, err := wago.Instantiate(c, wago.Imports{"env.abort": wago.HostFunc(func(int32) {})})
	if err != nil {
		t.Fatalf("instantiate (guard): %v", err)
	}
	if _, err := in.Invoke("_initialize"); err != nil {
		t.Fatalf("_initialize (guard): %v", err)
	}
	ser = func() { in.Invoke("serializeN", uint64(innerN)) }
	deser = func() { in.Invoke("deserializeN", uint64(innerN)) }
	return
}

// TestJsonAsGuardCorrect verifies guard-page mode produces the SAME results as
// explicit bounds (a growing module that lazily commits pages under the fault
// handler must still compute correctly).
func TestJsonAsGuardCorrect(t *testing.T) {
	b := loadJSON(t)
	mk := func(guard bool) *wago.Instance {
		cfg := wago.NewRuntimeConfig()
		if guard {
			cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
		}
		c, err := wago.CompileWithConfig(cfg, b)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		in, err := wago.Instantiate(c, wago.Imports{"env.abort": wago.HostFunc(func(int32) {})})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		in.Invoke("_initialize")
		return in
	}
	ex, gd := mk(false), mk(true)
	for _, n := range []uint64{1, 10, 100, 1000} {
		for _, fn := range []string{"serializeN", "deserializeN"} {
			re, _ := ex.Invoke(fn, n)
			rg, _ := gd.Invoke(fn, n)
			if len(re) != len(rg) || (len(re) == 1 && re[0] != rg[0]) {
				t.Fatalf("%s(%d): explicit=%v guard=%v", fn, n, re, rg)
			}
		}
	}
	t.Logf("guard-page results match explicit across serialize/deserialize")
}

// TestJsonAsGuard compares x64 explicit bounds vs x64 guard-page (elided) bounds.
func TestJsonAsGuard(t *testing.T) {
	b := loadJSON(t)
	const dur = 800 * time.Millisecond

	xSer, xDeser := wagoJSON(t, b)
	gSer, gDeser := wagoJSONGuard(t, b)

	xs, xd := timePerUnit(xSer, dur), timePerUnit(xDeser, dur)
	gs, gd := timePerUnit(gSer, dur), timePerUnit(gDeser, dur)

	fmt.Printf("\njson-as — x64 explicit vs guard-page bounds (ns/op)\n")
	fmt.Printf("%-18s %11s %11s\n", "backend", "serialize", "deserialize")
	fmt.Printf("%-18s %11.1f %11.1f\n", "x64 explicit", xs, xd)
	fmt.Printf("%-18s %11.1f %11.1f\n", "x64 guard-page", gs, gd)
	fmt.Printf("%-18s %10.1f%% %10.1f%%\n", "bounds-check cost", (xs-gs)/xs*100, (xd-gd)/xd*100)
}

// TestMemSumGuard isolates the bounds-check cost on a pure linear-memory load
// loop (corpus memory.sum, no memory.grow so guard mode works).
func TestMemSumGuard(t *testing.T) {
	mb, err := os.ReadFile("corpus/memory.wasm")
	if err != nil {
		t.Skip("corpus/memory.wasm absent")
	}
	const dur = 800 * time.Millisecond
	setup := func(guard bool) func() {
		cfg := wago.NewRuntimeConfig()
		if guard {
			cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
		}
		c, err := wago.CompileWithConfig(cfg, mb)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		in, err := wago.Instantiate(c, nil)
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		return func() { in.Invoke("sum", 512) }
	}
	ex := timePerUnit512(setup(false), dur)
	gd := timePerUnit512(setup(true), dur)
	fmt.Printf("\ncorpus memory.sum(512) — x64 bounds-check cost (ns per sum call)\n")
	fmt.Printf("  x64 explicit:   %.1f\n", ex)
	fmt.Printf("  x64 guard-page: %.1f\n", gd)
	fmt.Printf("  bounds check =  %.1f%%\n", (ex-gd)/ex*100)
}

func timePerUnit512(fn func(), dur time.Duration) float64 {
	for i := 0; i < 20; i++ {
		fn()
	}
	best := 1e18
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		start := time.Now()
		const reps = 200
		for i := 0; i < reps; i++ {
			fn()
		}
		ns := float64(time.Since(start).Nanoseconds()) / float64(reps)
		if ns < best {
			best = ns
		}
	}
	return best
}
