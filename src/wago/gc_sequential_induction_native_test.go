//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestGCSequentialInductionPendingInterrupt(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"struct", gcStructReductionBytes(false, false)},
		{"array", gcArrayReductionBytes(false)},
		{"fill", gcArrayFillReductionBytes(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = fn.Invoke(1); err != nil {
				t.Fatal(err)
			}
			before := in.gc.Stats()
			rt.RequestInterrupt(in.trap)
			out, err := fn.Invoke(7)
			after := in.gc.Stats()
			t.Logf("out=%v error=%v allocations=%d admission=%d identity=%d", out, err, after.Allocations-before.Allocations, in.invocationState.word.Load(), in.ensurePluginState().invocationID)
			if err == nil {
				t.Error("pending interrupt ignored")
			} else {
				requireInterruptedTrap(t, err)
			}
			if after != before {
				t.Errorf("interrupted call changed collector: %+v -> %+v", before, after)
			}
			if in.invocationState.word.Load() != 0 || in.ensurePluginState().invocationID != 0 {
				t.Error("admission leaked")
			}
			if out, err = fn.Invoke(7); err != nil || len(out) != 1 || out[0] != 21 {
				t.Fatalf("recovery=%v %v", out, err)
			}
		})
	}
}

func TestGCSequentialInductionColdAndRevokedContext(t *testing.T) {
	for _, data := range [][]byte{gcStructReductionBytes(false, false), gcArrayReductionBytes(false), gcArrayFillReductionBytes(false)} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for round := 0; round < 3; round++ {
			if round == 1 {
				fn.hostContextVersion = 0
			}
			if round == 2 {
				in.gc.CancelNativeAllocationBatch()
			}
			for _, n := range []uint64{0, 1, 7, 0xffffffff00000003} {
				before := in.gc.Stats().Allocations
				out, err := fn.Invoke(n)
				x := uint64(uint32(n))
				want := uint64(uint32(x * (x - 1) / 2))
				if err != nil || len(out) != 1 || out[0] != want {
					t.Fatalf("round=%d n=%x result=%v error=%v", round, n, out, err)
				}
				if got := in.gc.Stats().Allocations - before; got != uint64(max(uint32(n), 1)) {
					t.Fatalf("allocation delta=%d", got)
				}
				if in.invocationState.word.Load() != 0 || in.ensurePluginState().invocationID != 0 {
					t.Fatal("admission leaked")
				}
			}
		}
		in.Close()
		c.Close()
	}
}
