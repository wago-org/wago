package amd64

import (
	"bytes"
	"reflect"
	"testing"
)

func referenceMemoryMemOp(a *Asm, opcode byte, regField byte, base Reg, disp int32, w bool) {
	if regallocCheckEnabled {
		if opcode == 0x8b {
			a.regallocLoad(Reg(regField), base, disp, false, regallocWidth(w))
		} else if opcode == 0x89 {
			a.regallocStore(base, disp, Reg(regField), false, regallocWidth(w))
		}
		a.regallocGPMem(opcode, Reg(regField), w || regField >= 8 || base >= 8)
	}
	rb, rr := base >= 8, regField >= 8
	if w || rr || rb {
		a.emit(a.rex(w, rr, false, rb), opcode)
	} else {
		a.emit(opcode)
	}
	a.baseAddr(regField, base, disp)
}

func TestMemoryBatchParity(t *testing.T) {
	regs := []byte{0, 1, 4, 7, 8, 15, 16, 255}
	ops := make([]byte, 256)
	for i := range ops {
		ops[i] = byte(i)
	}
	if regallocCheckEnabled {
		regs = regs[:6]
		ops = []byte{0x8b, 0x89, 0x8d}
	}
	disps := []int32{-1 << 31, -129, -128, -1, 0, 1, 127, 128, 1<<31 - 1}
	for _, stats := range []bool{false, true} {
		for mode := 0; mode < 6; mode++ {
			for _, w := range []bool{false, true} {
				for _, op := range ops {
					for _, reg := range regs {
						for _, disp := range disps {
							checkMemoryBatch(t, op, reg, RSP, disp, w, stats, mode)
						}
					}
				}
			}
		}
	}
	// Cover every base encoding in both the ordinary batch and symbolic path,
	// including R12 SIB and the RBP/R13 explicit-zero displacement rule.
	for baseValue := 0; baseValue < 256; baseValue++ {
		base := Reg(baseValue)
		if regallocCheckEnabled && baseValue >= 16 {
			continue
		}
		for _, op := range []byte{0x8b, 0x89, 0x8d} {
			for _, reg := range regs {
				for _, disp := range disps {
					for _, w := range []bool{false, true} {
						for _, mode := range []int{0, 1, 2} {
							checkMemoryBatch(t, op, reg, base, disp, w, true, mode)
						}
					}
				}
			}
		}
	}
}

func checkMemoryBatch(t *testing.T, op, reg byte, base Reg, disp int32, w, stats bool, mode int) {
	t.Helper()
	makeAsm := func() *Asm {
		a := &Asm{B: append(make([]byte, 0, 16), 0xaa, 0xbb, 0xcc)}
		if stats {
			a.EncodingStats = &EncodingStats{MemoryDisp0: 7, FrameDisp8: 9, RexPrefixes: 2}
		}
		if mode != 0 {
			a.LocalRefs = &LocalRefRecorder{Sites: make([]LocalRefSite, 1, 2), Limit: 2, Locals: 2, Next: 1, Pending: mode >= 2, Overflow: mode == 5}
			a.LocalRefs.Sites[0] = LocalRefSite{ModRMOff: 1, DispOff: 2, Local: 0, OldDisp: 64}
			if mode == 3 {
				a.LocalRefs.Next = 2
			}
			if mode == 4 {
				a.LocalRefs.Limit = 1
			}
		}
		return a
	}
	got, want := makeAsm(), makeAsm()
	got.memOp(op, reg, base, disp, w)
	referenceMemoryMemOp(want, op, reg, base, disp, w)
	if !bytes.Equal(got.B, want.B) || !reflect.DeepEqual(got.EncodingStats, want.EncodingStats) || !reflect.DeepEqual(got.LocalRefs, want.LocalRefs) {
		t.Fatalf("op=%x reg=%d base=%d disp=%d w=%v stats=%v mode=%d: got=%x stats=%+v refs=%+v; want=%x stats=%+v refs=%+v", op, reg, base, disp, w, stats, mode, got.B, got.EncodingStats, got.LocalRefs, want.B, want.EncodingStats, want.LocalRefs)
	}
}
