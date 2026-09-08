//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeArrayDefaultBenchmarkModuleN(arrayType []byte, length uint32, count int) []byte {
	funcType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null type 0) local
	for range count {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(length))...)
		body = append(body, 0xfb, 0x07, 0x00, 0x21, 0x00)
	}
	body = append(body, 0x41, 0x00, 0x0b) // constant result keeps timing allocation-only
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
}

func gcNativeArrayDefaultBenchmarkModule(arrayType []byte, length uint32) []byte {
	return gcNativeArrayDefaultBenchmarkModuleN(arrayType, length, 33)
}

func gcNativeReferenceArrayModule() []byte {
	structType := []byte{0x5f, 0x00}      // type 0: final empty struct
	arrayType := []byte{0x5e, 0x6e, 0x01} // type 1: final (array (mut anyref))
	funcType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	fixedBody := []byte{
		0x02, 0x02, 0x63, 0x00, 0x01, 0x63, 0x01, // two child refs and one array ref
		0xfb, 0x01, 0x00, 0x21, 0x00, // child 0
		0xfb, 0x01, 0x00, 0x21, 0x01, // child 1
		0x20, 0x00, 0x20, 0x01, 0xfb, 0x08, 0x01, 0x02, 0x21, 0x02, // first fixed array; slow refill
		0x20, 0x00, 0x20, 0x01, 0xfb, 0x08, 0x01, 0x02, // second fixed array; native
		0xfb, 0x0f, 0x20, 0x02, 0xfb, 0x0f, 0x6a, 0x0b, // sum both lengths
	}
	uniformBody := []byte{
		0x02, 0x01, 0x63, 0x00, 0x01, 0x63, 0x01, // child and array locals
		0xfb, 0x01, 0x00, 0x21, 0x00, // child
		0x20, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x21, 0x01, // first uniform array; slow refill
		0x20, 0x00, 0x41, 0x03, 0xfb, 0x06, 0x01, // second uniform array; native
		0xfb, 0x0f, 0x20, 0x01, 0xfb, 0x0f, 0x6a, 0x0b, // lengths 3 + 2
	}
	fixedCode := append(wasmtest.ULEB(uint32(len(fixedBody))), fixedBody...)
	uniformCode := append(wasmtest.ULEB(uint32(len(uniformBody))), uniformBody...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fixed", 0, 0), wasmtest.ExportEntry("uniform", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fixedCode, uniformCode)),
	)
}

func TestGCNativeArrayAllocAcrossChunksAndCollections(t *testing.T) {
	data, err := hex.DecodeString(stagedGCArrayNumericFixedHex)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 512, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := uint64(0); i < 2000; i++ {
		bits := uint64(math.Float32bits(float32(i)))
		got, err := in.Invoke("set_get", 1, bits)
		if err != nil || len(got) != 1 || got[0] != bits {
			t.Fatalf("iteration %d = %v, %v", i, got, err)
		}
	}
	stats := in.gc.Stats()
	if stats.Allocations != 2001 || stats.MinorCollections == 0 {
		t.Fatalf("collector stats = %+v", stats)
	}
}

func TestGCNativeArrayAllocReferenceInitializers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeReferenceArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 512, StressBarriers: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 1000; i++ {
		got, err := in.Invoke("fixed")
		if err != nil || len(got) != 1 || got[0] != 4 {
			t.Fatalf("fixed iteration %d = %v, %v", i, got, err)
		}
		got, err = in.Invoke("uniform")
		if err != nil || len(got) != 1 || got[0] != 5 {
			t.Fatalf("uniform iteration %d = %v, %v", i, got, err)
		}
	}
	if stats := in.gc.Stats(); stats.Allocations != 7000 || stats.MinorCollections+stats.FullCollections == 0 {
		t.Fatalf("collector stats = %+v", stats)
	}
}

func TestGCNativeArrayAllocTrapCancelsBatch(t *testing.T) {
	compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.gc.PrepareNativeAllocation(32) {
		t.Fatal("failed to prepare native allocation batch")
	}
	if _, err := in.Invoke("len", math.MaxUint32); err == nil {
		t.Fatal("overflowing native array allocation succeeded")
	}
	view := in.gc.NativeView()
	state := unsafe.Slice((*byte)(offHeapPtr(view.StructAllocState)), corergc.NativeStructAllocChunkEndOffset+4)
	if count := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCountOffset:]); count != 0 {
		t.Fatalf("trapping fallback retained %d handles", count)
	}
	if chunkEnd := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocChunkEndOffset:]); chunkEnd != 0 {
		t.Fatalf("trapping fallback retained chunk end %d", chunkEnd)
	}
}

func BenchmarkGCNativeArrayFresh(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModuleN([]byte{0x5e, 0x7f, 0x01}, 4, 1))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := in.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func BenchmarkGCNativeArrayBatchDepth(b *testing.B) {
	for _, count := range []int{1, 2, 4, 8, 16, 33} {
		b.Run(fmt.Sprintf("constructors=%d", count), func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModuleN([]byte{0x5e, 0x7f, 0x01}, 4, count))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 0 {
					b.Fatalf("run = %v, %v", got, err)
				}
			}
		})
	}
}

func BenchmarkGCNativeArrayAllocationMatrix(b *testing.B) {
	fixtures := []struct {
		name      string
		arrayType []byte
	}{
		{name: "numeric", arrayType: []byte{0x5e, 0x7f, 0x01}},
		{name: "reference", arrayType: []byte{0x5e, 0x6e, 0x01}},
	}
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			for _, length := range []uint32{0, 1, 4, 32, 256, 4096} {
				b.Run(fmt.Sprintf("length=%d", length), func(b *testing.B) {
					compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModule(fixture.arrayType, length))
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { _ = compiled.Close() })
					in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 1 << 20}})
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { _ = in.Close() })
					if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 0 {
						b.Fatalf("warmup = %v, %v", got, err)
					}
					b.ReportAllocs()
					b.ReportMetric(float64(length), "elements/array")
					b.ReportMetric(33, "arrays/op")
					b.ResetTimer()
					for range b.N {
						if _, err := in.Invoke("run"); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func TestGCNativeArrayAllocMalformedMetadataFallsBack(t *testing.T) {
	data, err := hex.DecodeString(stagedGCArrayNumericFixedHex)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	seven := uint64(math.Float32bits(7))
	if got, err := in.Invoke("set_get", 1, seven); err != nil || len(got) != 1 || got[0] != seven {
		t.Fatalf("initial allocation = %v, %v", got, err)
	}
	view := in.gc.NativeView()
	max := view.NurseryObjectMaxBytes
	view.NurseryObjectMaxBytes = 0
	nine := uint64(math.Float32bits(9))
	if got, err := in.Invoke("set_get", 1, nine); err != nil || len(got) != 1 || got[0] != nine {
		t.Fatalf("malformed-view fallback = %v, %v", got, err)
	}
	view.NurseryObjectMaxBytes = max
}
