//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func floatMapModule(t testing.TB, wide bool, packets int, src, dst, start, limit, neighbor uint32) *wasm.Module {
	return floatMapModuleAt(t, wide, packets, src, dst, start, limit, neighbor, 0)
}

func floatMapModuleAt(t testing.TB, wide bool, packets int, src, dst, start, limit, neighbor uint32, localBase byte) *wasm.Module {
	return floatMapModuleOffsets(t, wide, packets, src, dst, start, limit, neighbor, localBase, 0, [2]uint32{})
}

func floatMapModuleOffsets(t testing.TB, wide bool, packets int, src, dst, start, limit, neighbor uint32, localBase byte, loadOffset uint32, storeOffsets [2]uint32) *wasm.Module {
	body := []byte{1, 5 + localBase, 0x7f}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{src, dst, start} {
		constant(x)
		body = append(body, 0x21, byte(i)+localBase)
	}
	// Preserve a carried operand as well as the final address temporaries.
	constant(123)
	body = append(body, 0x03, 0x40)
	size, load, store, add, mul := uint32(4), byte(0x2a), byte(0x38), byte(0x92), byte(0x94)
	if wide {
		size, load, store, add, mul = 8, 0x2b, 0x39, 0xa0, 0xa2
	}
	address := func(base, alias byte, bias uint32) {
		body = append(body, 0x20, base+localBase, 0x20, 2+localBase, 0x6a)
		constant(bias)
		body = append(body, 0x6a, 0x22, alias+localBase)
	}
	for i := 0; i < packets; i++ {
		bias := uint32(i) * size
		address(1, 4, bias)
		address(0, 3, bias)
		body = append(body, load, 0)
		body = append(body, wasmtest.ULEB(loadOffset)...)
		address(0, 3, bias+neighbor)
		body = append(body, load, 0)
		body = append(body, wasmtest.ULEB(loadOffset)...)
		body = append(body, add)
		if wide {
			body = appendF64ConstForCacheTest(body, 1.125)
		} else {
			body = append(body, 0x43)
			var bits [4]byte
			binary.LittleEndian.PutUint32(bits[:], math.Float32bits(1.125))
			body = append(body, bits[:]...)
		}
		body = append(body, mul, store, 0)
		body = append(body, wasmtest.ULEB(storeOffsets[i])...)
	}
	body = append(body, 0x20, 2+localBase)
	constant(uint32(packets) * size)
	body = append(body, 0x6a, 0x22, 2+localBase)
	if limit != 0 {
		constant(limit)
		body = append(body, 0x47)
	}
	body = append(body, 0x0d, 0, 0x0b, 0x20, 2+localBase, 0x73, 0x20, 3+localBase, 0x73, 0x20, 4+localBase, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func runFloatMapMemory(t *testing.T, m *wasm.Module, enabled bool, initial []byte) (uint32, []byte, error, int) {
	return runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": enabled}, initial)
}

func runFloatMapMemoryWith(t *testing.T, m *wasm.Module, options map[string]bool, initial []byte) (uint32, []byte, error, int) {
	t.Helper()
	var stats ModuleStats
	var out *ModuleStats
	if diagnosticsEnabled {
		out = &stats
	}
	cm, err := CompileModuleWith(m, CompileOptions{Stats: out, Optimizations: options})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	copy(jm.CurrentBytes(), initial)
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(coreruntime.TrapBufferBytes)
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	hits := 0
	if diagnosticsEnabled {
		hits = stats.Funcs[0].Peephole["float-map-loop-vector"]
	}
	return binary.LittleEndian.Uint32(results), append([]byte(nil), jm.CurrentBytes()...), err, hits
}

func TestFloatMapLoopRangesAliasesAndScalarTail(t *testing.T) {
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/8; i++ {
		binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i%91-45)*0.125))
	}
	// Include canonical NaNs, infinities, signed zero and subnormals alongside
	// ordinary data. Compare complete bytes against the scalar implementation.
	for i, bits := range []uint64{0x7ff8000000000000, 0xfff8000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x8000000000000000, 1, 0x000fffffffffffff} {
		binary.LittleEndian.PutUint64(initial[8*i:], bits)
	}
	for _, wide := range []bool{false, true} {
		size := uint32(4)
		if wide {
			size = 8
		}
		for _, packets := range []int{1, 2} {
			step := size * uint32(packets)
			for _, length := range []uint32{step, 2 * step, 4 * step, 8 * step, 32 * step, 128, 128 + step, 128 + 2*step, 64 * step} {
				for _, neighbor := range []uint32{0, size, 256} {
					for _, layout := range [][2]uint32{{0, 4096}, {4096, 0}, {4096, 4096}, {4096, 4097}, {4096, 4104}, {65504, 65512}, {0xfffffff0, 4096}, {1, 8193}} {
						for _, countdown := range []bool{false, true} {
							src, dst, start, limit := layout[0], layout[1], uint32(0), length
							if countdown {
								start, limit = 0-length, 0
								src += length
								dst += length
							}
							m := floatMapModule(t, wide, packets, src, dst, start, limit, neighbor)
							want, wantMem, wantErr, _ := runFloatMapMemory(t, m, false, initial)
							got, gotMem, gotErr, hits := runFloatMapMemory(t, m, true, initial)
							if (wantErr == nil) != (gotErr == nil) || wantErr == nil && got != want || !bytes.Equal(gotMem, wantMem) {
								t.Fatalf("wide%t packets%d len%d neighbor%d src%x dst%x countdown%t: got%x/%v want%x/%v memory equal%t", wide, packets, length, neighbor, src, dst, countdown, got, gotErr, want, wantErr, bytes.Equal(gotMem, wantMem))
							}
							if diagnosticsEnabled && hits != 1 {
								t.Fatalf("missing admission: wide%t packets%d len%d", wide, packets, length)
							}
						}
					}
				}
			}
		}
	}
}

// High source indices must neither exhaust candidate cells nor truncate the
// coefficient mask. Return values include the final counter/address temporaries;
// complete memory comparison also covers the store prefix before a later trap.
func TestFloatMapSparseRelevantLocals(t *testing.T) {
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/8; i++ {
		binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i%67-33)*0.125))
	}
	for _, wide := range []bool{false, true} {
		for _, packets := range []int{1, 2} {
			for _, base := range []byte{32, 64, 96} {
				for _, layout := range [][2]uint32{{0, 4096}, {4096, 4096}, {4096, 4097}, {65504, 4096}, {4096, 65504}, {0xfffffff0, 4096}} {
					for _, length := range []uint32{16, 128, 144} {
						low := floatMapModule(t, wide, packets, layout[0], layout[1], 0, length, 0)
						high := floatMapModuleAt(t, wide, packets, layout[0], layout[1], 0, length, 0, base)
						want, wantMem, wantErr, _ := runFloatMapMemory(t, low, false, initial)
						got, gotMem, gotErr, hits := runFloatMapMemoryWith(t, high, map[string]bool{"dot-loop-vector": true, "interval-region-pins": false}, initial)
						if (wantErr == nil) != (gotErr == nil) || wantErr == nil && got != want || !bytes.Equal(gotMem, wantMem) {
							t.Fatalf("wide%t packets%d base%d layout%v length%d: got%x/%v want%x/%v memory%t", wide, packets, base, layout, length, got, gotErr, want, wantErr, bytes.Equal(gotMem, wantMem))
						}
						if diagnosticsEnabled && hits != 1 {
							t.Fatalf("missing sparse-local admission: wide%t packets%d base%d", wide, packets, base)
						}
					}
				}
			}
		}
	}
}

func TestFloatMapHighFloatingCoefficient(t *testing.T) {
	for _, wide := range []bool{false, true} {
		floatType, width, load, store, mul := byte(0x7d), uint32(4), byte(0x2a), byte(0x38), byte(0x94)
		if wide {
			floatType, width, load, store, mul = 0x7c, 8, 0x2b, 0x39, 0xa2
		}
		body := []byte{2, 101, 0x7f, 1, floatType}
		if wide {
			body = appendF64ConstForCacheTest(body, 1.125)
		} else {
			body = append(body, 0x43, 0, 0, 0x90, 0x3f)
		}
		body = append(body, 0x21, 101, 0x41, 0, 0x21, 98, 0x41, 0x80, 0x20, 0x21, 97)
		body = append(body, 0x03, 0x40, 0x20, 97, 0x20, 98, 0x6a, 0x20, 98, 0x20, 96, 0x6a, load, 0, 0, 0x20, 101, mul, store, 0, 0)
		body = append(body, 0x20, 98, 0x41, byte(width), 0x6a, 0x22, 98, 0x41, 0x80, 1, 0x47, 0x0d, 0, 0x0b, 0x20, 98, 0x0b)
		m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
		initial := make([]byte, 65536)
		for i := 0; i < 128/int(width); i++ {
			if wide {
				binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i)-3.25))
			} else {
				binary.LittleEndian.PutUint32(initial[4*i:], math.Float32bits(float32(i)-3.25))
			}
		}
		want, wantMem, wantErr, _ := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": false, "interval-region-pins": false}, initial)
		got, gotMem, gotErr, hits := runFloatMapMemoryWith(t, m, map[string]bool{"dot-loop-vector": true, "interval-region-pins": false}, initial)
		if wantErr != nil || gotErr != nil || got != want || !bytes.Equal(gotMem, wantMem) {
			t.Fatalf("wide%t: got%x/%v want%x/%v memory%t", wide, got, gotErr, want, wantErr, bytes.Equal(gotMem, wantMem))
		}
		if diagnosticsEnabled && hits != 1 {
			t.Fatalf("missing high floating-coefficient admission: wide%t", wide)
		}
	}
}

func TestFloatMapLocalBudgetLeavesReaderUnchanged(t *testing.T) {
	f := fn{nLocals: 128, localType: make([]machineType, 128)}
	for i := range f.localType {
		f.localType[i] = mtI32
	}
	// Keep the operand stack empty while introducing distinct mutable cells.
	var body []byte
	for i := 0; i < 33; i++ {
		body = append(body, 0x41, 0, 0x21, byte(64+i))
	}
	r := wasm.NewReader(body)
	if _, ok := f.inspectFloatMapLoop(r); ok {
		t.Fatal("accepted exhausted relevant-local budget")
	}
	if r.Offset() != 0 {
		t.Fatalf("probe advanced original reader to %d", r.Offset())
	}
	if allocs := testing.AllocsPerRun(100, func() { f.inspectFloatMapLoop(r) }); allocs != 0 {
		t.Fatalf("rejected probe allocated %.0f objects", allocs)
	}
}

func TestFloatMapMemoryImmediatesAndWrap(t *testing.T) {
	initial := make([]byte, 65536)
	for i := 0; i < len(initial)/8; i++ {
		binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i%73-36)*0.125))
	}
	for _, wide := range []bool{false, true} {
		for _, packets := range []int{1, 2} {
			for _, offsets := range [][3]uint32{{12, 20, 20}, {0, 16, 16}, {16, 0, 0}, {16, 16, 8}, {65520, 0, 0}, {0, 65520, 65520}, {1 << 20, 0, 0}, {1<<20 + 1, 0, 0}, {0xffffffff, 0, 0}, {0, 0xffffffff, 0xffffffff}} {
				for _, layout := range [][3]uint32{{0, 4096, 0}, {4096, 4096, 0}, {0xfffffff0, 4096, 0}, {0xfffffff0, 4096, 16}, {65504, 4096, 0}, {4096, 65504, 0}} {
					for _, length := range []uint32{16, 128, 144} {
						m := floatMapModuleOffsets(t, wide, packets, layout[0], layout[1], layout[2], layout[2]+length, 0, 0, offsets[0], [2]uint32{offsets[1], offsets[2]})
						want, wantMem, wantErr, _ := runFloatMapMemory(t, m, false, initial)
						got, gotMem, gotErr, hits := runFloatMapMemory(t, m, true, initial)
						if (wantErr == nil) != (gotErr == nil) || wantErr == nil && got != want || !bytes.Equal(gotMem, wantMem) {
							t.Fatalf("wide%t packets%d offsets%v layout%v length%d: got%x/%v want%x/%v memory%t", wide, packets, offsets, layout, length, got, gotErr, want, wantErr, bytes.Equal(gotMem, wantMem))
						}
						admitted := offsets[0] <= 1<<20 && offsets[1] <= 1<<20 && (packets == 1 || offsets[1] == offsets[2] && offsets[2] <= 1<<20)
						if diagnosticsEnabled && (hits == 1) != admitted {
							t.Fatalf("admission wide%t packets%d offsets%v: hits%d want%t", wide, packets, offsets, hits, admitted)
						}
					}
				}
			}
		}
	}
}

func TestFloatMapInvariantMemoryImmediate(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width, load, store, mul := uint32(4), byte(0x2a), byte(0x38), byte(0x94)
		if wide {
			width, load, store, mul = 8, 0x2b, 0x39, 0xa2
		}
		initial := make([]byte, 65536)
		for i := 0; i < len(initial)/int(width); i++ {
			if wide {
				binary.LittleEndian.PutUint64(initial[8*i:], math.Float64bits(float64(i%37+1)*0.125))
			} else {
				binary.LittleEndian.PutUint32(initial[4*i:], math.Float32bits(float32(i%37+1)*0.125))
			}
		}
		for _, tc := range [][4]uint32{{100, 0, 16, 4096}, {100, 0, 16, 112}, {100, 0, 16, 116}, {65520, 0, 12, 4096}, {0xfffffff0, 0, 32, 4096}, {0xfffffff0, 32, 4, 4096}, {100, 0, 1<<20 + 1, 4096}} {
			body := []byte{1, 4, 0x7f}
			constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
			constant(tc[3])
			body = append(body, 0x21, 0)
			constant(tc[0])
			body = append(body, 0x21, 3)
			body = append(body, 0x03, 0x40, 0x20, 0, 0x20, 2, 0x6a, 0x20, 1, 0x20, 2, 0x6a, load, 0, 0, 0x20, 3)
			constant(tc[1])
			body = append(body, 0x6a, load, 0)
			body = append(body, wasmtest.ULEB(tc[2])...)
			body = append(body, mul, store, 0, 0, 0x20, 2)
			constant(width)
			body = append(body, 0x6a, 0x22, 2)
			constant(128)
			body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
			m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
			want, wantMem, wantErr, _ := runFloatMapMemory(t, m, false, initial)
			got, gotMem, gotErr, hits := runFloatMapMemory(t, m, true, initial)
			if (wantErr == nil) != (gotErr == nil) || wantErr == nil && got != want || !bytes.Equal(gotMem, wantMem) {
				t.Fatalf("wide%t case%v: got%x/%v want%x/%v memory%t", wide, tc, got, gotErr, want, wantErr, bytes.Equal(gotMem, wantMem))
			}
			if diagnosticsEnabled && (hits == 1) != (tc[2] <= 1<<20) {
				t.Fatalf("invariant immediate admission wide%t case%v hits%d", wide, tc, hits)
			}
		}
	}
}
