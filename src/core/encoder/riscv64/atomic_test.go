package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Independent LLVM19 RV64G words; fixture includes every aq/rl encoding. LR.rl
// and SC.aq are encodable but are not stronger alternatives to LR.aq/SC.rl.
var atomicCases = []struct {
	operation string
	order     AtomicOrder
	word      uint32
}{
	{"lr.w", AtomicRelaxed, 0x100322af}, {"lr.w", AtomicAcquire, 0x140322af}, {"lr.w", AtomicRelease, 0x120322af}, {"lr.w", AtomicAcquireRelease, 0x160322af},
	{"sc.w", AtomicRelaxed, 0x187322af}, {"sc.w", AtomicAcquire, 0x1c7322af}, {"sc.w", AtomicRelease, 0x1a7322af}, {"sc.w", AtomicAcquireRelease, 0x1e7322af},
	{"lr.d", AtomicRelaxed, 0x100332af}, {"lr.d", AtomicAcquire, 0x140332af}, {"lr.d", AtomicRelease, 0x120332af}, {"lr.d", AtomicAcquireRelease, 0x160332af},
	{"sc.d", AtomicRelaxed, 0x187332af}, {"sc.d", AtomicAcquire, 0x1c7332af}, {"sc.d", AtomicRelease, 0x1a7332af}, {"sc.d", AtomicAcquireRelease, 0x1e7332af},
}

func emitAtomic(a *Asm, operation string, dst, value, base Reg, order AtomicOrder) {
	switch operation {
	case "lr.w":
		a.LrW(dst, base, order)
	case "sc.w":
		a.ScW(dst, value, base, order)
	case "lr.d":
		a.LrD(dst, base, order)
	case "sc.d":
		a.ScD(dst, value, base, order)
	}
}

func TestAtomicEncodingAndAssembler(t *testing.T) {
	var a Asm
	for _, tc := range atomicCases {
		start := a.Len()
		emitAtomic(&a, tc.operation, X5, X7, X6, tc.order)
		if a.Err() != nil || a.Len() != start+4 || binary.LittleEndian.Uint32(a.B[start:]) != tc.word {
			t.Fatal("atomic encoding", tc, a.Err(), a.B[start:])
		}
	}
	if !bytes.Equal(a.B, assembledBytes(t, "atomic")) {
		t.Fatal("atomic LLVM comparison")
	}
}

func TestAtomicValidationAndEdgeRegisters(t *testing.T) {
	for _, operation := range []string{"lr.w", "sc.w", "lr.d", "sc.d"} {
		for _, reg := range []Reg{Zero, X31} {
			var a Asm
			emitAtomic(&a, operation, reg, reg, reg, AtomicAcquireRelease)
			if a.Err() != nil || len(a.B) != 4 {
				t.Fatal("edge register", operation, reg, a.Err())
			}
			w := binary.LittleEndian.Uint32(a.B)
			if Reg((w>>7)&31) != reg || Reg((w>>15)&31) != reg || ((operation == "lr.w" || operation == "lr.d") && w>>20&31 != 0) {
				t.Fatal("edge fields", operation, w)
			}
		}
		for _, tc := range []struct {
			dst, value, base Reg
			order            AtomicOrder
			prefix           []byte
		}{
			{32, X7, X6, AtomicRelaxed, nil}, {X5, X7, 32, AtomicRelaxed, nil}, {X5, X7, X6, 4, nil}, {X5, X7, X6, 255, nil}, {X5, X7, X6, AtomicRelaxed, []byte{0}},
		} {
			a := Asm{B: bytes.Clone(tc.prefix)}
			emitAtomic(&a, operation, tc.dst, tc.value, tc.base, tc.order)
			if a.Err() == nil || !bytes.Equal(a.B, tc.prefix) {
				t.Fatal("invalid atomic emitted", operation, tc, a.B, a.Err())
			}
			a.LrW(X5, X6, AtomicAcquire)
			if !bytes.Equal(a.B, tc.prefix) {
				t.Fatal("sticky error emitted")
			}
		}
		if operation == "sc.w" || operation == "sc.d" {
			var a Asm
			emitAtomic(&a, operation, X5, 32, X6, AtomicRelaxed)
			if a.Err() == nil || len(a.B) != 0 {
				t.Fatal("invalid store source emitted")
			}
		}
	}
}
