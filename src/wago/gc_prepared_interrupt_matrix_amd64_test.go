//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestGCPreparedPendingInterruptMatrix(t *testing.T) {
	for _, kind := range []string{"struct", "array", "fill"} {
		for _, mode := range []string{"cold", "warm", "revoked"} {
			t.Run(fmt.Sprintf("%s/%s", kind, mode), func(t *testing.T) {
				data := gcStructReductionBytes(false, false)
				want := uint64(21)
				switch kind {
				case "array":
					data = gcArrayReductionBytes(false)
				case "fill":
					data = gcArrayFillReductionBytes(false)
				}
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
				c, err := Compile(cfg, data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				opts := InstantiateOptions{}
				in, err := Instantiate(c, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				if mode != "cold" {
					if _, err = fn.Invoke(1); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "revoked" {
					fn.hostContextVersion = 0
				}
				t.Logf("private=%v lifetime=%v cached=%v", fn.gcRevocableContext, fn.gcLifetimeScalar, fn.hostPrepared != nil)
				before := in.gc.Stats()
				rt.RequestInterrupt(in.trap)
				var out []uint64
				out, err = fn.Invoke(7)
				if err == nil {
					t.Errorf("interrupt ignored: %v", out)
				} else {
					requireInterruptedTrap(t, err)
				}
				if after := in.gc.Stats(); after != before {
					t.Errorf("collector changed: %+v -> %+v", before, after)
				}
				if in.invocationState.word.Load() != 0 || in.ensurePluginState().invocationID != 0 {
					t.Fatal("gate/identity leaked")
				}
				out, err = fn.Invoke(7)
				if err != nil || len(out) != 1 || out[0] != want {
					t.Fatalf("recovery: %v %v", out, err)
				}
			})
		}
	}
}
func TestGCAdjacentLaterChargeCancellation(t *testing.T) {
	for _, cfg := range []GCConfig{{}, {NurseryBytes: 1 << 20, NurseryRegionShift: 6}} {
		t.Run(fmt.Sprintf("shift%d", cfg.NurseryRegionShift), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(4, 1, "only"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err = in.Invoke("leaf"); err != nil {
				t.Fatal(err)
			}
			if !in.gc.PrepareNativeAllocation(80) {
				t.Fatal("reservation unavailable")
			}
			before := in.gc.Stats().Allocations
			if _, err = in.Invoke("leaf"); err != nil {
				t.Fatal(err)
			}
			if got := in.gc.Stats().Allocations - before; got != 4 {
				t.Fatalf("charges=%d", got)
			}
			charged := lifetimeSnapshot(in)
			in.gc.CancelNativeAllocationBatch()
			after := lifetimeSnapshot(in)
			if after.bump != charged.bump || after.allocations != charged.allocations || after.dead != charged.dead {
				t.Fatalf("cancel changed later charges %+v -> %+v", charged, after)
			}
			if _, err = in.Invoke("leaf"); err != nil {
				t.Fatal(err)
			}
			if in.gc.Stats().Allocations != before+8 {
				t.Fatal("subsequent allocation accounting")
			}
		})
	}
}
