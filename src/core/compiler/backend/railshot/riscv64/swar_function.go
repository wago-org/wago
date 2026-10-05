//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const maxSWARLocals = 128

type swarSummary struct {
	locals, peak, codeBound int
	usesFP                  bool
}

// This private whole-function pilot consumes a frontend-validated function.
// It admits V128 and four scalar carriers, straight-line local/integer vector operations
// and select. An explicitly supplied nonshared memory32 descriptor also admits
// vector loads/stores. FP instructions require the production FCSR bridge.
// Module policy remains closed to SIMD; structured control and calls need lowering.
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
	var localClasses [maxSWARLocals]swarClass
	var paramSlots [maxSWARLocals]int
	incomingSlots := 0
	for i, typ := range ft.Params {
		paramSlots[i] = incomingSlots
		localClasses[i] = swarClassOf(typ)
		incomingSlots++
		if localClasses[i] == swarV128 {
			incomingSlots++
		}
	}
	nlocals := len(ft.Params)
	for _, run := range fn.Locals.Runs {
		if !swarCarrierType(run.Type) || uint64(run.Count) > uint64(maxSWARLocals-nlocals) {
			return swarSummary{}, unsupported("SWAR local type/bound")
		}
		for i := uint32(0); i < run.Count; i++ {
			localClasses[nlocals] = swarClassOf(run.Type)
			nlocals++
		}
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return swarSummary{}, unsupported("SWAR body bound")
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	h, peak, budget := 0, 0, 0
	usesFP := false
	// Covers bounded parameter expansion, frame/zero initialization, compact
	// result serialization and framing. Per-op bounds include escaped homes
	// and cold trap islands. Admission computes this before allocating code.
	codeBound := 64 << 10
	reserve := func(size int) error {
		if size > MaxCodeBytes-codeBound {
			return unsupported("SWAR emitted code budget")
		}
		codeBound += size
		return nil
	}
	left, right, mask := swarPair{enc.X5, enc.X6}, swarPair{enc.X7, enc.X9}, swarPair{enc.X10, enc.X11}
	local := func(i int) int32 { return int32(16 + 16*i) }
	spill := func(i int) int32 { return int32(16 + 16*nlocals + 16*i) }
	var stackClasses [256]swarClass
	push := func(class swarClass) error {
		if h >= 256 {
			return unsupported("SWAR operand bound")
		}
		stackClasses[h] = class
		h++
		peak = max(peak, h)
		return nil
	}
	// The shared frame helper copies compact serialized argument cells first.
	// Expand padded local homes backwards so a one-cell scalar cannot overwrite
	// the still unread halves of a later vector argument.
	if emit != nil && incomingSlots != len(ft.Params)*2 {
		for i := len(ft.Params) - 1; i >= 0; i-- {
			src := int32(16 + 8*paramSlots[i])
			if localClasses[i] == swarV128 {
				emit.swarLoad(src, left)
				emit.swarStore(local(i), left)
			} else {
				emit.Load(5, src, !localClasses[i].narrow())
				emit.Store(local(i), 5, true)
				emit.Store(local(i)+8, uint8(enc.Zero), true)
			}
		}
	}
	for r.HasNext() {
		if emit != nil && len(emit.a.B) > codeBound {
			return swarSummary{}, fmt.Errorf("rv64: SWAR body exceeded its admitted bound")
		}
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
		if op != 0xfd && op != 0x01 && op != 0x0b && op != 0x1a {
			bound := 256
			if op == 0x1b || op == 0x1c {
				bound = 512
			}
			if err := reserve(bound); err != nil {
				return swarSummary{}, err
			}
		}
		switch op {
		case 0x01: // nop
		case 0x0b:
			if r.HasNext() || h != len(ft.Results) {
				return swarSummary{}, unsupported("SWAR terminal stack/bytes")
			}
			for j, typ := range ft.Results {
				if stackClasses[j] != swarClassOf(typ) {
					return swarSummary{}, unsupported("SWAR result carrier class")
				}
			}
			if emit != nil {
				out := int32(0)
				for j := 0; j < h; j++ {
					emit.swarLoad(spill(j), left)
					if stackClasses[j].narrow() {
						emit.NormalizeI32(5)
					}
					emit.a.Ld(enc.Scratch1, enc.SP, 8)
					emit.a.Sd(left.low, enc.Scratch1, out)
					out += 8
					if stackClasses[j] == swarV128 {
						emit.a.Sd(left.high, enc.Scratch1, out)
						out += 8
					}
				}
			}
			return swarSummary{nlocals, peak, codeBound, usesFP}, nil
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
				if err := push(localClasses[index]); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.swarLoad(local(int(index)), left)
					emit.swarStore(spill(h-1), left)
				}
			} else {
				if h == 0 || stackClasses[h-1] != localClasses[index] {
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
		case 0x41, 0x42, 0x43, 0x44:
			class, bits := swarI32, uint64(0)
			switch op {
			case 0x41:
				value, err := r.I32()
				if err != nil {
					return swarSummary{}, err
				}
				bits = uint64(uint32(value))
			case 0x42:
				value, err := r.I64()
				if err != nil {
					return swarSummary{}, err
				}
				class, bits = swarI64, uint64(value)
			case 0x43:
				value, err := r.Bytes(4)
				if err != nil {
					return swarSummary{}, err
				}
				class, bits = swarF32, uint64(binary.LittleEndian.Uint32(value))
			case 0x44:
				value, err := r.Bytes(8)
				if err != nil {
					return swarSummary{}, err
				}
				class, bits = swarF64, binary.LittleEndian.Uint64(value)
			}
			if err := push(class); err != nil {
				return swarSummary{}, err
			}
			if emit != nil {
				if class.narrow() {
					emit.a.Constant32(left.low, uint32(bits))
				} else {
					emit.a.Constant64(left.low, int64(bits))
				}
				emit.a.Addi(left.high, enc.Zero, 0)
				emit.swarStore(spill(h-1), left)
			}
		case 0x1b, 0x1c:
			class := swarInvalid
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
				class = swarByteClass(typ)
				if class == swarInvalid {
					return swarSummary{}, unsupported("SWAR select carrier")
				}
			}
			if h < 3 || stackClasses[h-1] != swarI32 {
				return swarSummary{}, unsupported("SWAR select condition class/underflow")
			}
			if op == 0x1b {
				class = stackClasses[h-3]
				if class == swarV128 || class == swarInvalid {
					return swarSummary{}, unsupported("SWAR untyped select carrier")
				}
			}
			if stackClasses[h-3] != class || stackClasses[h-2] != class {
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
			bound := 512
			_, memoryOp := swarMemoryOpcode(kind)
			if memoryOp || kind == 130 || kind == 186 {
				bound = 1024
			} else if _, _, ok := swarFloatRoundOpcode(kind); ok {
				bound = 1024
			} else if _, _, _, ok := swarFloatMinMaxOpcode(kind); ok {
				bound = 1024
			} else if kind == 13 {
				bound = 768
			} else if kind == 14 {
				bound = 2048
			} else if _, _, ok := swarComparisonOpcode(kind); ok {
				bound = 1024
			} else if _, _, ok := swarNarrowOpcode(kind); ok {
				bound = 2048
			} else if _, _, ok := swarSaturationOpcode(kind); ok {
				bound = 2048
			} else if _, _, ok := swarProductOpcode(kind); ok {
				bound = 1024
			} else if _, _, ok := swarShiftOpcode(kind); ok {
				bound = 768
			} else if _, operation, ok := swarUnaryOpcode(kind); ok && (operation == swarAbs || operation == swarNeg) {
				bound = 768
			}
			if err := reserve(bound); err != nil {
				return swarSummary{}, err
			}
			if width, mode, ok := swarFloatRoundOpcode(kind); ok {
				usesFP = true
				if h < 1 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR floating rounding carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.swarFloatRound(width, mode)
					emit.swarStore(spill(h-1), left)
				}
				continue
			}
			if width, maximum, pseudo, ok := swarFloatMinMaxOpcode(kind); ok {
				usesFP = true
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR floating min/max carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarFloatMinMax(width, maximum, pseudo)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, operation, unary, ok := swarFloatMathOpcode(kind); ok {
				usesFP = usesFP || operation >= swarFloatSqrt
				arity := 2
				if unary {
					arity = 1
				}
				if h < arity || stackClasses[h-arity] != swarV128 || (!unary && stackClasses[h-1] != swarV128) {
					return swarSummary{}, unsupported("SWAR floating arithmetic carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-arity), left)
					if !unary {
						emit.swarLoad(spill(h-1), right)
					}
					emit.swarFloatMath(width, operation)
					emit.swarStore(spill(h-arity), left)
				}
				h -= arity - 1
				continue
			}
			if width, relation, ok := swarFloatComparisonOpcode(kind); ok {
				usesFP = true
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR floating comparison carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarFloatCompare(width, relation)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, signed, high, ok := swarExtmultiplyOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR extmultiply carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarExtmultiply(width, signed, high)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, signed, ok := swarNarrowOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR narrowing carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarNarrow(width, signed)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, signed, high, pairwise, ok := swarWidenOpcode(kind); ok {
				if h == 0 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR widening carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					emit.swarWiden(width, signed, high, pairwise)
					emit.swarStore(spill(h-1), swarPair{enc.X6, enc.X7})
				}
				continue
			}
			if kind == 98 {
				if h == 0 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR popcount carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					emit.swarPopcnt()
					emit.swarStore(spill(h-1), swarPair{enc.X6, enc.X7})
				}
				continue
			}
			if kind == 130 || kind == 186 {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR special multiply carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarSpecialMultiply(kind)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, operation, ok := swarSaturationOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR saturation carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarSaturate(width, operation)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, operation, ok := swarProductOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR multiply/minmax carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarProduct(width, operation)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, operation, ok := swarUnaryOpcode(kind); ok {
				if h == 0 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR unary carrier class/underflow")
				}
				scalar := operation == swarAllTrue || operation == swarBitmask
				if emit != nil {
					emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					emit.swarUnary(width, operation)
					if scalar {
						emit.a.Addi(enc.X6, enc.Zero, 0)
						emit.swarStore(spill(h-1), swarPair{enc.X5, enc.X6})
					} else {
						emit.swarStore(spill(h-1), swarPair{enc.X6, enc.X7})
					}
				}
				stackClasses[h-1] = swarV128
				if scalar {
					stackClasses[h-1] = swarI32
				}
				continue
			}
			if kind == 13 || kind == 14 {
				var indices [16]byte
				if kind == 13 {
					bytes, err := r.Bytes(16)
					if err != nil {
						return swarSummary{}, err
					}
					copy(indices[:], bytes)
					for _, index := range indices {
						if index >= 32 {
							return swarSummary{}, unsupported("SWAR shuffle byte index")
						}
					}
				}
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR permutation carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					if kind == 13 {
						emit.swarShuffle(indices)
					} else {
						emit.swarSwizzle()
					}
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, relation, ok := swarComparisonOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR comparison carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarCompare(width, relation)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, subtract, ok := swarAddSubOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR arithmetic carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), left)
					emit.swarLoad(spill(h-1), right)
					emit.swarAddSub(width, subtract)
					emit.swarStore(spill(h-2), left)
				}
				h--
				continue
			}
			if width, direction, ok := swarShiftOpcode(kind); ok {
				if h < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarI32 {
					return swarSummary{}, unsupported("SWAR shift carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-2), swarPair{enc.X6, enc.X7})
					emit.Load(5, spill(h-1), false)
					emit.swarShift(width, direction)
					emit.swarStore(spill(h-2), swarPair{enc.X6, enc.X7})
				}
				h--
				continue
			}
			if kind >= 15 && kind <= 20 {
				class, width := swarI32, uint8(8<<(kind-15))
				switch kind {
				case 18:
					class, width = swarI64, 64
				case 19:
					class, width = swarF32, 32
				case 20:
					class, width = swarF64, 64
				}
				if h == 0 || stackClasses[h-1] != class {
					return swarSummary{}, unsupported("SWAR splat carrier class/underflow")
				}
				if emit != nil {
					emit.Load(5, spill(h-1), !class.narrow())
					emit.swarSplat(width)
					emit.swarStore(spill(h-1), swarPair{enc.X6, enc.X7})
				}
				stackClasses[h-1] = swarV128
				continue
			}
			if kind >= 21 && kind <= 34 {
				lane, err := r.Byte()
				if err != nil {
					return swarSummary{}, err
				}
				class, width := swarI32, uint8(8)
				if kind >= 24 {
					width = 16
				}
				if kind >= 27 {
					width = 32
				}
				if kind == 29 || kind == 30 {
					class, width = swarI64, 64
				} else if kind == 31 || kind == 32 {
					class, width = swarF32, 32
				} else if kind == 33 || kind == 34 {
					class, width = swarF64, 64
				}
				if lane >= 128/width {
					return swarSummary{}, unsupported("SWAR lane immediate bound")
				}
				replace := kind == 23 || kind == 26 || kind == 28 || kind == 30 || kind == 32 || kind == 34
				arity := 1
				if replace {
					arity = 2
				}
				if h < arity || stackClasses[h-arity] != swarV128 || (replace && stackClasses[h-1] != class) {
					return swarSummary{}, unsupported("SWAR lane carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-arity), swarPair{enc.X6, enc.X7})
					if replace {
						emit.Load(5, spill(h-1), !class.narrow())
						emit.swarReplace(width, lane)
						emit.swarStore(spill(h-2), swarPair{enc.X6, enc.X7})
					} else {
						emit.swarExtract(width, lane, kind == 21 || kind == 24)
						emit.a.Addi(enc.X6, enc.Zero, 0)
						emit.swarStore(spill(h-1), swarPair{enc.X5, enc.X6})
					}
				}
				if replace {
					h--
				} else {
					stackClasses[h-1] = class
				}
				continue
			}
			if maxAlign, ok := swarMemoryOpcode(kind); ok {
				if !memory32 {
					return swarSummary{}, unsupported("SWAR memory32 descriptor is required")
				}
				align, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				// This pilot admits the implicit memory0 form only. In particular,
				// indexed/memory64 memargs cannot be misread as a memory32 offset.
				if align > maxAlign {
					return swarSummary{}, unsupported("SWAR memarg alignment/index")
				}
				offset, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				laneOp := kind >= 84 && kind <= 91
				lane := byte(0)
				if laneOp {
					lane, err = r.Byte()
					if err != nil {
						return swarSummary{}, err
					}
					if uint32(lane) >= 16>>maxAlign {
						return swarSummary{}, unsupported("SWAR memory lane index")
					}
				}
				store := kind == 11 || (kind >= 88 && kind <= 91)
				arity := 1
				if store || laneOp {
					arity = 2
				}
				if h < arity || stackClasses[h-arity] != swarI32 || ((store || laneOp) && stackClasses[h-1] != swarV128) {
					return swarSummary{}, unsupported("SWAR memory carrier class/underflow")
				}
				if emit != nil {
					if store || laneOp {
						emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					}
					emit.Load(5, spill(h-arity), false)
					if kind == 0 || kind == 11 {
						emit.swarMemory(uint64(offset), store)
					} else if laneOp {
						emit.swarLaneMemory(uint64(offset), uint8(8<<maxAlign), lane, store)
					} else if kind >= 7 && kind <= 10 {
						emit.swarSplatMemory(uint64(offset), uint8(8<<maxAlign))
					} else {
						emit.swarPartialLoad(uint64(offset), kind)
					}
					if !store {
						emit.swarStore(spill(h-arity), swarPair{enc.X6, enc.X7})
					}
				}
				if store {
					h -= 2
				} else {
					h -= arity - 1
					stackClasses[h-1] = swarV128
				}
				continue
			}
			if kind == 12 {
				literal, err := r.Bytes(16)
				if err != nil {
					return swarSummary{}, err
				}
				if err := push(swarV128); err != nil {
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
				if h == 0 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR any_true carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.a.Or(left.low, left.low, left.high)
					emit.a.Sltu(left.low, enc.Zero, left.low)
					emit.a.Addi(left.high, enc.Zero, 0)
					emit.swarStore(spill(h-1), left)
				}
				stackClasses[h-1] = swarI32
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
				if stackClasses[j] != swarV128 {
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
	if summary.usesFP && !mode.production {
		// The isolated integer bridge does not preserve accrued FP flags/FCSR.
		return nil, FunctionInfo{}, unsupported("SWAR FP requires production FCSR bridge")
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
	code, info, err := frameFunction(f.a.B, widths, params, summary.peak*2, true, -1, info)
	if err != nil {
		return nil, FunctionInfo{}, err
	}
	if len(code) > summary.codeBound {
		return nil, FunctionInfo{}, fmt.Errorf("rv64: SWAR code exceeded its admitted bound")
	}
	return code, info, nil
}

func swarCarrierType(typ wasm.ValType) bool {
	return swarClassOf(typ) != swarInvalid
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
