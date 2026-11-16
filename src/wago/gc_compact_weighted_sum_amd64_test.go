//go:build linux && amd64 && (wago_gc_compact_weighted_sum || wago_gc_direct_weighted_sum || wago_gc_compact_weighted_sse2 || wago_gc_direct_weighted_sse2)

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func weightedLoopInstance(t *testing.T, data []byte) *Instance {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	compact := c.gcCompactNativeReceipt() != 0
	if compact {
		if c.gcTrustedDirectVersion != 0 {
			t.Fatal("mixed receipt")
		}
	} else if c.gcTrustedDirectVersion != 6 || c.gcDirectReadABIReceipt() == 0 {
		t.Fatal("missing existing trusted direct receipt")
	}
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	config := GCConfig{ExperimentalCompactOffsets: compact, ExperimentalDirectOffsets: !compact, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: config})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func compactWeightedModule(weight, bound int32, pattern ...uint32) []byte {
	body := []byte{1, 1, 0x63, 0, 0x20, 3, 0x20, 0, 0xfb, 6, 0, 0x21, 4}
	for i, value := range pattern {
		body = append(body, 0x20, 4, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i))...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(value))...)
		body = append(body, 0xfb, 14, 0)
	}
	body = append(body, 0x03, 0x40, 0x20, 2, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(weight)...)
	body = append(body, 0x6a, 0x20, 4, 0x20, 1, 0xfb, 11, 0, 0x6c, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41)
	body = append(body, wasmtest.SLEB32(bound)...)
	body = append(body, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}
func TestCompactNativeWeightedSum(t *testing.T) {
	for _, weight := range []int32{1, -7, 0x7fffffff, -0x80000000} {
		for _, bound := range []int32{1, 7, 32} {
			in := weightedLoopInstance(t, compactWeightedModule(weight, bound))
			for _, first := range []uint32{0, uint32(bound) - 1, uint32(bound)} {
				for _, value := range []uint32{7, 0xffffffff, 0x7fffffff, 0x80000001} {
					index, sum := first, uint32(0xfffffff0)
					for {
						sum += (index + uint32(weight)) * value
						index++
						if index >= uint32(bound) {
							break
						}
					}
					got, e := in.Invoke("run", uint64(bound)+2, uint64(first), 0xfffffff0, uint64(value))
					if e != nil || len(got) != 2 || got[0] != uint64(index) || got[1] != uint64(sum) {
						t.Fatalf("weight%d bound%d first%d value%x: %v %v want%d,%x", weight, bound, first, value, got, e, index, sum)
					}
				}
			}
			for _, length := range []uint64{0, uint64(bound) - 1} {
				if _, e := in.Invoke("run", length, 0, 1, 7); e == nil {
					t.Fatalf("missing bounds weight%d bound%d length%d", weight, bound, length)
				}
			}
		}
	}
}

func compactWeightedRepeatedModule() []byte {
	// First and third loops match; the middle signed comparison deliberately
	// does not. Its different weight and bound expose a stale certificate.
	body := []byte{1, 1, 0x63, 0, 0x20, 3, 0x20, 0, 0xfb, 6, 0, 0x21, 4}
	for i, spec := range []struct {
		weight, bound int32
		compare       byte
	}{{3, 7, 0x49}, {-5, 11, 0x48}, {9, 5, 0x49}} {
		if i != 0 {
			body = append(body, 0x41, 0, 0x21, 1)
		}
		body = append(body, 0x03, 0x40, 0x20, 2, 0x20, 1, 0x41)
		body = append(body, wasmtest.SLEB32(spec.weight)...)
		body = append(body, 0x6a, 0x20, 4, 0x20, 1, 0xfb, 11, 0, 0x6c, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41)
		body = append(body, wasmtest.SLEB32(spec.bound)...)
		body = append(body, spec.compare, 0x0d, 0, 0x0b)
	}
	body = append(body, 0x20, 1, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestCompactNativeWeightedMultipleLoops(t *testing.T) {
	in := weightedLoopInstance(t, compactWeightedRepeatedModule())
	for repeat := 0; repeat < 24; repeat++ {
		value := uint32(0x80000001) + uint32(repeat)
		want := uint32(0xfffffff0)
		for _, spec := range []struct {
			weight int32
			bound  uint32
		}{{3, 7}, {-5, 11}, {9, 5}} {
			for i := uint32(0); i < spec.bound; i++ {
				want += (i + uint32(spec.weight)) * value
			}
		}
		got, err := in.Invoke("run", 16, 0, 0xfffffff0, uint64(value))
		if err != nil || len(got) != 2 || got[0] != 5 || got[1] != uint64(want) {
			t.Fatalf("repeat%d: got%v %v want5,%x", repeat, got, err, want)
		}
	}
	// First loop succeeds; the nonmatching middle loop must still trap at
	// its own longer span, not reuse the first loop's shorter bound.
	if _, err := in.Invoke("run", 7, 0, 1, 3); err == nil {
		t.Fatal("middle loop lost its late bounds trap")
	}
}
