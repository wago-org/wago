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

// Decode the destination from the emitted bytes, independently of the checker.
// Reg is a byte; every value >= 8 requests the physical extension bit.
func TestRegallocIndexedLoadsEncodedDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm, Reg, bool)
	}{
		{"SSE", func(a *Asm, r Reg, load bool) {
			if load {
				a.MovdquLoadIdx(r, RAX, RCX, 32)
			} else {
				a.MovdquStoreIdx(RAX, RCX, r, 32)
			}
		}},
		{"SSE-raw", func(a *Asm, r Reg, load bool) {
			op := byte(0x7f)
			if load {
				op = 0x6f
			}
			a.SseIdx(0xf3, op, r, RAX, RCX, 32)
		}},
		{"AVX", func(a *Asm, r Reg, load bool) {
			if load {
				a.VMovdquLoadIdx(r, RAX, RCX, 32)
			} else {
				a.VMovdquStoreIdx(RAX, RCX, r, 32)
			}
		}},
		{"AVX-raw", func(a *Asm, r Reg, load bool) {
			op := byte(0x7f)
			if load {
				op = 0x6f
			}
			a.VMovdquIdx(op, r, RAX, RCX, 32)
		}},
		{"f32", func(a *Asm, r Reg, load bool) {
			if load {
				a.FLoadIdx(r, RAX, RCX, 32, false)
			} else {
				a.FStoreIdx(RAX, RCX, r, 32, false)
			}
		}},
		{"f64", func(a *Asm, r Reg, load bool) {
			if load {
				a.FLoadIdx(r, RAX, RCX, 32, true)
			} else {
				a.FStoreIdx(RAX, RCX, r, 32, true)
			}
		}},
	} {
		for _, load := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/load=%v", tc.name, load), func(t *testing.T) {
				for raw := 0; raw < 256; raw++ {
					var a Asm
					var state regalloccheck.State
					var values [16]regalloccheck.Value
					for i := range values {
						values[i] = state.Seed(regalloccheck.Register(regalloccheck.FP, uint8(i)), 16)
					}
					var effect regalloccheck.Effect
					a.ObserveRegalloc(func(e regalloccheck.Effect) { effect = e; state.Apply(e) })
					tc.emit(&a, Reg(raw), load)
					// Each address ends with ModRM, SIB, and an 8-bit displacement.
					encoded := a.B[len(a.B)-3] >> 3 & 7
					if a.B[0] == 0xc4 {
						if a.B[1]&0x80 == 0 { // inverted VEX.R
							encoded |= 8
						}
					} else if a.B[1]&0xf0 == 0x40 && a.B[1]&4 != 0 {
						encoded |= 8 // REX.R
					}
					if load && (effect.Kind != regalloccheck.Kill || effect.Dst.Bank != regalloccheck.FP || effect.Dst.Index != int32(encoded) || effect.Size != 16) {
						t.Fatalf("raw=%d bytes=%x effect=%+v encoded=%d", raw, a.B, effect, encoded)
					}
					for i, value := range values {
						loc := regalloccheck.Register(regalloccheck.FP, uint8(i))
						if load && uint8(i) == encoded {
							if !reflect.DeepEqual(state.Read(loc, 16), make(regalloccheck.Value, 16)) {
								t.Fatalf("raw=%d bytes=%x retained XMM%d facts", raw, a.B, i)
							}
							requireVectorLost(t, func() { state.Expect("encoded load destination", loc, value) })
						} else {
							state.Expect("preserved vector", loc, value)
						}
					}
				}
			})
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
