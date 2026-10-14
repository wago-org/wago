//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func absReduceModule(t testing.TB, lanes int, reverse, localStride bool, src, dst, rows, index, stride uint32) *wasm.Module {
	body := []byte{1, 9, 0x7f}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for i, x := range []uint32{src, dst, rows, index, 0x87654321} {
		constant(x)
		body = append(body, 0x21, byte(i))
	}
	constant(stride)
	body = append(body, 0x21, 8)
	constant(123)
	body = append(body, 0x03, 0x40)
	for i := 0; i < lanes; i++ {
		off := uint32(i * 4)
		if reverse {
			off = uint32((lanes - 1 - i) * 4)
		}
		for base := byte(0); base < 2; base++ {
			body = append(body, 0x20, base, 0x20, 3, 0x6a)
			constant(off)
			body = append(body, 0x6a, 0x22, 5+base, 0x28, 2, 0)
		}
		body = append(body, 0x6b, 0x22, 7, 0x20, 7)
		constant(31)
		body = append(body, 0x75, 0x22, 7, 0x73, 0x20, 7, 0x6b)
	}
	body = append(body, 0x20, 4)
	for i := 0; i < lanes; i++ {
		body = append(body, 0x6a)
	}
	body = append(body, 0x21, 4, 0x20, 3)
	if localStride {
		body = append(body, 0x20, 8)
	} else {
		constant(stride)
	}
	body = append(body, 0x6a, 0x21, 3, 0x20, 2)
	constant(0xffffffff)
	body = append(body, 0x6a, 0x22, 2, 0x0d, 0, 0x0b)
	for _, local := range []byte{2, 3, 4, 5, 6, 7} {
		body = append(body, 0x20, local, 0x73)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestAbsReduceIndependentOverflowAndFallback(t *testing.T) {
	initial := make([]byte, 65536)
	seeds := []uint32{0, 1, 0xffffffff, 0x80000000, 0x7fffffff, 0x12345678, 0x87654321}
	for i := 0; i < len(initial)/4; i++ {
		binary.LittleEndian.PutUint32(initial[4*i:], seeds[i%len(seeds)])
	}
	for _, lanes := range []int{4, 8} {
		for _, reverse := range []bool{false, true} {
			for _, localStride := range []bool{false, true} {
				for _, rows := range []uint32{1, 2, 8, 31} {
					for _, stride := range []uint32{0, 4, 32, 192, 0xfffffffc} {
						for _, layout := range [][3]uint32{{0, 4096, 0}, {4096, 0, 0}, {1, 4097, 0}, {0, 0, 0}, {65504, 0, 0}, {0xfffffff0, 4096, 0}, {64, 128, 0xfffffff0}, {0, 4096, 65500}} {
							src, dst, index := layout[0], layout[1], layout[2]
							acc, remaining := uint32(0x87654321), rows
							var lastA, lastB, sign uint32
							trapped := false
							for remaining != 0 && !trapped {
								for i := 0; i < lanes; i++ {
									off := uint32(i * 4)
									if reverse {
										off = uint32((lanes - 1 - i) * 4)
									}
									lastA, lastB = src+index+off, dst+index+off
									if uint64(lastA)+4 > 65536 || uint64(lastB)+4 > 65536 {
										trapped = true
										break
									}
									diff := binary.LittleEndian.Uint32(initial[lastA:]) - binary.LittleEndian.Uint32(initial[lastB:])
									sign = uint32(int32(diff) >> 31)
									acc += (diff ^ sign) - sign
								}
								if !trapped {
									index += stride
									remaining--
								}
							}
							want := uint32(123) ^ remaining ^ index ^ acc ^ lastA ^ lastB ^ sign
							m := absReduceModule(t, lanes, reverse, localStride, src, dst, rows, layout[2], stride)
							for _, enabled := range []bool{false, true} {
								got, mem, err, _ := runFloatMapMemory(t, m, enabled, initial)
								if (err != nil) != trapped || !trapped && got != want || !bytes.Equal(mem, initial) {
									t.Fatalf("lanes%d reverse%t local%t rows%d stride%x src%x dst%x index%x enabled%t: got%x/%v want%x trapped%t", lanes, reverse, localStride, rows, stride, src, dst, layout[2], enabled, got, err, want, trapped)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestAbsReduceAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(absReduceModule(t, 8, true, true, 0, 4096, 8, 0, 192), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"dot-loop-vector": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		hits := stats.Funcs[0].Peephole["abs-reduce-loop-vector"]
		if enabled && hits != 1 || !enabled && hits != 0 {
			t.Fatalf("enabled%t admissions%d", enabled, hits)
		}
	}
}
