//go:build amd64 && wago_profile

package amd64

import (
	"bytes"
	"reflect"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestProfileFPressurePreservesAllocationAndCode(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtF64, mtV128} {
		for _, caches := range []uint8{0, 1, 2, 3} {
			for _, borrow := range []bool{false, true} {
				emit := func(enabled bool) ([]byte, map[string]int) {
					f := &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{RecordSources: enabled}}
					var avoid regMask
					for r := Reg(0); r < 16; r++ {
						if r == 14 && caches&1 != 0 {
							f.fconsts = append(f.fconsts, floatConstReg{typ: mtF64, bits: 1, reg: r})
							if borrow {
								avoid = avoid.add(r)
							}
							continue
						}
						if r == 15 && caches&2 != 0 {
							f.vconsts = append(f.vconsts, v128ConstReg{lo: 1, reg: r})
							if borrow {
								avoid = avoid.add(r)
							}
							continue
						}
						f.pushFReg(r, typ)
					}
					oldest := f.s.head.next
					if r := f.allocFReg(avoid); r != 0 || oldest.st.kind != stSlot {
						t.Fatalf("allocation changed: r=%v st=%+v", r, oldest.st)
					}
					return f.a.B, f.stats.Peephole
				}
				plain, absent := emit(false)
				profiled, decisions := emit(true)
				if !bytes.Equal(plain, profiled) || len(absent) != 0 {
					t.Fatal("profiling changed code or disabled collection", typ, caches, borrow)
				}
				want := map[string]int{"fp-pressure-spill": 1}
				if !borrow && caches != 0 {
					want["fp-pressure-constant-available"] = 1
					if caches&1 != 0 {
						want["fp-pressure-scalar-constant-available"] = 1
					}
					if caches&2 != 0 {
						want["fp-pressure-vector-constant-available"] = 1
					}
				}
				if !reflect.DeepEqual(decisions, want) {
					t.Fatal(typ, caches, borrow, decisions, want)
				}
			}
		}
	}
}

func TestProfileFPressureExcludesOwnedAndPinnedCaches(t *testing.T) {
	for _, blocked := range []string{"avoid", "pinned", "local", "owned"} {
		f := &fn{s: newStack(), stats: &CodegenStats{RecordSources: true}, fconsts: []floatConstReg{{reg: 14}}, vconsts: []v128ConstReg{{reg: 15}}}
		var avoid regMask
		switch blocked {
		case "avoid":
			avoid = maskOf(14, 15)
		case "pinned":
			f.fpinned = maskOf(14, 15)
		case "local":
			f.fpinnedLocalMask = maskOf(14, 15)
		case "owned":
			f.fregUser[14] = f.pushValue(storage{kind: stReg, typ: mtF64, reg: 14})
			f.fregUser[15] = f.pushValue(storage{kind: stReg, typ: mtV128, reg: 15})
		}
		f.recordProfileFPressure(avoid, "fp-pressure-exhaustion")
		if !reflect.DeepEqual(f.stats.Peephole, map[string]int{"fp-pressure-exhaustion": 1}) {
			t.Fatal(blocked, f.stats.Peephole)
		}
	}
}
