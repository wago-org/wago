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
	body := []byte{1, 5, 0x7f}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{src, dst, start} {
		constant(x)
		body = append(body, 0x21, byte(i))
	}
	// Preserve a carried operand as well as the final address temporaries.
	constant(123)
	body = append(body, 0x03, 0x40)
	size, load, store, add, mul := uint32(4), byte(0x2a), byte(0x38), byte(0x92), byte(0x94)
	if wide {
		size, load, store, add, mul = 8, 0x2b, 0x39, 0xa0, 0xa2
	}
	address := func(base, alias byte, bias uint32) {
		body = append(body, 0x20, base, 0x20, 2, 0x6a)
		constant(bias)
		body = append(body, 0x6a, 0x22, alias)
	}
	for i := 0; i < packets; i++ {
		bias := uint32(i) * size
		address(1, 4, bias)
		address(0, 3, bias)
		body = append(body, load, 0, 0)
		address(0, 3, bias+neighbor)
		body = append(body, load, 0, 0, add)
		if wide {
			body = appendF64ConstForCacheTest(body, 1.125)
		} else {
			body = append(body, 0x43)
			var bits [4]byte
			binary.LittleEndian.PutUint32(bits[:], math.Float32bits(1.125))
			body = append(body, bits[:]...)
		}
		body = append(body, mul, store, 0, 0)
	}
	body = append(body, 0x20, 2)
	constant(uint32(packets) * size)
	body = append(body, 0x6a, 0x22, 2)
	if limit != 0 {
		constant(limit)
		body = append(body, 0x47)
	}
	body = append(body, 0x0d, 0, 0x0b, 0x20, 2, 0x73, 0x20, 3, 0x73, 0x20, 4, 0x73, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func runFloatMapMemory(t *testing.T, m *wasm.Module, enabled bool, initial []byte) (uint32, []byte, error, int) {
	t.Helper()
	var stats ModuleStats
	var out *ModuleStats
	if diagnosticsEnabled {
		out = &stats
	}
	cm, err := CompileModuleWith(m, CompileOptions{Stats: out, Optimizations: map[string]bool{"dot-loop-vector": enabled}})
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
