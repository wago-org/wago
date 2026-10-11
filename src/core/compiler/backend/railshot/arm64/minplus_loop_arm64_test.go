//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func minPlusModule(t testing.TB, dst, src, pivot, start, limit uint32) *wasm.Module {
	body := []byte{1, 7, 0x7f}
	constant := func(x uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(x))...) }
	for local, x := range []uint32{dst, src, pivot, start} {
		constant(x)
		body = append(body, 0x21, byte(local))
	}
	// Keep a carried operand below the loop, as real calling functions do.
	constant(123)
	body = append(body, 0x03, 0x40)
	for i := uint32(0); i < 2; i++ {
		body = append(body, 0x20, 0, 0x20, 3, 0x6a)
		if i != 0 {
			constant(4)
			body = append(body, 0x6a)
		}
		body = append(body, 0x22, 4, 0x20, 4, 0x28, 2, 0, 0x22, 5, 0x20, 1, 0x20, 3, 0x6a)
		if i != 0 {
			constant(4)
			body = append(body, 0x6a)
		}
		body = append(body, 0x28, 2, 0, 0x20, 2, 0x28, 2, 0, 0x6a, 0x22, 6, 0x20, 6, 0x20, 5, 0x4a, 0x1b, 0x36, 2, 0)
	}
	body = append(body, 0x20, 3)
	constant(8)
	body = append(body, 0x6a, 0x22, 3)
	constant(limit)
	body = append(body, 0x47, 0x0d, 0, 0x0b)
	// Observe final source temporaries and every updated word in the result.
	body = append(body, 0x20, 3, 0x20, 4, 0x73, 0x20, 5, 0x73, 0x20, 6, 0x73, 0x73)
	if uint64(dst)+uint64(limit) <= 65536 {
		for i := start; i < limit; i += 4 {
			constant(dst + i)
			body = append(body, 0x28, 2, 0, 0x6a)
			constant(0x9e3779b9)
			body = append(body, 0x6c)
		}
	}
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestMinPlusLoopIndependentExecution(t *testing.T) {
	for _, dst := range []uint32{0, 1, 64, 65500, 0xfffffff0} {
		for _, src := range []uint32{0, 1, 4, 64, 256, 8192} {
			for _, pivot := range []uint32{0, 1, 4, 7, 12, 16, 28, 31, 32, 35, 40, 256, 65534} {
				for _, trip := range [][2]uint32{{0, 24}, {8, 24}, {0, 32}, {8, 32}, {0, 40}, {8, 40}, {0, 48}, {8, 48}, {0, 64}, {8, 64}, {0, 72}, {8, 72}, {0, 128}, {8, 128}, {0, 136}, {8, 136}, {0, 4144}, {8, 4144}} {
					start, limit := trip[0], trip[1]
					t.Run(fmt.Sprintf("%x/%x/%x/%d/%d", dst, src, pivot, start, limit), func(t *testing.T) {
						mem := make([]byte, 65536)
						for i := 0; i < len(mem)/4; i++ {
							binary.LittleEndian.PutUint32(mem[i*4:], uint32(i)*0x9e3779b9+0x80000001)
						}
						original := append([]byte(nil), mem...)
						var lastOld, lastSum, lastAddr uint32
						trap := false
						for c := start; c < limit && !trap; c += 8 {
							for i := uint32(0); i < 8; i += 4 {
								d, s := dst+c+i, src+c+i
								if uint64(d)+4 > 65536 || uint64(s)+4 > 65536 || uint64(pivot)+4 > 65536 {
									trap = true
									break
								}
								lastAddr = d
								lastOld = binary.LittleEndian.Uint32(mem[d:])
								lastSum = binary.LittleEndian.Uint32(mem[s:]) + binary.LittleEndian.Uint32(mem[pivot:])
								v := lastSum
								if int32(lastOld) < int32(v) {
									v = lastOld
								}
								binary.LittleEndian.PutUint32(mem[d:], v)
							}
						}
						want := limit ^ lastAddr ^ lastOld ^ lastSum ^ 123
						if !trap {
							for i := start; i < limit; i += 4 {
								want = (want + binary.LittleEndian.Uint32(mem[dst+i:])) * 0x9e3779b9
							}
						}
						got, err := runArm64WrapperMem(t, minPlusModule(t, dst, src, pivot, start, limit), 0, func(m []byte) { copy(m, original) })
						if trap {
							if err == nil {
								t.Fatal("expected trap")
							}
						} else if err != nil || got != want {
							t.Fatalf("got %x, %v; want %x", got, err, want)
						}
					})
				}
			}
		}
	}
}

func TestMinPlusLoopAdmissionAndRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, limit := range []uint32{24, 32, 40, 64, 128, 136, 4144} {
		m := minPlusModule(t, 0, 8192, 4, 0, limit)
		for _, enabled := range []bool{true, false} {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"dot-loop-vector": enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			got := stats.Funcs[0].Peephole["minplus-loop-vector"]
			if (got == 1) != enabled {
				t.Fatalf("limit %d enabled %t: admitted %d", limit, enabled, got)
			}
			got = stats.Funcs[0].Peephole["minplus-loop-unroll"]
			if (got == 1) != (enabled && limit >= 4*32+8) {
				t.Fatalf("limit %d enabled %t: unrolled %d", limit, enabled, got)
			}
		}
	}
}

func TestMinPlusLoopRejectsEffects(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name   string
		prefix []byte
		shared bool
	}{
		{"changed-base", []byte{0x41, 0, 0x21, 0}, false},
		{"extra-load", []byte{0x41, 0, 0x28, 2, 0, 0x21, 4}, false},
		{"extra-store", []byte{0x41, 0, 0x41, 0, 0x36, 2, 0}, false},
		{"shared", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := minPlusModule(t, 0, 256, 4, 0, 64)
			if tc.shared {
				m.Memories[0].Shared = true
			} else {
				body := m.Code[0].BodyBytes
				for i := 0; i+1 < len(body); i++ {
					if body[i] == 0x03 && body[i+1] == 0x40 {
						body = append(append(append([]byte{}, body[:i+2]...), tc.prefix...), body[i+2:]...)
						break
					}
				}
				m.Code[0].BodyBytes = body
			}
			stats := compileWithStats(t, m, false).Funcs[0]
			if stats.Peephole["minplus-loop-vector"] != 0 {
				t.Fatal("unsafe loop admitted")
			}
		})
	}
}
