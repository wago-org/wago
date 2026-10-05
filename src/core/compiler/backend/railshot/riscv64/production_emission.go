//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Admission follows the production lowerings implemented below. Ordinary
// memory and scalar global imports are supported; host imports, starts and tables remain gated.
func admitProductionWire(data []byte) error {
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return err
		}
		n, err := r.U32()
		if err != nil {
			return err
		}
		switch id {
		case 0, 1, 2, 3, 5, 6, 7, 10, 11, 12:
		default:
			return unsupported("production state/feature lowering is pending")
		}
		if uint64(n) > uint64(r.BytesLeft()) {
			return unsupported("truncated production section")
		}
		if id == 2 {
			section := wasm.ReaderFrom(data[8+r.Offset() : 8+r.Offset()+int(n)])
			count, err := section.U32()
			if err != nil || count == 0 || count > 257 {
				return unsupported("production requires 1..257 resource imports when an import section is present")
			}
			// Decoded admission checks every import's kind/type and aggregate bounds.
		}
		if err := r.JumpTo(r.Offset() + int(n)); err != nil {
			return err
		}
	}
	return nil
}

func productionTrapCode(code int) int {
	switch code {
	case TrapUnreachable:
		return abi.TrapUnreachable
	case TrapDivideByZero:
		return abi.TrapDivZero
	case TrapIntegerOverflow:
		return abi.TrapDivOverflow
	case TrapMemoryBounds:
		return abi.TrapLinMemOutOfBounds
	case trapFloatConversion:
		return abi.TrapTruncOverflow
	case trapCooperativeInterrupt:
		return abi.TrapInterrupted
	default:
		panic("rv64: unknown admitted integer trap")
	}
}

func (f *target) emitProductionTrap(code int, sourcePC uint32) {
	f.a.Ld(enc.X12, enc.X25, -abi.TrapCellPtrOffset)
	f.a.Constant32(enc.X5, uint32(code))
	f.a.Sw(enc.X5, enc.X12, 0)
	source := uint64(sourcePC)<<32 | uint64(f.mode.functionIndex+1)
	f.a.Constant64(enc.X5, int64(source))
	f.a.Sd(enc.X5, enc.X12, 16)
	f.a.Ld(enc.SP, enc.X25, -abi.RV64TrapReentrySPOffset)
	f.a.Ld(enc.Scratch0, enc.X25, -abi.RV64TrapContinuationOffset)
	f.a.Jalr(enc.Zero, enc.Scratch0, 0)
}
