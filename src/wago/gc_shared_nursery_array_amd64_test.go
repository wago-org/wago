//go:build amd64 && linux

package wago

import (
	"encoding/binary"
	"os"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// A struct refill must also serve the arrays between live structs. No helper
// may cancel that reservation while its nursery space and identities fit.
func TestGCNativeArraysConsumeStructReservation(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-shared-nursery-array.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, name := range []string{"alternating", "alternating-default", "alternating-fill", "alternating-fill-dynamic"} {
		t.Run(name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			const n = 2
			got, err := in.Invoke(name, n)
			if err != nil || len(got) != 1 || got[0] != sharedNurseryArrayWant(name, n) {
				t.Fatalf("%s(%d) = %v, %v", name, n, got, err)
			}
			state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().StructAllocState)), gc.NativeStructAllocStateSize)
			cursor := binary.LittleEndian.Uint32(state[gc.NativeStructAllocCursorOffset:])
			end := binary.LittleEndian.Uint32(state[gc.NativeStructAllocChunkEndOffset:])
			if cursor != 2*n-1 || end != 0 {
				t.Fatalf("reservation cursor/chunk end = %d/%d, want %d/0; mixed constructors entered a helper", cursor, end, 2*n-1)
			}
			if count := in.gc.Stats().Allocations; count != 2*n {
				t.Fatalf("allocations = %d, want %d", count, 2*n)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkGCSharedNurseryArrays(b *testing.B) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-shared-nursery-array.wasm")
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	for _, name := range []string{"alternating", "alternating-default", "alternating-fill", "alternating-fill-dynamic"} {
		b.Run(name, func(b *testing.B) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			const n = 8192
			if got, err := in.Invoke(name, n); err != nil || len(got) != 1 || got[0] != sharedNurseryArrayWant(name, n) {
				b.Fatalf("warm %s=%v,%v", name, got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke(name, n)
				if err != nil || len(got) != 1 || got[0] != sharedNurseryArrayWant(name, n) {
					b.Fatalf("%s(%d) = %v, %v", name, n, got, err)
				}
			}
		})
	}
}

func sharedNurseryArrayWant(name string, n uint64) uint64 {
	switch name {
	case "alternating":
		return uint64(uint32(n * n))
	case "alternating-default":
		return uint64(uint32(n * (n - 1) / 2))
	case "alternating-fill", "alternating-fill-dynamic":
		return uint64(uint32(n * (n - 1)))
	default:
		panic("unknown shared-nursery array benchmark: " + name)
	}
}

func TestGCDynamicUniformArrayBoundaries(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-shared-nursery-array.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{NurseryBytes: 4096, VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, VerifyAfterCollect: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			for _, n := range []uint64{0, 1, 4, 60, 61, 4096} {
				for _, value := range []uint64{0, 7, 0xffffffff} {
					got, err := in.Invoke("dynamic-fill-check", n, value)
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("dynamic-fill-check(%d,%d)=%v,%v", n, value, got, err)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				}
			}
		}()
	}
}
