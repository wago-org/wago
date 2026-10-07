//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// This bounded public wire profile accompanies genuine RV64 lowering.
// Defined starts execute through public construction; private wire admission
// remains a separate profile without a start lifecycle.
func admitProductionWire(data []byte) error {
	return admitProductionWireProfile(data, false)
}

func admitProductionWireProfile(data []byte, public bool) error {
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
		case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12:
		default:
			return unsupported("production state/feature lowering is pending")
		}
		if uint64(n) > uint64(r.BytesLeft()) {
			return unsupported("truncated production section")
		}
		if id == 2 {
			section := wasm.ReaderFrom(data[8+r.Offset() : 8+r.Offset()+int(n)])
			count, err := section.U32()
			if err != nil || count == 0 && !public || count > uint32(MaxProductionImports) {
				return unsupported("production import section count bound")
			}
			// Decoded admission checks every import's kind/type and aggregate bounds.
		}
		if id == 5 {
			section := wasm.ReaderFrom(data[8+r.Offset() : 8+r.Offset()+int(n)])
			count, err := section.U32()
			limit := uint32(1)
			if public {
				limit = uint32(MaxProductionMemories)
			}
			if err != nil || count > limit {
				return unsupported("production memory section count bound")
			}
		}
		if id == 4 || id == 9 {
			section := wasm.ReaderFrom(data[8+r.Offset() : 8+r.Offset()+int(n)])
			count, err := section.U32()
			limit := uint32(1)
			if id == 9 {
				limit = MaxOwnedElementSegments
			}
			if err != nil || count == 0 && !public || count > limit {
				return unsupported("bounded private table/element section")
			}
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
	case trapIndirectBounds:
		return abi.TrapIndirectOutOfBounds
	case trapIndirectSignature:
		return abi.TrapIndirectWrongSig
	case trapTableBounds:
		return abi.TrapTableOutOfBounds
	case trapImportMissing:
		return abi.TrapCalledFnNotLinked
	case trapImportContext:
		return abi.TrapCalledFnNotLinked
	default:
		panic("rv64: unknown admitted integer trap")
	}
}

func (f *target) emitProductionTrap(code int, sourcePC uint32) {
	f.a.Ld(enc.X12, enc.X25, -abi.TrapCellPtrOffset)
	f.a.Constant32(enc.X5, uint32(code))
	f.a.Sw(enc.X5, enc.X12, 0)
	f.a.Ld(enc.X5, enc.X25, -abi.RV64NativeContextOffset)
	f.a.Sd(enc.X5, enc.X12, 8)
	source := uint64(sourcePC)<<32 | uint64(f.mode.functionIndex+1)
	f.a.Constant64(enc.X5, int64(source))
	f.a.Sd(enc.X5, enc.X12, 16)
	f.a.Ld(enc.SP, enc.X25, -abi.RV64TrapReentrySPOffset)
	f.a.Ld(enc.Scratch0, enc.X25, -abi.RV64TrapContinuationOffset)
	f.a.Jalr(enc.Zero, enc.Scratch0, 0)
}
