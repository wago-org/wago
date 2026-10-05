//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const maxSWARLocals = 128

type swarSummary struct{ locals, peak int }

// This private whole-function pilot consumes a frontend-validated function.
// It deliberately admits straight-line V128-only signatures/locals and the
// complete small bit-operation family. Module policy remains closed to SIMD;
// control, scalar/vector mixtures, memory and calls need their own lowering.
// Admission and emission walk every byte with identical bounded stack rules.
func walkSWARPairs(fn *wasm.Func, ft *wasm.CompType, emit *integerEmitter) (swarSummary, error) {
	if fn == nil || ft == nil || ft.Kind != wasm.CompFunc || len(ft.Params) > maxSWARLocals || len(ft.Results) > maxSWARLocals {
		return swarSummary{}, unsupported("SWAR signature bound")
	}
	for _, types := range [][]wasm.ValType{ft.Params, ft.Results} {
		for _, typ := range types {
			if !wasm.EqualValType(typ, wasm.V128) {
				return swarSummary{}, unsupported("SWAR pilot requires vector-only signatures")
			}
		}
	}
	nlocals := len(ft.Params)
	for _, run := range fn.Locals.Runs {
		if !wasm.EqualValType(run.Type, wasm.V128) || uint64(run.Count) > uint64(maxSWARLocals-nlocals) {
			return swarSummary{}, unsupported("SWAR local type/bound")
		}
		nlocals += int(run.Count)
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return swarSummary{}, unsupported("SWAR body bound")
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	h, peak, budget := 0, 0, 0
	left, right, mask := swarPair{enc.X5, enc.X6}, swarPair{enc.X7, enc.X9}, swarPair{enc.X10, enc.X11}
	local := func(i int) int32 { return int32(16 + 16*i) }
	spill := func(i int) int32 { return int32(16 + 16*nlocals + 16*i) }
	push := func() error {
		if h >= 256 {
			return unsupported("SWAR operand bound")
		}
		h++
		peak = max(peak, h)
		return nil
	}
	for r.HasNext() {
		budget++
		if budget > integerBudget {
			return swarSummary{}, unsupported("SWAR instruction budget")
		}
		op, err := r.Byte()
		if err != nil {
			return swarSummary{}, err
		}
		switch op {
		case 0x01: // nop
		case 0x0b:
			if r.HasNext() || h != len(ft.Results) {
				return swarSummary{}, unsupported("SWAR terminal stack/bytes")
			}
			if emit != nil {
				for j := 0; j < h; j++ {
					emit.swarLoad(spill(j), left)
					emit.a.Ld(enc.Scratch1, enc.SP, 8)
					emit.a.Sd(left.low, enc.Scratch1, int32(j*16))
					emit.a.Sd(left.high, enc.Scratch1, int32(j*16+8))
				}
			}
			return swarSummary{nlocals, peak}, nil
		case 0x1a:
			if h == 0 {
				return swarSummary{}, unsupported("SWAR drop underflow")
			}
			h--
		case 0x20, 0x21, 0x22:
			index, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if uint64(index) >= uint64(nlocals) {
				return swarSummary{}, unsupported("SWAR local index")
			}
			if op == 0x20 {
				if err := push(); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.swarLoad(local(int(index)), left)
					emit.swarStore(spill(h-1), left)
				}
			} else {
				if h == 0 {
					return swarSummary{}, unsupported("SWAR local write underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.swarStore(local(int(index)), left)
				}
				if op == 0x21 {
					h--
				}
			}
		case 0xfd:
			kind, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if kind == 12 {
				literal, err := r.Bytes(16)
				if err != nil {
					return swarSummary{}, err
				}
				if err := push(); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.a.Constant64(left.low, int64(binary.LittleEndian.Uint64(literal)))
					emit.a.Constant64(left.high, int64(binary.LittleEndian.Uint64(literal[8:])))
					emit.swarStore(spill(h-1), left)
				}
				continue
			}
			var semantic wasm.InstrKind
			arity := 2
			switch kind {
			case 77:
				semantic, arity = wasm.InstrV128Not, 1
			case 78:
				semantic = wasm.InstrV128And
			case 79:
				semantic = wasm.InstrV128Andnot
			case 80:
				semantic = wasm.InstrV128Or
			case 81:
				semantic = wasm.InstrV128Xor
			case 82:
				semantic, arity = wasm.InstrV128Bitselect, 3
			default:
				return swarSummary{}, unsupported("SWAR opcode lowering is pending")
			}
			if h < arity {
				return swarSummary{}, unsupported("SWAR bit operation underflow")
			}
			if emit != nil {
				emit.swarLoad(spill(h-arity), left)
				if arity > 1 {
					emit.swarLoad(spill(h-arity+1), right)
				}
				if arity > 2 {
					emit.swarLoad(spill(h-1), mask)
				}
				emit.swarBits(semantic, left, left, right, mask)
				emit.swarStore(spill(h-arity), left)
			}
			h -= arity - 1
		default:
			return swarSummary{}, unsupported("SWAR control/call/scalar lowering is pending")
		}
	}
	return swarSummary{}, fmt.Errorf("rv64: missing SWAR function end")
}

func compileSWARPairs(fn *wasm.Func, ft *wasm.CompType, mode emissionMode) ([]byte, FunctionInfo, error) {
	summary, err := walkSWARPairs(fn, ft, nil)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	f := integerEmitter{target: target{mode: mode}}
	actual, err := walkSWARPairs(fn, ft, &f)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if actual != summary {
		return nil, FunctionInfo{}, fmt.Errorf("rv64: SWAR admission changed")
	}
	if f.err != nil {
		return nil, FunctionInfo{}, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, f.a.Err()
	}
	widths := make([]bool, summary.locals*2)
	for i := range widths {
		widths[i] = true
	}
	info := FunctionInfo{Params: len(ft.Params) * 2, Results: len(ft.Results) * 2, SpillSlots: summary.peak * 2, FrameHeaderBytes: 16, Fallback: true, mode: mode}
	return frameFunction(f.a.B, widths, len(ft.Params)*2, summary.peak*2, true, -1, info)
}
