//go:build wago_regalloccheck && !tinygo

package riscv64

import (
	"bytes"
	"fmt"
	"math/bits"
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
)

func TestRegallocImmediateCopyPrecedesPhysicalWrite(t *testing.T) {
	for _, tc := range []struct {
		name        string
		emit        func(*Asm)
		source      regalloccheck.Location
		size, clear int
	}{
		{"self", func(a *Asm) { a.Addi(X9, X9, 0) }, gp(X9), 8, 0},
		{"other", func(a *Asm) { a.Addi(X9, X10, 0) }, gp(X10), 8, 0},
		{"ld", func(a *Asm) { a.Ld(X9, SP, 24) }, regalloccheck.Slot(24), 8, 0},
		{"lwu", func(a *Asm) { a.Lwu(X9, SP, 24) }, regalloccheck.Slot(24), 4, 8},
		{"lw", func(a *Asm) { a.Lw(X9, SP, 24) }, regalloccheck.Slot(24), 4, 8},
		{"lbu", func(a *Asm) { a.Lbu(X9, SP, 24) }, regalloccheck.Slot(24), 1, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a, plain Asm
			var events []string
			a.ObserveRegalloc(func(e regalloccheck.Effect) {
				if len(a.B) != 0 {
					t.Fatal("effect arrived after emission")
				}
				if e != (regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: gp(X9), Src: tc.source, Size: tc.size, ClearTo: tc.clear}) {
					t.Fatal("wrong physical copy", e)
				}
				events = append(events, "copy")
			})
			a.ObserveGPWrites(func(mask uint32) {
				if len(a.B) != 0 || mask != 1<<X9 {
					t.Fatal("wrong write notification", mask)
				}
				events = append(events, "write")
			})
			tc.emit(&a)
			tc.emit(&plain)
			if a.Err() != nil || !reflect.DeepEqual(events, []string{"copy", "write"}) || !bytes.Equal(a.B, plain.B) {
				t.Fatal("copy ordering or bytes", events, a.Err())
			}
		})
	}
}

func TestRegallocShift32ActualByteProvenance(t *testing.T) {
	for _, op := range []string{"left", "right", "normalize"} {
		for _, src := range []Reg{Zero, SP, X9, X31} {
			for _, dst := range []Reg{Zero, SP, X9, X31} {
				t.Run(fmt.Sprintf("%s/%d/%d", op, src, dst), func(t *testing.T) {
					var a, plain Asm
					var state regalloccheck.State
					originalDst := state.Seed(gp(dst), 8)
					input := state.Seed(gp(src), 8)
					var masks []uint32
					a.ObserveRegalloc(state.Apply)
					a.ObserveGPWrites(func(mask uint32) { masks = append(masks, mask) })
					emit := func(a *Asm) {
						switch op {
						case "left":
							a.Slli(dst, src, 32)
						case "right":
							a.Srli(dst, src, 32)
						default:
							a.ZeroExtend32(dst, src)
						}
					}
					emit(&a)
					emit(&plain)
					want := make(regalloccheck.Value, 8)
					if dst == Zero {
						copy(want, originalDst)
					} else if src != Zero {
						switch op {
						case "left":
							copy(want[4:], input[:4])
						case "right":
							copy(want[:4], input[4:])
						default:
							copy(want[:4], input[:4])
						}
					}
					if got := state.Read(gp(dst), 8); !reflect.DeepEqual(got, want) {
						t.Fatal("incorrect byte identities or displaced-half kill", got, want)
					}
					if src != dst {
						state.Expect("source untouched", gp(src), input)
					}
					if a.Err() != nil || !bytes.Equal(a.B, plain.B) || !reflect.DeepEqual(masks, decodedGPWrites(t, a.B)) {
						t.Fatal("physical writes/bytes changed", masks, a.Err())
					}
				})
			}
		}
	}
}

func TestRegallocNormalizationReportsBothInstructions(t *testing.T) {
	var a Asm
	var events []string
	a.ObserveRegalloc(func(e regalloccheck.Effect) {
		instruction := len(a.B) / 4
		index := len(events) % 6
		if instruction > 1 || index > 4 {
			t.Fatal("transfer notification ordering", events)
		}
		if index < 4 {
			from, to := uint8(index), uint8(index+4)
			if instruction == 1 {
				from, to = to, from
			}
			d, s := gp(X9), gp(X9)
			d.Byte, s.Byte = to, from
			if e != (regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: d, Src: s, Size: 1}) {
				t.Fatal("not an actual shift-byte copy", e)
			}
		} else {
			d := gp(X9)
			if instruction == 1 {
				d.Byte = 4
			}
			if e != (regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: d, Size: 4}) {
				t.Fatal("wrong displaced half", e)
			}
		}
		events = append(events, "effect")
	})
	a.ObserveGPWrites(func(mask uint32) {
		if len(events)%6 != 5 || mask != 1<<X9 {
			t.Fatal("missing/early intermediate write", events, mask)
		}
		events = append(events, "write")
	})
	a.ZeroExtend32(X9, X9)
	if a.Err() != nil || len(events) != 12 || len(a.B) != 8 {
		t.Fatal("incomplete physical normalization", events, a.Err())
	}
}

func TestRegallocShiftOtherFormsRemainDefinitions(t *testing.T) {
	for _, emit := range []func(*Asm){
		func(a *Asm) { a.Slli(X9, X9, 31) }, func(a *Asm) { a.Srli(X9, X9, 33) },
		func(a *Asm) { a.Srai(X9, X9, 32) }, func(a *Asm) { a.Slliw(X9, X9, 0) },
		func(a *Asm) { a.Srliw(X9, X9, 31) }, func(a *Asm) { a.Addiw(X9, X9, 0) },
	} {
		var a Asm
		var events []string
		a.ObserveGPWrites(func(mask uint32) {
			if mask != 1<<X9 {
				t.Fatal(mask)
			}
			events = append(events, "write")
		})
		a.ObserveRegalloc(func(e regalloccheck.Effect) {
			if e != (regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: gp(X9), Size: 8}) {
				t.Fatal("unqualified shift became transfer", e)
			}
			events = append(events, "kill")
		})
		emit(&a)
		if a.Err() != nil || !reflect.DeepEqual(events, []string{"write", "kill"}) {
			t.Fatal("definition ordering changed", events, a.Err())
		}
	}
}

// Reconcile independent physical writes with actual transfer effects, as the
// shared graph does. No semantic Define or post-emission reseeding is allowed.
func TestRegallocNormalizationEffectsProveTransportAndRejectClobber(t *testing.T) {
	for _, clobber := range []bool{false, true} {
		var a Asm
		upper := gp(X9)
		upper.Byte = 4
		g := regalloccheck.Graph{Widths: []uint8{4, 4}, Blocks: []regalloccheck.Block{{}}, Inputs: []regalloccheck.Binding{{Location: gp(X9), Value: 1}, {Location: upper, Value: 2}}}
		copies := uint32(0)
		appendEffect := func(e regalloccheck.Effect) {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Machine, Effect: e})
		}
		a.ObserveRegalloc(func(e regalloccheck.Effect) {
			if e.Kind == regalloccheck.Copy && e.Dst.Bank == regalloccheck.GP {
				copies |= 1 << uint(e.Dst.Index)
			}
			appendEffect(e)
		})
		a.ObserveGPWrites(func(mask uint32) {
			unmatched := mask &^ copies
			copies &^= mask
			for unmatched != 0 {
				r := bits.TrailingZeros32(unmatched)
				unmatched &^= 1 << r
				appendEffect(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: gp(Reg(r)), Size: 8})
			}
		})
		a.Addi(X9, X9, 0)
		a.ZeroExtend32(X9, X9)
		if clobber {
			a.Addi(X9, Zero, 7)
		}
		g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: gp(X9), Value: 1})
		want := regalloccheck.Verified
		if clobber {
			want = regalloccheck.Rejected
		}
		if result := g.Verify(regalloccheck.Limits{}); result.Verdict != want || copies != 0 {
			t.Fatal("actual transport/clobber verdict", clobber, result, copies)
		}
		if !clobber {
			g.Blocks[0].Operations = append(g.Blocks[0].Operations, regalloccheck.Operation{Kind: regalloccheck.Use, Location: upper, Value: 2})
			if result := g.Verify(regalloccheck.Limits{}); result.Verdict != regalloccheck.Rejected {
				t.Fatal("old upper identity survived normalization", result)
			}
		}
	}
}

func TestRegallocShift32ValidationAndIndependentObservers(t *testing.T) {
	for _, bad := range []func(*Asm){
		func(a *Asm) { a.ZeroExtend32(Reg(32), X9) }, func(a *Asm) { a.ZeroExtend32(X9, Reg(32)) },
		func(a *Asm) { a.Slli(X9, Reg(32), 32) }, func(a *Asm) { a.Srli(Reg(32), X9, 32) },
		func(a *Asm) { a.Slli(X9, X9, 64) },
	} {
		var a Asm
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("invalid/sticky transfer") })
		a.ObserveGPWrites(func(uint32) { t.Fatal("invalid/sticky write") })
		bad(&a)
		a.ZeroExtend32(X9, X9)
		if a.Err() == nil || len(a.B) != 0 {
			t.Fatal("invalid shift emitted bytes")
		}
	}
	for _, emit := range []func(*Asm){func(a *Asm) { a.Slli(X9, X9, 32) }, func(a *Asm) { a.Srli(X9, X9, 32) }, func(a *Asm) { a.ZeroExtend32(X9, X9) }} {
		a := Asm{B: []byte{1}}
		a.ObserveRegalloc(func(regalloccheck.Effect) { t.Fatal("unaligned transfer") })
		a.ObserveGPWrites(func(uint32) { t.Fatal("unaligned write") })
		emit(&a)
		if a.Err() == nil || len(a.B) != 1 {
			t.Fatal("unaligned shift emitted bytes")
		}
	}
	var a Asm
	writes, outer, inner := 0, 0, 0
	a.ObserveGPWrites(func(uint32) { writes++ })
	previous := a.ObserveRegalloc(func(regalloccheck.Effect) { outer++ })
	a.ZeroExtend32(X9, X9)
	saved := a.ObserveRegalloc(func(regalloccheck.Effect) { inner++ })
	a.ZeroExtend32(X9, X9)
	a.ObserveRegalloc(saved)
	a.ZeroExtend32(X9, X9)
	a.ObserveRegalloc(previous)
	a.ZeroExtend32(X9, X9) // no transfer observer; both GP writes still occur
	if writes != 8 || outer != 20 || inner != 10 {
		t.Fatal("transfer scope hid writes or leaked observers", writes, outer, inner)
	}
}

func TestRegallocShiftObserverScopesRestoreAfterPanic(t *testing.T) {
	var a Asm
	outerEffects, outerWrites, innerEffects := 0, 0, 0
	a.ObserveRegalloc(func(regalloccheck.Effect) { outerEffects++ })
	a.ObserveGPWrites(func(uint32) { outerWrites++ })
	func() {
		defer func() {
			if p := recover(); p != "shift observer panic" {
				t.Fatalf("wrong panic: %v", p)
			}
		}()
		var previousEffect func(regalloccheck.Effect)
		previousEffect = a.ObserveRegalloc(func(e regalloccheck.Effect) {
			previousEffect(e)
			innerEffects++
			if a.Len() == 4 {
				panic("shift observer panic")
			}
		})
		defer func() { a.ObserveRegalloc(previousEffect) }()
		var previousWrite func(uint32)
		previousWrite = a.ObserveGPWrites(func(mask uint32) { previousWrite(mask) })
		defer func() { a.ObserveGPWrites(previousWrite) }()
		a.ZeroExtend32(X9, X9)
	}()
	if a.Len() != 4 || outerWrites != 1 || innerEffects != 6 {
		t.Fatal("panic did not preserve instruction boundary", a.Len(), outerWrites, innerEffects)
	}
	beforeEffects, beforeWrites := outerEffects, outerWrites
	a.Addi(X9, X10, 0)
	if outerEffects != beforeEffects+1 || outerWrites != beforeWrites+1 || innerEffects != 6 {
		t.Fatal("enclosing observers were not restored")
	}
}
