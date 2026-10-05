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
// It admits V128/i32 carriers, straight-line local/bit operations and typed
// select. An explicitly supplied nonshared memory32 descriptor also admits
// full-width vector loads/stores. Module policy remains closed to SIMD;
// structured control, other memory operations and calls need complete lowering.
// Admission and emission walk every byte with identical bounded stack rules.
func walkSWARPairs(fn *wasm.Func, ft *wasm.CompType, memory32 bool, emit *integerEmitter) (swarSummary, error) {
	if fn == nil || ft == nil || ft.Kind != wasm.CompFunc || len(ft.Params) > maxSWARLocals || len(ft.Results) > maxSWARLocals {
		return swarSummary{}, unsupported("SWAR signature bound")
	}
	for _, types := range [][]wasm.ValType{ft.Params, ft.Results} {
		for _, typ := range types {
			if !swarCarrierType(typ) {
				return swarSummary{}, unsupported("SWAR pilot carrier type")
			}
		}
	}
	var localVectors [maxSWARLocals]bool
	var paramSlots [maxSWARLocals]int
	incomingSlots := 0
	for i, typ := range ft.Params {
		paramSlots[i] = incomingSlots
		localVectors[i] = wasm.EqualValType(typ, wasm.V128)
		incomingSlots++
		if localVectors[i] {
			incomingSlots++
		}
	}
	nlocals := len(ft.Params)
	for _, run := range fn.Locals.Runs {
		if !swarCarrierType(run.Type) || uint64(run.Count) > uint64(maxSWARLocals-nlocals) {
			return swarSummary{}, unsupported("SWAR local type/bound")
		}
		for i := uint32(0); i < run.Count; i++ {
			localVectors[nlocals] = wasm.EqualValType(run.Type, wasm.V128)
			nlocals++
		}
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return swarSummary{}, unsupported("SWAR body bound")
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	h, peak, budget := 0, 0, 0
	left, right, mask := swarPair{enc.X5, enc.X6}, swarPair{enc.X7, enc.X9}, swarPair{enc.X10, enc.X11}
	local := func(i int) int32 { return int32(16 + 16*i) }
	spill := func(i int) int32 { return int32(16 + 16*nlocals + 16*i) }
	var stackVectors [256]bool
	push := func(vector bool) error {
		if h >= 256 {
			return unsupported("SWAR operand bound")
		}
		stackVectors[h] = vector
		h++
		peak = max(peak, h)
		return nil
	}
	// The shared frame helper copies compact serialized argument cells first.
	// Expand padded local homes backwards so a one-cell i32 cannot overwrite
	// the still unread halves of a later vector argument.
	if emit != nil && incomingSlots != len(ft.Params)*2 {
		for i := len(ft.Params) - 1; i >= 0; i-- {
			src := int32(16 + 8*paramSlots[i])
			if localVectors[i] {
				emit.swarLoad(src, left)
				emit.swarStore(local(i), left)
			} else {
				emit.Load(5, src, false)
				emit.Store(local(i), 5, true)
				emit.Store(local(i)+8, uint8(enc.Zero), true)
			}
		}
	}
	for r.HasNext() {
		if emit != nil && emit.mode.production {
			emit.sourcePC = uint32(fn.LocalDeclBytes) + uint32(r.Offset())
		}
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
			for j, typ := range ft.Results {
				if stackVectors[j] != wasm.EqualValType(typ, wasm.V128) {
					return swarSummary{}, unsupported("SWAR result carrier class")
				}
			}
			if emit != nil {
				out := int32(0)
				for j := 0; j < h; j++ {
					emit.swarLoad(spill(j), left)
					if !stackVectors[j] {
						emit.NormalizeI32(5)
					}
					emit.a.Ld(enc.Scratch1, enc.SP, 8)
					emit.a.Sd(left.low, enc.Scratch1, out)
					out += 8
					if stackVectors[j] {
						emit.a.Sd(left.high, enc.Scratch1, out)
						out += 8
					}
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
				if err := push(localVectors[index]); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.swarLoad(local(int(index)), left)
					emit.swarStore(spill(h-1), left)
				}
			} else {
				if h == 0 || stackVectors[h-1] != localVectors[index] {
					return swarSummary{}, unsupported("SWAR local write carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.swarStore(local(int(index)), left)
				}
				if op == 0x21 {
					h--
				}
			}
		case 0x41:
			value, err := r.I32()
			if err != nil {
				return swarSummary{}, err
			}
			if err := push(false); err != nil {
				return swarSummary{}, err
			}
			if emit != nil {
				emit.a.Constant32(left.low, uint32(value))
				emit.a.Addi(left.high, enc.Zero, 0)
				emit.swarStore(spill(h-1), left)
			}
		case 0x1b, 0x1c:
			vector := false
			if op == 0x1c {
				count, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if count != 1 {
					return swarSummary{}, unsupported("SWAR select type count")
				}
				typ, err := r.Byte()
				if err != nil {
					return swarSummary{}, err
				}
				if typ != 0x7f && typ != 0x7b {
					return swarSummary{}, unsupported("SWAR select carrier")
				}
				vector = typ == 0x7b
			}
			if h < 3 || stackVectors[h-1] || stackVectors[h-3] != vector || stackVectors[h-2] != vector {
				return swarSummary{}, unsupported("SWAR select carrier class/underflow")
			}
			if emit != nil {
				emit.Load(28, spill(h-1), false)
				other := emit.a.BranchLong(enc.EQ, enc.X28, enc.Zero)
				emit.swarLoad(spill(h-3), left)
				end := emit.Jump()
				if err := emit.Patch(other, emit.Position()); err != nil {
					return swarSummary{}, err
				}
				emit.swarLoad(spill(h-2), left)
				if err := emit.Patch(end, emit.Position()); err != nil {
					return swarSummary{}, err
				}
				emit.swarStore(spill(h-3), left)
			}
			h -= 2
		case 0xfd:
			kind, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if kind == 0 || kind == 11 {
				if !memory32 {
					return swarSummary{}, unsupported("SWAR memory32 descriptor is required")
				}
				align, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				// This pilot admits the implicit memory0 form only. In particular,
				// indexed/memory64 memargs cannot be misread as a memory32 offset.
				if align > 4 {
					return swarSummary{}, unsupported("SWAR full-width memarg alignment/index")
				}
				offset, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				store := kind == 11
				arity := 1
				if store {
					arity = 2
				}
				if h < arity || stackVectors[h-arity] || (store && !stackVectors[h-1]) {
					return swarSummary{}, unsupported("SWAR memory carrier class/underflow")
				}
				if emit != nil {
					if store {
						emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					}
					emit.Load(5, spill(h-arity), false)
					emit.swarMemory(uint64(offset), store)
					if !store {
						emit.swarStore(spill(h-1), swarPair{enc.X6, enc.X7})
					}
				}
				if store {
					h -= 2
				} else {
					stackVectors[h-1] = true
				}
				continue
			}
			if kind == 12 {
				literal, err := r.Bytes(16)
				if err != nil {
					return swarSummary{}, err
				}
				if err := push(true); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.a.Constant64(left.low, int64(binary.LittleEndian.Uint64(literal)))
					emit.a.Constant64(left.high, int64(binary.LittleEndian.Uint64(literal[8:])))
					emit.swarStore(spill(h-1), left)
				}
				continue
			}
			if kind == 83 {
				if h == 0 || !stackVectors[h-1] {
					return swarSummary{}, unsupported("SWAR any_true carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.a.Or(left.low, left.low, left.high)
					emit.a.Sltu(left.low, enc.Zero, left.low)
					emit.a.Addi(left.high, enc.Zero, 0)
					emit.swarStore(spill(h-1), left)
				}
				stackVectors[h-1] = false
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
			for j := h - arity; j < h; j++ {
				if !stackVectors[j] {
					return swarSummary{}, unsupported("SWAR bit operation carrier class")
				}
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
	return compileSWARPairsWithMemory(fn, ft, nil, mode)
}

// memory must describe the caller's validated single nonshared memory32 and
// its production JobMemory home. This private entry never admits a module.
func compileSWARPairsWithMemory(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, mode emissionMode) ([]byte, FunctionInfo, error) {
	if memory != nil && (!mode.production || memory.Shared || memory.Limits.Addr64 || memory.Limits.Min > 65536 ||
		(memory.Limits.HasMax && (memory.Limits.Max < memory.Limits.Min || memory.Limits.Max > 65536))) {
		return nil, FunctionInfo{}, unsupported("SWAR production memory32 resource")
	}
	summary, err := walkSWARPairs(fn, ft, memory != nil, nil)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	f := integerEmitter{target: target{mode: mode}}
	actual, err := walkSWARPairs(fn, ft, memory != nil, &f)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if actual != summary {
		return nil, FunctionInfo{}, fmt.Errorf("rv64: SWAR admission changed")
	}
	if len(f.traps) != 0 {
		end := f.Jump()
		for _, tr := range f.traps {
			if err := f.Patch(tr.site, f.Position()); err != nil {
				return nil, FunctionInfo{}, err
			}
			f.emitProductionTrap(productionTrapCode(tr.code), tr.sourcePC)
		}
		if err := f.Patch(end, f.Position()); err != nil {
			return nil, FunctionInfo{}, err
		}
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
	params, results := swarSignatureSlots(ft.Params), swarSignatureSlots(ft.Results)
	info := FunctionInfo{Params: params, Results: results, SpillSlots: summary.peak * 2, FrameHeaderBytes: 16, Fallback: true, mode: mode}
	return frameFunction(f.a.B, widths, params, summary.peak*2, true, -1, info)
}

func swarCarrierType(typ wasm.ValType) bool {
	return wasm.EqualValType(typ, wasm.V128) || wasm.EqualValType(typ, wasm.I32)
}
func swarSignatureSlots(types []wasm.ValType) int {
	slots := len(types)
	for _, typ := range types {
		if wasm.EqualValType(typ, wasm.V128) {
			slots++
		}
	}
	return slots
}
