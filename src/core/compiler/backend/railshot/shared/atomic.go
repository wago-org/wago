package shared

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type AtomicClass uint8

const (
	AtomicNotify AtomicClass = iota
	AtomicWait
	AtomicFence
	AtomicLoad
	AtomicStore
	AtomicRMW
	AtomicCmpxchg
)

type AtomicOperation uint8

const (
	AtomicNone AtomicOperation = iota
	AtomicAdd
	AtomicSub
	AtomicAnd
	AtomicOr
	AtomicXor
	AtomicXchg
)

// AtomicWaitDispatchBit selects the internal synchronous wait/notify bridge.
// It is disjoint from public host-funcref and GC helper dispatch bits.
const AtomicWaitDispatchBit uint32 = 1 << 29

const (
	AtomicHelperNotify uint32 = iota
	AtomicHelperWait32
	AtomicHelperWait64
)

// Atomic describes exactly the architecture-neutral facts needed by direct
// Railshot lowering. Align is the encoded log2 alignment; Size and ResultSize
// are bytes. The initial product accepts memory32 index zero only.
type Atomic struct {
	Sub         uint32
	Class       AtomicClass
	Operation   AtomicOperation
	Size        uint8
	ResultSize  uint8
	Align       uint32
	MemoryIndex uint32
	Offset      uint64
}

func DecodeAtomic(r *wasm.Reader) (Atomic, error) {
	sub, err := r.U32()
	if err != nil {
		return Atomic{}, err
	}
	d := Atomic{Sub: sub}
	if sub == 3 {
		order, err := r.Byte()
		if err != nil {
			return Atomic{}, err
		}
		if order != 0 {
			return Atomic{}, fmt.Errorf("atomic.fence reserved byte = %d, want 0", order)
		}
		d.Class = AtomicFence
		return d, nil
	}
	if (sub >= 4 && sub < 0x10) || sub > 0x4e {
		return Atomic{}, fmt.Errorf("unsupported atomic opcode %d", sub)
	}
	d.Align, err = r.U32()
	if err != nil {
		return Atomic{}, err
	}
	if d.Align >= 64 {
		return Atomic{}, fmt.Errorf("atomic explicit memory index is outside the initial threads boundary")
	}
	off, err := r.U32()
	if err != nil {
		return Atomic{}, err
	}
	d.Offset = uint64(off)

	switch sub {
	case 0:
		d.Class, d.Size, d.ResultSize = AtomicNotify, 4, 4
	case 1:
		d.Class, d.Size, d.ResultSize = AtomicWait, 4, 4
	case 2:
		d.Class, d.Size, d.ResultSize = AtomicWait, 8, 4
	case 0x10:
		d.Class, d.Size, d.ResultSize = AtomicLoad, 4, 4
	case 0x11:
		d.Class, d.Size, d.ResultSize = AtomicLoad, 8, 8
	case 0x12, 0x14:
		d.Class, d.Size = AtomicLoad, 1
		if sub == 0x14 {
			d.ResultSize = 8
		} else {
			d.ResultSize = 4
		}
	case 0x13, 0x15:
		d.Class, d.Size = AtomicLoad, 2
		if sub == 0x15 {
			d.ResultSize = 8
		} else {
			d.ResultSize = 4
		}
	case 0x16:
		d.Class, d.Size, d.ResultSize = AtomicLoad, 4, 8
	case 0x17:
		d.Class, d.Size, d.ResultSize = AtomicStore, 4, 4
	case 0x18:
		d.Class, d.Size, d.ResultSize = AtomicStore, 8, 8
	case 0x19, 0x1b:
		d.Class, d.Size = AtomicStore, 1
		if sub == 0x1b {
			d.ResultSize = 8
		} else {
			d.ResultSize = 4
		}
	case 0x1a, 0x1c:
		d.Class, d.Size = AtomicStore, 2
		if sub == 0x1c {
			d.ResultSize = 8
		} else {
			d.ResultSize = 4
		}
	case 0x1d:
		d.Class, d.Size, d.ResultSize = AtomicStore, 4, 8
	default:
		group := (sub - 0x1e) / 7
		lane := (sub - 0x1e) % 7
		if sub >= 0x48 {
			d.Class = AtomicCmpxchg
		} else {
			d.Class = AtomicRMW
			d.Operation = []AtomicOperation{AtomicAdd, AtomicSub, AtomicAnd, AtomicOr, AtomicXor, AtomicXchg}[group]
		}
		switch lane {
		case 0:
			d.Size, d.ResultSize = 4, 4
		case 1:
			d.Size, d.ResultSize = 8, 8
		case 2:
			d.Size, d.ResultSize = 1, 4
		case 3:
			d.Size, d.ResultSize = 2, 4
		case 4:
			d.Size, d.ResultSize = 1, 8
		case 5:
			d.Size, d.ResultSize = 2, 8
		case 6:
			d.Size, d.ResultSize = 4, 8
		}
	}
	return d, nil
}
