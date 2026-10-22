//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const trapAtomicUnaligned = 15

// Every operation is sequentially consistent with both Wasm and Go accesses.
// Full memory fences also order naturally atomic byte/halfword loads and stores;
// aq/rl order the constrained reservation loops themselves.
func (f *integerEmitter) atomicFence() {
	f.a.Fence(enc.FenceRead|enc.FenceWrite, enc.FenceRead|enc.FenceWrite)
}

// atomicAddress keeps the stable memory base in X28. Check the exact byte range
// before alignment, matching the other backends, and normalize only the index.
func (f *integerEmitter) atomicAddress(d shared.Atomic, slot int, spill func(int) int32) {
	f.Load(5, spill(slot), false)
	f.a.Constant64(enc.X28, int64(d.Offset))
	f.a.Add(enc.X28, enc.X28, enc.X5)
	f.a.Addi(enc.X7, enc.X28, int32(d.Size))
	f.productionMemorySizeAt(0, enc.X29)
	f.trap(TrapMemoryBounds, f.a.BranchLong(enc.LTU, enc.X29, enc.X7))
	if d.Size > 1 {
		f.a.Andi(enc.X7, enc.X28, int32(d.Size-1))
		f.trap(trapAtomicUnaligned, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	}
	f.productionMemoryBaseAt(0, enc.X29)
	f.a.Add(enc.X28, enc.X29, enc.X28)
}

// atomicMemory transports operands through canonical homes. X13 remains the
// external result arena; X9/X14/X15 are only temporary values within the body.
func (f *integerEmitter) atomicMemory(d shared.Atomic, base int, spill func(int) int32) {
	f.atomicAddress(d, base, spill)
	f.atomicFence()
	if d.Class != shared.AtomicLoad {
		value := base + 1
		if d.Class == shared.AtomicCmpxchg {
			value++
		}
		f.Load(6, spill(value), d.ResultSize == 8)
	}
	switch d.Class {
	case shared.AtomicLoad:
		switch d.Size {
		case 1:
			f.a.Lbu(enc.X5, enc.X28, 0)
		case 2:
			f.a.Lhu(enc.X5, enc.X28, 0)
		case 4:
			f.a.Lwu(enc.X5, enc.X28, 0)
		case 8:
			f.a.Ld(enc.X5, enc.X28, 0)
		}
	case shared.AtomicStore:
		switch d.Size {
		case 1:
			f.a.Sb(enc.X6, enc.X28, 0)
		case 2:
			f.a.Sh(enc.X6, enc.X28, 0)
		case 4:
			f.a.Sw(enc.X6, enc.X28, 0)
		case 8:
			f.a.Sd(enc.X6, enc.X28, 0)
		}
	default:
		if d.Class == shared.AtomicCmpxchg {
			f.Load(7, spill(base+1), d.ResultSize == 8)
		}
		if d.Size < 4 {
			f.atomicLaneRMW(d)
		} else {
			f.atomicWordRMW(d)
		}
	}
	f.atomicFence()
	if d.Class != shared.AtomicStore {
		f.Store(spill(base), 5, d.ResultSize == 8)
	}
}

func (f *integerEmitter) atomicUpdate(d shared.Atomic, dst, old, value enc.Reg) {
	switch d.Operation {
	case shared.AtomicAdd:
		f.a.Add(dst, old, value)
	case shared.AtomicSub:
		f.a.Sub(dst, old, value)
	case shared.AtomicAnd:
		f.a.And(dst, old, value)
	case shared.AtomicOr:
		f.a.Or(dst, old, value)
	case shared.AtomicXor:
		f.a.Xor(dst, old, value)
	default:
		f.a.Addi(dst, value, 0) // exchange and compare-exchange replacement
	}
}

func (f *integerEmitter) atomicWordRMW(d shared.Atomic) {
	if d.Size == 4 && d.Class == shared.AtomicCmpxchg {
		f.a.ZeroExtend32(enc.X7, enc.X7)
	}
	loop := f.Position()
	if d.Size == 8 {
		f.a.LrD(enc.X5, enc.X28, enc.AtomicAcquire)
	} else {
		f.a.LrW(enc.X5, enc.X28, enc.AtomicAcquire)
		f.a.ZeroExtend32(enc.X5, enc.X5)
	}
	if d.Class == shared.AtomicCmpxchg {
		f.a.Branch(enc.NE, enc.X5, enc.X7, 16)
	}
	f.atomicUpdate(d, enc.X29, enc.X5, enc.X6)
	if d.Size == 8 {
		f.a.ScD(enc.X30, enc.X29, enc.X28, enc.AtomicRelease)
	} else {
		f.a.ScW(enc.X30, enc.X29, enc.X28, enc.AtomicRelease)
	}
	f.a.Branch(enc.NE, enc.X30, enc.Zero, int32(loop-f.Position()))
}

// Baseline A has no subword AMO. A <=11-instruction LR/SC sequence replaces
// only the selected aligned lane in its enclosing word. The word lies within
// the page-aligned linear reservation even when the lane is the last byte.
func (f *integerEmitter) atomicLaneRMW(d shared.Atomic) {
	f.a.Andi(enc.X9, enc.X28, 3)
	f.a.Slli(enc.X9, enc.X9, 3)
	f.a.Andi(enc.X28, enc.X28, -4)
	mask := int64(255)
	if d.Size == 2 {
		mask = 65535
	}
	f.a.Constant64(enc.X14, mask)
	f.a.And(enc.X6, enc.X6, enc.X14)
	if d.Class == shared.AtomicCmpxchg {
		f.a.And(enc.X7, enc.X7, enc.X14)
	}
	f.a.Sll(enc.X15, enc.X14, enc.X9)
	f.a.Xori(enc.X15, enc.X15, -1)
	loop := f.Position()
	f.a.LrW(enc.X29, enc.X28, enc.AtomicAcquire)
	f.a.Srl(enc.X5, enc.X29, enc.X9)
	f.a.And(enc.X5, enc.X5, enc.X14)
	if d.Class == shared.AtomicCmpxchg {
		f.a.Branch(enc.NE, enc.X5, enc.X7, 32)
	}
	f.atomicUpdate(d, enc.X30, enc.X5, enc.X6)
	f.a.And(enc.X30, enc.X30, enc.X14)
	f.a.Sll(enc.X30, enc.X30, enc.X9)
	f.a.And(enc.X29, enc.X29, enc.X15)
	f.a.Or(enc.X29, enc.X29, enc.X30)
	f.a.ScW(enc.X30, enc.X29, enc.X28, enc.AtomicRelease)
	f.a.Branch(enc.NE, enc.X30, enc.Zero, int32(loop-f.Position()))
}

func (f *integerEmitter) atomicWaitCall(d shared.Atomic, base int, spill func(int) int32) {
	helper, slots := shared.AtomicHelperNotify, 3
	if d.Class == shared.AtomicWait {
		helper, slots = shared.AtomicHelperWait32, 4
		if d.Size == 8 {
			helper = shared.AtomicHelperWait64
		}
	}
	f.restoreCallTransport()
	for i := 0; i < slots-1; i++ {
		wide := i == 2 || i == 1 && d.Class == shared.AtomicWait && d.Size == 8
		f.Load(5, spill(base+i), wide)
		f.a.Sd(enc.X5, enc.X10, int32(i*8))
	}
	f.a.Constant64(enc.X5, int64(d.Offset))
	f.a.Sd(enc.X5, enc.X10, int32((slots-1)*8))
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), atomicHelper: shared.AtomicWaitDispatchBit | helper})
	f.restoreCallTransport()
	f.a.Lwu(enc.X5, enc.X13, 0)
	f.Store(spill(base), 5, false)
}
