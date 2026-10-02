//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestSelectXMMReloadsPressureSpilledBranches(t *testing.T) {
	for _, typ := range []machineType{mtF32, mtF64, mtV128} {
		t.Run(fmt.Sprintf("type=%v", typ), func(t *testing.T) {
			f := &fn{a: &encoder.Asm{}, s: newStack(), sc: newScratch(), localSlot: []uint32{0, 1}}
			var prefix [14]*elem
			for r := range prefix {
				prefix[r] = f.pushFReg(Reg(r), typ)
			}
			// Both branches were displaced before select. Keep their slots live
			// while reloading them requires spilling two of the XMM prefix values.
			a := f.pushFReg(14, typ)
			f.spillF(a)
			aSlot := a.st.slot
			b := f.pushFReg(15, typ)
			f.spillF(b)
			bSlot := b.st.slot
			branchEnd := int(bSlot) + typ.stackSlots()
			if a.st.kind != stSlot || b.st.kind != stSlot || aSlot+uint32(typ.stackSlots()) > bSlot {
				t.Fatalf("branches were not displaced into distinct slots: a=%+v b=%+v", a.st, b.st)
			}
			// Model two pinned XMM locals: all remaining registers are occupied.
			f.fpinnedLocalMask = maskOf(14, 15)
			f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
			divisor := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
			f.pushBinOp(opDivU, mtI32)
			f.emitSelect()

			// Check displacement itself, rather than aggregate counters: XMM
			// spills/reloads aren't included in CodegenStats.Spills/Reloads.
			for i, e := range prefix[:2] {
				if e.st.kind != stSlot || e.st.slotIndex() < branchEnd {
					t.Fatalf("prefix %d did not spill beyond live branch slots: %+v, branch end=%d", i, e.st, branchEnd)
				}
			}
			if prefix[0].st.slotIndex()+typ.stackSlots() > prefix[1].st.slotIndex() {
				t.Fatal("pressure spills overlap")
			}
			if a.st.kind != stReg || b.st.kind != stReg {
				t.Fatalf("displaced branches were not reloaded: a=%+v b=%+v", a.st, b.st)
			}
			var divide encoder.Asm
			divide.Div(divisor.st.reg, false)
			if !bytes.Contains(f.a.B, divide.B) {
				t.Fatal("runtime condition did not emit a divide")
			}
			for _, branch := range []struct {
				e    *elem
				slot uint32
			}{{a, aSlot}, {b, bSlot}} {
				var reload encoder.Asm
				if typ == mtV128 {
					// Use the same feature selection as the lowering under test.
					loader := &fn{a: &reload, sc: f.sc}
					loader.mov128LoadDisp(branch.e.st.reg, RSP, f.spillOff(int(branch.slot)))
				} else {
					reload.FLoadDisp(branch.e.st.reg, RSP, f.spillOff(int(branch.slot)), true)
				}
				if !bytes.Contains(f.a.B, reload.B) {
					t.Fatalf("branch slot %d was not reloaded into XMM%d", branch.slot, branch.e.st.reg)
				}
			}
			if f.depth() != len(prefix)+1 || f.pinned != 0 || f.fpinned != 0 {
				t.Fatalf("select left depth=%d GP pins=%#x XMM pins=%#x", f.depth(), f.pinned, f.fpinned)
			}
		})
	}
}

func TestSelectXMMRegisterPressure(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.V128} {
		for _, typed := range []bool{false, true} {
			for _, flags := range []bool{false, true} {
				t.Run(fmt.Sprintf("type=%v/typed=%v/flags=%v", typ, typed, flags), func(t *testing.T) {
					m, prefix := selectXMMPressureModule(t, typ, typed, flags)
					// Signed zero and a NaN payload must survive float selection
					// bit-for-bit. Vector operands differ in both 64-bit halves.
					var a, b [16]byte
					switch typ {
					case wasm.F32:
						binary.LittleEndian.PutUint32(a[:], 0x80000000)
						binary.LittleEndian.PutUint32(b[:], 0x7fc12345)
					case wasm.F64:
						binary.LittleEndian.PutUint64(a[:], 0x8000000000000000)
						binary.LittleEndian.PutUint64(b[:], 0x7ff8123456789abc)
					case wasm.V128:
						copy(a[:], []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})
						copy(b[:], []byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0})
					}
					if diagnosticsEnabled {
						stats := compileWithStats(t, m, false).Funcs[0]
						if stats.Spills == 0 || stats.MaxSpillSlots == 0 {
							t.Fatalf("fixture did not create register pressure: spills=%d slots=%d", stats.Spills, stats.MaxSpillSlots)
						}
					}
					const divisor = uint64(2)
					for _, numerator := range []uint64{0, 1, 4} {
						t.Run(fmt.Sprintf("numerator=%d/divisor=%d", numerator, divisor), func(t *testing.T) {
							got, mem, err := runMemAmd64(t, m, func(mem []byte) {
								copy(mem[64:], a[:])
								copy(mem[80:], b[:])
							}, numerator, divisor)
							if err != nil {
								t.Fatal(err)
							}
							chooseA := numerator/divisor != 0
							if flags {
								chooseA = !chooseA
							}
							want := b[:len(prefix)]
							if chooseA {
								want = a[:len(prefix)]
							}
							if !bytes.Equal(mem[:len(want)], want) {
								t.Fatalf("selected bits = %x, want %x", mem[:len(want)], want)
							}
							if got != 210 || !bytes.Equal(mem[32:32+len(prefix)], prefix) {
								t.Fatalf("live prefixes corrupted: integer=%d XMM=%x, want 210 and %x", got, mem[32:32+len(prefix)], prefix)
							}
						})
					}
				})
			}
		}
	}
}

func selectXMMPressureModule(t testing.TB, typ wasm.ValType, typed, flags bool) (*wasm.Module, []byte) {
	t.Helper()
	const pressure = 20 // More live values than either register file can hold.
	// Two temporary XMM locals hold the selected value and the prefix checksum.
	body := []byte{0x01, 0x02, wasm.MustEncodeValType(typ)}
	for i := 1; i <= pressure; i++ {
		body = append(body, 0x41, byte(i), 0x41, 0, 0x41, 1, 0x1b)
	}
	var prefix []byte
	var store, align byte
	var combine []byte
	for i := 1; i <= pressure; i++ {
		switch typ {
		case wasm.F32:
			body = append(body, 0x43)
			body = binary.LittleEndian.AppendUint32(body, math.Float32bits(float32(i)))
			body = append(body, 0x43, 0, 0, 0, 0)
			prefix = binary.LittleEndian.AppendUint32(nil, math.Float32bits(210))
			store, align, combine = 0x38, 2, []byte{0x92} // f32.store / f32.add
		case wasm.F64:
			body = append(body, 0x44)
			body = binary.LittleEndian.AppendUint64(body, math.Float64bits(float64(i)))
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
			prefix = binary.LittleEndian.AppendUint64(nil, math.Float64bits(210))
			store, align, combine = 0x39, 3, []byte{0xa0} // f64.store / f64.add
		case wasm.V128:
			if prefix == nil {
				prefix = make([]byte, 16)
			}
			body = append(body, 0xfd, 0x0c) // v128.const
			for j := range 16 {
				v := byte(i * (j + 1))
				body = append(body, v)
				prefix[j] ^= v
			}
			body = append(body, 0xfd, 0x0c)
			body = append(body, make([]byte, 16)...)
			store, align, combine = 0x0b, 4, []byte{0xfd, 0x51} // v128.store / v128.xor
		}
		// Select eagerly owns each prefix value; bare constants would stay lazy
		// and would not exhaust XMM registers before the select being tested.
		body = append(body, 0x41, 1, 0x1b)
	}
	load := []byte{0x2a, 2, 0} // f32.load
	if typ == wasm.F64 {
		load = []byte{0x2b, 3, 0}
	} else if typ == wasm.V128 {
		load = []byte{0xfd, 0, 4, 0}
	}
	body = append(body, 0x41, 0xc0, 0) // i32.const 64
	body = append(body, load...)
	body = append(body, 0x41, 0xd0, 0) // i32.const 80
	body = append(body, load...)
	body = append(body, 0x20, 0, 0x20, 1, 0x6e) // Runtime numerator / divisor must reclaim RAX/RDX.
	if flags {
		body = append(body, 0x45) // eqz must fall back to scalar condition recovery for XMM.
	}
	if typed {
		body = append(body, 0x1c, 1, wasm.MustEncodeValType(typ))
	} else {
		body = append(body, 0x1b)
	}
	body = append(body, 0x21, 2) // Save selected bits without doing floating arithmetic.
	for range pressure - 1 {
		body = append(body, combine...)
	}
	body = append(body, 0x21, 3)
	for range pressure - 1 {
		body = append(body, 0x6a) // Sum all live integer prefix values, too.
	}
	for _, output := range []struct{ address, local byte }{{0, 2}, {32, 3}} {
		body = append(body, 0x41, output.address, 0x20, output.local)
		if typ == wasm.V128 {
			body = append(body, 0xfd)
		}
		body = append(body, store, align, 0)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body), prefix
}
