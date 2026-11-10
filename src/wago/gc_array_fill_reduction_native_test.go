//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcArrayFillReductionBytes(ordinary bool) []byte {
	body := []byte{2, 2, 0x7f, 1, 0x63, 0}
	if ordinary {
		body = append(body, 1)
	}
	body = append(body, 0x03, 0x40, 0x20, 2, 0x02, 0x7f,
		0x41, 0, 0x41, 16, 0xfb, 6, 0, 0x21, 3,
		0x20, 3, 0x41, 0, 0x20, 1, 0x41, 16, 0xfb, 16, 0,
		0x20, 3, 0x20, 1, 0x41, 15, 0x71, 0xfb, 11, 0, 0x0b, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCArrayFillReductionRepeatedCharges(t *testing.T) {
	fast, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayFillReductionBytes(false))
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	slow, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayFillReductionBytes(true))
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	for _, cfg := range []GCConfig{{}, {StressNurseryBytes: 256, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}, {DisableCollection: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}} {
		a, err := Instantiate(fast, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		b, err := Instantiate(slow, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		af, err := a.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		bf, err := b.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		if !af.boundedWrapper || bf.boundedWrapper {
			t.Fatal("wrong reduction admission")
		}
		for round := 0; round < 3; round++ {
			for _, n := range []uint64{0, 1, 1, 2, 7, 64, 1000, 4096, 3} {
				got, ge := af.Invoke(n)
				want, we := bf.Invoke(n)
				if (ge == nil) != (we == nil) {
					t.Fatalf("n=%d config=%+v errors=%v/%v", n, cfg, ge, we)
				}
				if ge == nil && (len(got) != 1 || len(want) != 1 || got[0] != want[0] || got[0] != uint64(uint32(n*(n-1)/2))) {
					t.Fatalf("n=%d result=%v/%v", n, got, want)
				}
				x, y := a.gc.Stats(), b.gc.Stats()
				if x.Allocations != y.Allocations {
					t.Fatalf("n=%d config=%+v allocations=%+v/%+v", n, cfg, x, y)
				}
				// Ordinary native arrays reserve payload chunks; the reduction
				// removes those gaps. Pressure collection timing can consequently
				// differ. Policies that disable native reservations remain exact.
				if cfg.CollectEveryAlloc || cfg.DisableCollection || cfg.Profile == GCProfileTiny {
					if x.LiveObjects != y.LiveObjects || x.MinorCollections+x.FullCollections != y.MinorCollections+y.FullCollections {
						t.Fatalf("n=%d config=%+v stats=%+v/%+v", n, cfg, x, y)
					}
				}
				if ge != nil {
					break
				}
			}
			for _, in := range []*Instance{a, b} {
				if err := in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
				if in.gc.Stats().LiveObjects != 0 {
					t.Fatal("dead array survived full collection")
				}
				if err := in.gc.Verify(gc.EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
