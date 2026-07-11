//go:build wago_guardpage

package wagobench

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/wago"
)

// wagoJSONGuard sets up signals-based (guard-page) bounds — the inline
// bounds check is elided and out-of-bounds accesses fault into a trap handler.
func wagoJSONGuard(t *testing.T, wasmBytes []byte) (ser, deser func()) {
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksSignalsBased)
	c, err := wago.Compile(cfg, wasmBytes)
	if err != nil {
		t.Fatalf("compile (guard): %v", err)
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}})
	if err != nil {
		t.Fatalf("instantiate (guard): %v", err)
	}
	if _, err := in.Invoke("_initialize"); err != nil {
		t.Fatalf("_initialize (guard): %v", err)
	}
	ser = func() {
		if _, err := in.Invoke("serializeN", uint64(innerN)); err != nil {
			t.Fatalf("serializeN (guard): %v", err)
		}
	}
	deser = func() {
		if _, err := in.Invoke("deserializeN", uint64(innerN)); err != nil {
			t.Fatalf("deserializeN (guard): %v", err)
		}
	}
	return
}

// TestJsonAsGuardCorrect verifies guard-page mode produces the SAME results as
// explicit bounds (a growing module that lazily commits pages under the fault
// handler must still compute correctly).
func TestJsonAsGuardCorrect(t *testing.T) {
	b := loadJSON(t)
	mk := func(guard bool) *wago.Instance {
		// Force explicit bounds for the baseline so the comparison is stable even
		// if WAGO_BOUNDS is set in the environment.
		cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
		if guard {
			cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
		}
		c, err := wago.Compile(cfg, b)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		if _, err := in.Invoke("_initialize"); err != nil {
			t.Fatalf("_initialize: %v", err)
		}
		return in
	}
	ex, gd := mk(false), mk(true)
	for _, n := range []uint64{1, 10, 100, 1000} {
		for _, fn := range []string{"serializeN", "deserializeN"} {
			re, eErr := ex.Invoke(fn, n)
			rg, gErr := gd.Invoke(fn, n)
			if eErr != nil || gErr != nil {
				t.Fatalf("%s(%d): explicit err=%v guard err=%v", fn, n, eErr, gErr)
			}
			if len(re) != len(rg) || (len(re) == 1 && re[0] != rg[0]) {
				t.Fatalf("%s(%d): explicit=%v guard=%v", fn, n, re, rg)
			}
		}
	}
	t.Logf("guard-page results match explicit across serialize/deserialize")
}

// TestJsonAsGuard compares explicit bounds vs guard-page (elided) bounds.
func TestJsonAsGuard(t *testing.T) {
	b := loadJSON(t)
	const dur = 800 * time.Millisecond

	xSer, xDeser := wagoJSON(t, b)
	gSer, gDeser := wagoJSONGuard(t, b)

	xs, xd := timePerUnit(xSer, dur), timePerUnit(xDeser, dur)
	gs, gd := timePerUnit(gSer, dur), timePerUnit(gDeser, dur)

	fmt.Printf("\njson-as — %s explicit vs guard-page bounds (ns/op)\n", runtime.GOARCH)
	fmt.Printf("%-18s %11s %11s\n", "backend", "serialize", "deserialize")
	fmt.Printf("%-18s %11.1f %11.1f\n", runtime.GOARCH+" explicit", xs, xd)
	fmt.Printf("%-18s %11.1f %11.1f\n", runtime.GOARCH+" guard-page", gs, gd)
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
		cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
		if guard {
			cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
		}
		c, err := wago.Compile(cfg, mb)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		in, err := wago.Instantiate(c, wago.InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		return func() { in.Invoke("sum", 512) }
	}
	ex := timePerUnit512(setup(false), dur)
	gd := timePerUnit512(setup(true), dur)
	fmt.Printf("\ncorpus memory.sum(512) — %s bounds-check cost (ns per sum call)\n", runtime.GOARCH)
	fmt.Printf("  %s explicit:   %.1f\n", runtime.GOARCH, ex)
	fmt.Printf("  %s guard-page: %.1f\n", runtime.GOARCH, gd)
	fmt.Printf("  bounds check =  %.1f%%\n", (ex-gd)/ex*100)
}

var memSumSink uint64

// BenchmarkMemSumBounds is the repeatable counterpart to TestMemSumGuard. It
// reports public Invoke cost and allocations for the same memory-heavy function
// under explicit and signals-based bounds modes on every supported host.
func BenchmarkMemSumBounds(b *testing.B) {
	mb, err := os.ReadFile("corpus/memory.wasm")
	if err != nil {
		b.Skip("corpus/memory.wasm absent")
	}
	for _, tc := range []struct {
		name string
		mode wago.BoundsCheckMode
	}{
		{name: "explicit", mode: wago.BoundsChecksExplicit},
		{name: "guard", mode: wago.BoundsChecksSignalsBased},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := wago.NewRuntimeConfig().WithBoundsChecks(tc.mode)
			c, err := wago.Compile(cfg, mb)
			if err != nil {
				b.Fatal(err)
			}
			in, err := wago.Instantiate(c, wago.InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := in.Invoke("sum", 512)
				if err != nil {
					b.Fatal(err)
				}
				memSumSink = r[0]
			}
		})
	}
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
