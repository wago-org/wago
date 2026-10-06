//go:build wago_regalloccheck

package amd64

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func TestRegallocIndexedVectorLoads(t *testing.T) {
	for _, regs := range []struct {
		dst, base, index Reg
		disp             int32
	}{{1, RAX, RCX, 32}, {14, R12, R13, -32}, {8, RSP, R9, 4096}} {
		for _, form := range []string{"SSE", "SSE-raw", "AVX", "AVX-raw", "f32", "f64"} {
			for _, load := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/xmm%d/load=%v", form, regs.dst, load), func(t *testing.T) {
					var a Asm
					var state regalloccheck.State
					dst := regalloccheck.Register(regalloccheck.FP, uint8(regs.dst))
					old := state.Seed(dst, 16)
					neighbor := regalloccheck.Register(regalloccheck.FP, uint8((regs.dst+1)%16))
					neighborValue := state.Seed(neighbor, 16)
					base := regalloccheck.Register(regalloccheck.GP, uint8(regs.base))
					baseValue := state.Seed(base, 8)
					index := regalloccheck.Register(regalloccheck.GP, uint8(regs.index))
					indexValue := state.Seed(index, 8)
					// Even an RSP base is not a known stack slot when it has an index.
					slot := regalloccheck.Slot(regs.disp)
					slotValue := state.Seed(slot, 16)
					a.ObserveRegalloc(state.Apply)
					op := byte(0x7f)
					if load {
						op = 0x6f
					}
					switch form {
					case "SSE":
						if load {
							a.MovdquLoadIdx(regs.dst, regs.base, regs.index, regs.disp)
						} else {
							a.MovdquStoreIdx(regs.base, regs.index, regs.dst, regs.disp)
						}
					case "SSE-raw":
						a.SseIdx(0xf3, op, regs.dst, regs.base, regs.index, regs.disp)
					case "AVX":
						if load {
							a.VMovdquLoadIdx(regs.dst, regs.base, regs.index, regs.disp)
						} else {
							a.VMovdquStoreIdx(regs.base, regs.index, regs.dst, regs.disp)
						}
					case "AVX-raw":
						a.VMovdquIdx(op, regs.dst, regs.base, regs.index, regs.disp)
					default:
						if load {
							a.FLoadIdx(regs.dst, regs.base, regs.index, regs.disp, form == "f64")
						} else {
							a.FStoreIdx(regs.base, regs.index, regs.dst, regs.disp, form == "f64")
						}
					}
					if load {
						if !reflect.DeepEqual(state.Read(dst, 16), make(regalloccheck.Value, 16)) {
							t.Fatal("indexed load retained known destination bytes")
						}
						// The old vector must also fail the same downstream transport gate.
						requireVectorLost(t, func() { state.Expect("stale vector after indexed load", dst, old) })
					} else {
						state.Expect("store source", dst, old)
					}
					state.Expect("neighbor", neighbor, neighborValue)
					state.Expect("base", base, baseValue)
					state.Expect("index", index, indexValue)
					state.Expect("unrelated stack slot", slot, slotValue)
				})
			}
		}
	}
}

// This isolates the active checker cost. Reuse the register facts and byte
// buffer. Restore known input facts on each iteration, including store controls.
func BenchmarkIndexedVectorObserved(b *testing.B) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
	}{
		{"SSE-load", func(a *Asm) { a.MovdquLoadIdx(10, R8, R9, 127) }},
		{"AVX-load", func(a *Asm) { a.VMovdquLoadIdx(10, R8, R9, 127) }},
		{"AVX-raw-load", func(a *Asm) { a.VMovdquIdx(0x6f, 10, R8, R9, 127) }},
		{"SSE-store", func(a *Asm) { a.MovdquStoreIdx(R8, R9, 10, 127) }},
		{"AVX-store", func(a *Asm) { a.VMovdquStoreIdx(R8, R9, 10, 127) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			a := Asm{B: make([]byte, 0, 16)}
			var state regalloccheck.State
			loc := regalloccheck.Register(regalloccheck.FP, 10)
			value := state.Seed(loc, 16)
			a.ObserveRegalloc(state.Apply)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state.Put(loc, value)
				a.B = a.B[:0]
				tc.emit(&a)
			}
			b.StopTimer()
			if len(a.B) != 7 {
				b.Fatalf("instruction size=%d", len(a.B))
			}
		})
	}
}
