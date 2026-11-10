//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
)

func gcStructReductionBytes(cast, ordinary bool) []byte {
	types := [][]byte{{0x50, 0, 0x5f, 1, 0x7f, 0}}
	source := byte(0)
	if cast {
		types = append(types, []byte{0x50, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0})
		source = 1
	}
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	body := []byte{1, 2, 0x7f}
	if ordinary {
		body = append(body, 1)
	} // same semantics, outside the complete-loop proof
	body = append(body, 0x03, 0x40, 0x20, 2, 0x20, 1)
	if cast {
		body = append(body, 0x41, 7)
	}
	body = append(body, 0xfb, 0, source)
	if cast {
		body = append(body, 0xfb, 22, 0)
	}
	body = append(body, 0xfb, 2, 0, 0, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(uint32(len(types)-1)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCStructReductionExecutionAndAllocation(t *testing.T) {
	for _, cast := range []bool{false, true} {
		optimized, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(cast, false))
		if err != nil {
			t.Fatal(err)
		}
		ordinary, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(cast, true))
		if err != nil {
			t.Fatal(err)
		}
		for _, cfg := range []GCConfig{{}, {StressNurseryBytes: 128, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}, {DisableCollection: true, ThroughputHeapBytes: 64 << 10}} {
			for _, n := range []uint64{0, 1, 2, 15, 1000, 65537} {
				a, err := Instantiate(optimized, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				b, err := Instantiate(ordinary, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				got, gotErr := a.Invoke("run", n)
				want, wantErr := b.Invoke("run", n)
				if (gotErr == nil) != (wantErr == nil) {
					t.Fatalf("cast=%v n=%d config=%+v errors %v / %v", cast, n, cfg, gotErr, wantErr)
				}
				if gotErr == nil && (len(got) != 1 || len(want) != 1 || got[0] != want[0] || got[0] != uint64(uint32(n*(n-1)/2))) {
					t.Fatalf("cast=%v n=%d values %v / %v", cast, n, got, want)
				}
				if x, y := a.gc.Stats().Allocations, b.gc.Stats().Allocations; x != y {
					t.Fatalf("cast=%v n=%d allocations %d / %d", cast, n, x, y)
				}
				a.Close()
				b.Close()
			}
		}
		optimized.Close()
		ordinary.Close()
	}
}

func TestGCStructReductionRepeatedChargesAndCollection(t *testing.T) {
	for _, cast := range []bool{false, true} {
		fast, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(cast, false))
		if err != nil {
			t.Fatal(err)
		}
		defer fast.Close()
		slow, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(cast, true))
		if err != nil {
			t.Fatal(err)
		}
		defer slow.Close()
		for _, cfg := range []GCConfig{{}, {StressNurseryBytes: 256, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true}} {
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
			if !af.gcPrepared || !af.boundedWrapper || bf.boundedWrapper {
				t.Fatalf("unexpected entry admission: fast GC=%v bounded=%v ordinary bounded=%v", af.gcPrepared, af.boundedWrapper, bf.boundedWrapper)
			}
			for round := 0; round < 3; round++ {
				for _, n := range []uint64{1, 1, 2, 0, 7, 64, 1000, 3} {
					got, e := af.Invoke(n)
					want, e2 := bf.Invoke(n)
					if e != nil || e2 != nil || len(got) != 1 || len(want) != 1 || got[0] != want[0] {
						t.Fatalf("cast=%v round=%d n=%d values=%v/%v errors=%v/%v", cast, round, n, got, want, e, e2)
					}
					x, y := a.gc.Stats(), b.gc.Stats()
					if x.Allocations != y.Allocations || x.LiveObjects != y.LiveObjects || x.MinorCollections+x.FullCollections != y.MinorCollections+y.FullCollections {
						t.Fatalf("cast=%v round=%d n=%d stats=%+v / %+v", cast, round, n, x, y)
					}
				}
				for _, in := range []*Instance{a, b} {
					if err := in.gc.CollectMinor(corergc.EmptyRoots{}); err != nil {
						t.Fatal(err)
					}
					if err := in.gc.Verify(corergc.EmptyRoots{}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func TestGCStructReductionPreparedWithGoGC(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructReductionBytes(false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.gcPrepared || !fn.boundedWrapper {
		t.Fatal("missing bounded GC entry")
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()
	defer func() { close(stop); <-done }()
	for i := 0; i < 100; i++ {
		got, err := fn.Invoke(1000)
		if err != nil || len(got) != 1 || got[0] != 499500 {
			t.Fatalf("got=%v err=%v", got, err)
		}
	}
}
