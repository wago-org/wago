//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCDynamicNumericCopy(t *testing.T) {
	testGCDynamicNumericCopy(t)
}

func testGCDynamicNumericCopy(t *testing.T) {
	for _, storage := range []byte{0x78, 0x77, 0x7f, 0x7e, 0x7d, 0x7c} {
		for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
			t.Run(fmt.Sprintf("%x/profile%d", storage, profile), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModule(storage))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				const length = 1100
				if _, err := in.Invoke("init", length); err != nil {
					t.Fatal(err)
				}
				want, src := make([]uint64, length), make([]uint64, length)
				for i := range want {
					bits := uint64(0x7ff012347fa12345) + uint64(i)*0x10203
					want[i], src[i] = gcCopyExpected(storage, bits^0x55aa55aa55aa55aa), gcCopyExpected(storage, bits)
					for _, v := range []struct {
						name string
						bits uint64
					}{{"set-dst", want[i]}, {"set-src", src[i]}} {
						if _, err := in.Invoke(v.name, uint64(i), v.bits); err != nil {
							t.Fatal(err)
						}
					}
				}
				check := func() {
					t.Helper()
					for i, value := range want {
						got, err := in.Invoke("get-dst", uint64(i))
						if err != nil || len(got) != 1 || got[0] != value {
							t.Fatalf("index%d: %x,%v want%x", i, got, err, value)
						}
					}
				}
				for _, count := range []int{0, 1, 2, 3, 4, 5, 15, 16, 17, 63, 64, 65, 127, 128, 129, 511, 512, 513, 1024} {
					if _, err := in.Invoke("copy-dynamic", 2, 3, uint64(count)); err != nil {
						t.Fatal(err)
					}
					copy(want[2:2+count], src[3:3+count])
					check()
				}
				for _, indices := range [][3]int{{1, 0, 1024}, {0, 1, 1024}, {3, 2, 513}, {2, 3, 513}, {7, 7, 65}, {0, 0, 1024}, {length, length, 0}, {2, 1, 17}, {1, 2, 17}} {
					dst, start, count := indices[0], indices[1], indices[2]
					if _, err := in.Invoke("copy-overlap-dynamic", uint64(dst), uint64(start), uint64(count)); err != nil {
						t.Fatal(err)
					}
					copy(want[dst:dst+count], want[start:start+count])
					check()
				}
				// Exercise every attainable alignment of wide destination stores.
				// For i8 this spans all 32 byte alignments in both directions.
				for index := 0; index < 32; index++ {
					if _, err := in.Invoke("copy-dynamic", uint64(index), 35, 1024); err != nil {
						t.Fatal(err)
					}
					copy(want[index:index+1024], src[35:1059])
					check()
					dst, start := index+1, index
					if _, err := in.Invoke("copy-overlap-dynamic", uint64(dst), uint64(start), 1024); err != nil {
						t.Fatal(err)
					}
					copy(want[dst:dst+1024], want[start:start+1024])
					check()
					// Same-array offsets guarantee the forward direction too.
					if _, err := in.Invoke("copy-overlap-dynamic", uint64(start), uint64(dst), 1024); err != nil {
						t.Fatal(err)
					}
					copy(want[start:start+1024], want[dst:dst+1024])
					check()
				}
				for _, args := range [][3]uint64{{length + 1, 0, 0}, {0, length + 1, 0}, {length, 0, 1}, {0, length, 1}, {0xffffffff, 0, 2}, {0, 1, 0xffffffff}, {1, 0xffffffff, 2}} {
					_, err := in.Invoke("copy-dynamic", args[:]...)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
						t.Fatalf("range%v: %v", args, err)
					}
					check()
				}
				if _, err := in.Invoke("copy-dynamic", 0xdeadbeef00000002, 0xcafebabe00000003, 0x1122334400000011); err != nil {
					t.Fatal(err)
				}
				copy(want[2:19], src[3:20])
				check()
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("copy-dynamic", 0, 0, length); err != nil {
					t.Fatal(err)
				}
				copy(want, src)
				check()
				for i := 0; i < 17; i++ {
					src[i] ^= 0x55
					if _, err := in.Invoke("set-src", uint64(i), src[i]); err != nil {
						t.Fatal(err)
					}
				}

				// Destination fits its physical range but source does not. Both
				// validations must finish before even the first destination store.
				for _, global := range []int{0, 1} {
					view := in.gc.NativeView()
					ref := uint32(readGlobalObject(in.globalCells[global], in.c.Globals[global].Type))
					handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
					header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), gc.HeaderSize)
					size := binary.LittleEndian.Uint32(header[4:])
					binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset)
					_, err := in.Invoke("copy-dynamic", 0, 0, 17)
					binary.LittleEndian.PutUint32(header[4:], size)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("short object%d: %v", global, err)
					}
					check()
				}
				if _, err := in.Invoke("clear-src"); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][3]uint64{{length + 1, 0, 1}, {0, 0, 0}} {
					_, err := in.Invoke("copy-dynamic", args[:]...)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapNullReference {
						t.Fatalf("null precedence%v: %v", args, err)
					}
					check()
				}
			})
		}
	}
}

func gcDynamicCopyPressureModuleSites(sites int) []byte {
	values := make([]wasm.ValType, 32)
	for i := range values {
		values[i] = wasm.I64
		if i < 20 {
			values[i] = wasm.F64
		}
	}
	params := append([]wasm.ValType{wasm.I32}, values...)
	results := []wasm.ValType{wasm.EqRef, wasm.EqRef}
	results = append(results, values...)
	results = append(results, values...)
	body := []byte{1, 2, 0x63, 0, 0x23, 0, 0x21, 33, 0x23, 1, 0x21, 34, 0x20, 33, 0x20, 34}
	for i := range values {
		body = append(body, 0x20, byte(i+1))
	}
	for i := 0; i < sites; i++ {
		body = append(body, 0x20, 33, 0x41, 0, 0x20, 34, 0x41, 0, 0x20, 0, 0xfb, 17, 0, 0)
	}
	for i := range values {
		body = append(body, 0x20, byte(i+1))
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x80, 4, 0xfb, 7, 0, 0x24, 0, 0x41, 0x80, 4, 0xfb, 7, 0, 0x24, 1, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCDynamicCopyPreservesLiveValues(t *testing.T) {
	testGCDynamicCopyPreservesLiveValues(t)
}

func testGCDynamicCopyPreservesLiveValues(t *testing.T) {
	for _, sites := range []int{1, 17} {
		t.Run(fmt.Sprint(sites), func(t *testing.T) { testGCDynamicCopyPressureSites(t, sites) })
	}
}

func testGCDynamicCopyPressureSites(t *testing.T, sites int) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDynamicCopyPressureModuleSites(sites))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	host, ok := cachedAMD64CPUFeatures()
	if !ok {
		t.Fatal("CPU feature detection failed")
	}
	if want := selectedAMD64CompileFeatures(host) & shared.AMD64AVX; c.requiredAMD64Features&shared.AMD64AVX != want {
		t.Fatalf("AVX requirement=%x want%x", c.requiredAMD64Features&shared.AMD64AVX, want)
	}
	artifact, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(artifact); err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(&decoded, InstantiateOptions{GC: GCConfig{Profile: profile}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Invoke("init"); err != nil {
			t.Fatal(err)
		}
		args := make([]uint64, 33)
		for i := 1; i < len(args); i++ {
			args[i] = 0x7ff0000000000001 + uint64(i)*0x123456789abc
		}
		for _, count := range []uint64{0, 1, 4, 17, 129, 512} {
			args[0] = count
			got, err := in.Invoke("run", args...)
			if err != nil || len(got) != 66 {
				t.Fatalf("count%d: %x,%v", count, got, err)
			}
			for i := 0; i < 2; i++ {
				entry, ok := in.refStore.gcByToken[got[i]]
				want := readGlobalObject(in.globalCells[i], in.c.Globals[i].Type)
				if !ok || entry.owner != in || uint64(entry.ref) != want {
					t.Fatalf("count%d reference%d: %x", count, i, got[i])
				}
				if err := in.ReleaseGCRef(GCRef{token: got[i]}); err != nil {
					t.Fatal(err)
				}
			}
			for i, value := range got[2:] {
				if value != args[i%32+1] {
					t.Fatalf("count%d value%d: %x want%x", count, i, value, args[i%32+1])
				}
			}
		}
	}
}

func TestGCDynamicCopySSE2(t *testing.T) {
	// SSE2 is guaranteed on AMD64. Compile and execute the supported subset
	// rather than modeling instructions that the physical host cannot execute.
	cachedAMD64CPUFeatures()
	savedFeatures, savedOK := amd64CPUCache.features, amd64CPUCache.ok
	t.Cleanup(func() { amd64CPUCache.features, amd64CPUCache.ok = savedFeatures, savedOK })
	amd64CPUCache.features, amd64CPUCache.ok = 0, true
	t.Run("payload", testGCDynamicNumericCopy)
	t.Run("pressure-and-artifact", testGCDynamicCopyPreservesLiveValues)
}

func BenchmarkGCDynamicNumericCopy(b *testing.B) {
	for _, count := range []int{4, 16, 64, 1024, 16384} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModule(0x7f))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init", uint64(count)); err != nil {
				b.Fatal(err)
			}
			if _, err := in.Invoke("set-src", 0, 42); err != nil {
				b.Fatal(err)
			}
			if got, err := in.Invoke("run-copy", 128, uint64(count)); err != nil || len(got) != 1 || got[0] != 42 {
				b.Fatalf("copy preflight=%v,%v", got, err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(count * 4 * 128))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run-copy", 128, uint64(count))
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("copy=%v,%v", got, err)
				}
			}
		})
	}
}

func BenchmarkGCDynamicNumericCopyCompilation(b *testing.B) {
	data := gcNumericCopyModule(0x7f)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(c.code)), "code-B")
		c.Close()
	}
}

func BenchmarkGCDynamicNumericCopyCompileDensity(b *testing.B) {
	for _, sites := range []int{1, 4, 16, 256} {
		b.Run(fmt.Sprint(sites), func(b *testing.B) {
			body := make([]byte, 0, sites*14+1)
			for i := 0; i < sites; i++ {
				body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0xfb, 17, 0, 0)
			}
			body = append(body, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 5, 0x63, 0, 0x7f, 0x63, 0, 0x7f, 0x7f, 0})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(c.code)), "code-B")
				c.Close()
			}
		})
	}
}

func BenchmarkGCDynamicNumericCopyOverlap(b *testing.B) {
	for direction, name := range []string{"", "backward", "forward", "same"} {
		if direction == 0 {
			continue
		}
		for _, count := range []int{16, 1024, 16384} {
			b.Run(fmt.Sprintf("%s/%d", name, count), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericCopyModuleWithCopyRun(0x7f, direction))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				if _, err := in.Invoke("init", uint64(count+1)); err != nil {
					b.Fatal(err)
				}
				for i := 0; i <= count; i++ {
					if _, err := in.Invoke("set-dst", uint64(i), 42); err != nil {
						b.Fatal(err)
					}
				}
				if got, err := in.Invoke("run-copy", 128, uint64(count)); err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("overlap preflight=%v,%v", got, err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(count * 4 * 128))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err := in.Invoke("run-copy", 128, uint64(count))
					if err != nil || len(got) != 1 || got[0] != 42 {
						b.Fatalf("overlap=%v,%v", got, err)
					}
				}
			})
		}
	}
}
