//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

const maxSWARLocals = 128

type swarSummary struct {
	locals, peak, codeBound, calls int
	usesFP, usesTraps              bool
	usesLoops                      bool
}

// This private whole-function pilot consumes a frontend-validated function.
// It admits V128 and four scalar carriers, local/scalar/vector
// operations and select. An explicitly supplied nonshared memory32 descriptor
// also admits scalar/vector loads/stores and memory size/growth/copy/fill.
// FP and traps require their production bridges.
// Structured control uses exact classes and canonical branch homes; indexed
// signatures require an explicit type directory and loops require interruption.
// Module policy remains closed to SIMD. Direct calls require an explicit
// same-instance function directory and the production call bridge.
// Admission and emission walk every byte with identical bounded stack rules.
func walkSWARPairs(fn *wasm.Func, ft *wasm.CompType, memory32 bool, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithTypes(fn, ft, memory32, nil, emit)
}

func walkSWARPairsWithTypes(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types []*wasm.CompType, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithCalls(fn, ft, memory32, types, nil, emit)
}

func walkSWARPairsWithCalls(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithGlobals(fn, ft, memory32, types, functions, nil, emit)
}

func walkSWARPairsWithGlobals(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithData(fn, ft, memory32, types, functions, globals, 0, emit)
}

func walkSWARPairsWithData(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithTable(fn, ft, memory32, types, functions, globals, dataCount, nil, emit)
}

func walkSWARPairsWithTable(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithIndirectKeys(fn, ft, memory32, types, functions, globals, dataCount, table, nil, emit)
}

func walkSWARPairsWithIndirectKeys(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithElements(fn, ft, memory32, types, functions, globals, dataCount, table, typeKeys, 0, emit)
}

func walkSWARPairsWithElements(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount int, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithImports(fn, ft, memory32, types, functions, globals, dataCount, table, typeKeys, elementCount, 0, emit)
}

// importedFunctions is an explicit, immutable numeric/vector prefix of the actual
// validated function directory. Imports use live same-instance dynamic dispatch.
// The legacy entry keeps tables separate; the retained public profile can
// combine them while imported reference proxies remain unsupported.
func walkSWARPairsWithImports(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount, importedFunctions int, emit *integerEmitter) (swarSummary, error) {
	return walkSWARPairsWithImportMode(fn, ft, memory32, types, functions, globals, dataCount, table, typeKeys, elementCount, importedFunctions, false, false, emit)
}

func walkSWARPairsWithImportMode(fn *wasm.Func, ft *wasm.CompType, memory32 bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount, importedFunctions int, foreignTableCalls, borrowedTable bool, emit *integerEmitter) (swarSummary, error) {
	count := 0
	if memory32 {
		count = 1
	}
	return walkSWARPairsWithMemoryMode(fn, ft, count, false, types, functions, globals, dataCount, table, typeKeys, elementCount, importedFunctions, foreignTableCalls, borrowedTable, emit)
}
func walkSWARPairsWithMemoryMode(fn *wasm.Func, ft *wasm.CompType, memoryCount int, indexedMemories bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount, importedFunctions int, foreignTableCalls, borrowedTable bool, emit *integerEmitter) (swarSummary, error) {
	if memoryCount < 0 || memoryCount > MaxProductionMemories || !indexedMemories && memoryCount > 1 {
		return swarSummary{}, unsupported("SWAR memory count bound")
	}

	if importedFunctions < 0 || importedFunctions > MaxHostFunctionImports || importedFunctions > len(functions) || importedFunctions != 0 && (table != nil && !foreignTableCalls || len(functions) == importedFunctions) {
		return swarSummary{}, unsupported("SWAR import prefix bound/defined functions/table")
	}
	if elementCount < 0 || elementCount > MaxOwnedElementSegments || elementCount != 0 && table == nil && !foreignTableCalls {
		return swarSummary{}, unsupported("SWAR owned element directory bound/table")
	}
	if typeKeys != nil && (table == nil || len(typeKeys) != len(types) || len(types) == 0 || len(functions) == 0) {
		return swarSummary{}, unsupported("SWAR indirect type identity directory/owner")
	}
	if err := swarOwnedTableType(table); err != nil {
		return swarSummary{}, err
	}
	privateRefs := table != nil || foreignTableCalls
	descriptorRefs := privateRefs
	if dataCount < 0 || dataCount > 256 {
		return swarSummary{}, unsupported("SWAR passive data directory bound")
	}
	if len(globals) > 256 {
		return swarSummary{}, unsupported("SWAR global directory bound")
	}
	for _, global := range globals {
		reference := foreignTableCalls && !global.Mutable && wasm.EqualValType(global.Type, wasm.FuncRef)
		if !swarCarrierType(global.Type) && !reference {
			return swarSummary{}, unsupported("SWAR global carrier")
		}
	}
	if len(functions) > MaxFunctions {
		return swarSummary{}, unsupported("SWAR function directory bound")
	}
	for i, typ := range functions {
		if err := swarCallSignature(typ); err != nil {
			return swarSummary{}, err
		}
		if i < importedFunctions {
			if err := swarImportSignature(typ); err != nil {
				return swarSummary{}, err
			}
		}
	}
	if len(types) > 1024 {
		return swarSummary{}, unsupported("SWAR control type bound")
	}
	for _, typ := range types {
		if typ == nil || typ.Kind != wasm.CompFunc || len(typ.Params) > maxSWARLocals || len(typ.Results) > maxSWARLocals {
			return swarSummary{}, unsupported("SWAR control type shape/bound")
		}
		for _, list := range [][]wasm.ValType{typ.Params, typ.Results} {
			for _, carrier := range list {
				if !swarCarrierType(carrier) {
					return swarSummary{}, unsupported("SWAR control type carrier")
				}
			}
		}
	}
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
		if (!swarCarrierType(run.Type) && !(privateRefs && wasm.EqualValType(run.Type, wasm.FuncRef))) || uint64(run.Count) > uint64(maxSWARLocals-nlocals) {
			return swarSummary{}, unsupported("SWAR local type/bound")
		}
		for i := uint32(0); i < run.Count; i++ {
			localClasses[nlocals] = swarClassOf(run.Type)
			if privateRefs && wasm.EqualValType(run.Type, wasm.FuncRef) {
				localClasses[nlocals] = swarFuncRef
			}
			nlocals++
		}
	}
	if len(fn.BodyBytes) == 0 || len(fn.BodyBytes) > 64<<10 {
		return swarSummary{}, unsupported("SWAR body bound")
	}
	r := wasm.ReaderFrom(fn.BodyBytes)
	h, peak, budget := 0, 0, 0
	usesFP, usesTraps := false, false
	calls := 0
	header := 16
	if len(functions) != 0 {
		header = callFrameHeader
	}
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
	local := func(i int) int32 { return int32(header + 16*i) }
	spill := func(i int) int32 { return int32(header + 16*nlocals + 16*i) }
	var stackClasses [256]swarClass
	var flow swarFlow
	push := func(class swarClass) error {
		if h >= 256 {
			return unsupported("SWAR operand bound")
		}
		stackClasses[h] = class
		h++
		if flow.live {
			peak = max(peak, h)
		}
		return nil
	}
	flow = swarFlow{depth: 1, live: true, h: &h, peak: &peak, budget: &budget, classes: &stackClasses, spill: spill, reserve: reserve, emit: emit, types: types, privateRefs: privateRefs}
	flow.frames[0] = swarControl{kind: 0xff, entry: true}
	for _, typ := range ft.Results {
		flow.frames[0].results = append(flow.frames[0].results, swarClassOf(typ))
	}
	// The shared frame helper copies compact serialized argument cells first.
	// Expand padded local homes backwards so a one-cell scalar cannot overwrite
	// the still unread halves of a later vector argument.
	if emit != nil && incomingSlots != len(ft.Params)*2 {
		for i := len(ft.Params) - 1; i >= 0; i-- {
			src := int32(header + 8*paramSlots[i])
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
	emitter := emit
	for r.HasNext() {
		emit = emitter
		if !flow.live {
			emit = nil
		}
		if emitter != nil && len(emitter.a.B) > codeBound {
			return swarSummary{}, fmt.Errorf("rv64: SWAR body exceeded its admitted bound")
		}
		if emitter != nil && emitter.mode.production {
			emitter.sourcePC = uint32(fn.LocalDeclBytes) + uint32(r.Offset())
		}
		budget++
		if budget > integerBudget {
			return swarSummary{}, unsupported("SWAR instruction budget")
		}
		op, err := r.Byte()
		if err != nil {
			return swarSummary{}, err
		}
		if op != 0xfd && op != 0xfc && op != 0x01 && op != 0x0b && op != 0x1a {
			bound := 256
			_, _, _, _, integer := integerOpcode(op)
			if op == 0x1b || op == 0x1c || integer {
				bound = 512
			}
			if _, floating := swarScalarFloatOpcode(op); floating {
				bound = 1024
			}
			if err := reserve(bound); err != nil {
				return swarSummary{}, err
			}
		}
		switch op {
		case 0x01: // nop
		case 0x00, 0x02, 0x03, 0x04, 0x05, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f:
			done, trap, err := flow.instruction(op, &r)
			if err != nil {
				return swarSummary{}, err
			}
			usesTraps = usesTraps || trap
			if !done {
				continue
			}
			emit = emitter // A live root-label join can follow a dead source tail.
			if r.HasNext() || h != len(ft.Results) {
				return swarSummary{}, unsupported("SWAR terminal stack/bytes")
			}
			for j, typ := range ft.Results {
				if stackClasses[j] != swarClassOf(typ) {
					return swarSummary{}, unsupported("SWAR result carrier class")
				}
			}
			if flow.live && emit != nil {
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
			return swarSummary{nlocals, peak, codeBound, calls, usesFP, usesTraps, flow.usesLoops}, nil
		case 0x10:
			index, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if uint64(index) >= uint64(len(functions)) {
				return swarSummary{}, unsupported("SWAR direct call directory/index")
			}
			sig := functions[index]
			calls++
			budget += len(sig.Params) + len(sig.Results)
			if calls > maxDirectCalls || budget > integerBudget {
				return swarSummary{}, unsupported("SWAR direct call count/transfer budget")
			}
			callBound := 128
			if index < uint32(importedFunctions) {
				callBound = 1024 // dynamic guards and their source-attributed cold islands
				usesTraps = true
			}
			if err := reserve(128*(len(sig.Params)+len(sig.Results)) + callBound); err != nil {
				return swarSummary{}, err
			}
			params := make([]swarClass, len(sig.Params))
			for i, typ := range sig.Params {
				params[i] = swarClassOf(typ)
			}
			if err := flow.prepareDead(params...); err != nil {
				return swarSummary{}, err
			}
			base := h - len(params)
			if base < flow.floor() {
				return swarSummary{}, unsupported("SWAR direct call operand underflow")
			}
			for i, class := range params {
				if stackClasses[base+i] != class {
					return swarSummary{}, unsupported("SWAR direct call carrier class")
				}
			}
			if base+len(sig.Results) > len(stackClasses) {
				return swarSummary{}, unsupported("SWAR direct call result operand bound")
			}
			if emit != nil {
				if index < uint32(importedFunctions) {
					emit.swarImportCall(index, sig, base, spill)
				} else {
					emit.swarDirectCall(index-uint32(importedFunctions), sig, base, spill)
				}
			}
			h = base
			for _, typ := range sig.Results {
				if err := push(swarClassOf(typ)); err != nil {
					return swarSummary{}, err
				}
			}
		case 0x11:
			typeIndex, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			tableIndex, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if table == nil || typeKeys == nil || uint64(typeIndex) >= uint64(len(types)) || tableIndex != 0 {
				return swarSummary{}, unsupported("SWAR indirect signature/table identity")
			}
			sig := types[typeIndex]
			calls++
			budget += len(sig.Params) + len(sig.Results) + 1
			if calls > maxDirectCalls || budget > integerBudget {
				return swarSummary{}, unsupported("SWAR indirect call count/transfer budget")
			}
			if err := reserve(128*(len(sig.Params)+len(sig.Results)) + 1024); err != nil {
				return swarSummary{}, err
			}
			if foreignTableCalls {
				if err := reserve(nativeTableCallCodeBound(swarSignatureSlots(sig.Params), swarSignatureSlots(sig.Results))); err != nil {
					return swarSummary{}, err
				}
			}
			operands := make([]swarClass, len(sig.Params)+1)
			for i, typ := range sig.Params {
				operands[i] = swarClassOf(typ)
			}
			operands[len(sig.Params)] = swarI32
			if err := flow.prepareDead(operands...); err != nil {
				return swarSummary{}, err
			}
			base := h - len(operands)
			if base < flow.floor() {
				return swarSummary{}, unsupported("SWAR indirect call operand underflow")
			}
			for i, class := range operands {
				if stackClasses[base+i] != class {
					return swarSummary{}, unsupported("SWAR indirect call carrier class")
				}
			}
			usesTraps = true
			if emit != nil {
				emit.swarIndirectCall(sig, typeKeys[typeIndex], base, h-1, spill)
			}
			h = base
			for _, typ := range sig.Results {
				if err := push(swarClassOf(typ)); err != nil {
					return swarSummary{}, err
				}
			}
		case 0x23, 0x24:
			index, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if uint64(index) >= uint64(len(globals)) {
				return swarSummary{}, unsupported("SWAR global directory/index")
			}
			global := globals[index]
			class := swarGlobalClass(global.Type)
			if op == 0x24 {
				if !global.Mutable {
					return swarSummary{}, unsupported("SWAR immutable global write")
				}
				if err := flow.prepareDead(class); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != class {
					return swarSummary{}, unsupported("SWAR global write carrier/underflow")
				}
				h--
				if emit != nil {
					emit.Load(5, spill(h), !class.narrow())
					if class == swarV128 {
						emit.Load(6, spill(h)+8, true)
					}
					emit.swarGlobal(index, class, true)
				}
			} else {
				if err := push(class); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.swarGlobal(index, class, false)
					emit.Store(spill(h-1), 5, true)
					emit.Store(spill(h-1)+8, 6, true)
				}
			}
		case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40:
			if memoryCount == 0 {
				return swarSummary{}, unsupported("SWAR scalar memory without resource")
			}
			var imm wasm.InstructionImmediate
			if err := wasm.ClassifyInstructionImmediateIntoWithFeatures(&r, op, &imm, false, indexedMemories); err != nil {
				return swarSummary{}, err
			}
			memoryIndex := imm.MemIndex
			if op == 0x3f || op == 0x40 {
				memoryIndex = imm.Index
			}
			if uint64(memoryIndex) >= uint64(memoryCount) || !indexedMemories && (memoryIndex != 0 || imm.HasMemIndex) || imm.MemOffset > uint64(^uint32(0)) {
				return swarSummary{}, unsupported("SWAR scalar memory index/offset")
			}
			if err := reserve(768); err != nil { // 1024 including the opcode's base bound.
				return swarSummary{}, err
			}
			if op == 0x3f || op == 0x40 {
				if op == 0x40 {
					if err := flow.prepareDead(swarI32); err != nil {
						return swarSummary{}, err
					}
					if h == flow.floor() || stackClasses[h-1] != swarI32 {
						return swarSummary{}, unsupported("SWAR memory grow delta carrier")
					}
					h--
				}
				if err := push(swarI32); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					if op == 0x40 {
						emit.Load(5, spill(h-1), false)
					}
					emit.memorySizeGrowAt(memoryIndex, op == 0x40)
					emit.Store(spill(h-1), 5, true)
					emit.Store(spill(h-1)+8, uint8(enc.Zero), true)
				}
				continue
			}
			size, wide, signed, store, _ := memoryOpcode(op)
			if imm.MemAlign > uint32(swarMemoryAlignment(size)) {
				return swarSummary{}, unsupported("SWAR scalar memory alignment")
			}
			class := swarI32
			if wide {
				class = swarI64
			}
			if op == 0x2a || op == 0x38 {
				class = swarF32
			} else if op == 0x2b || op == 0x39 {
				class = swarF64
			}
			inputs := []swarClass{swarI32}
			if store {
				inputs = append(inputs, class)
			}
			if err := flow.prepareDead(inputs...); err != nil {
				return swarSummary{}, err
			}
			base := h - len(inputs)
			if base < flow.floor() {
				return swarSummary{}, unsupported("SWAR scalar memory operand underflow")
			}
			for i, input := range inputs {
				if stackClasses[base+i] != input {
					return swarSummary{}, unsupported("SWAR scalar memory carrier class")
				}
			}
			usesTraps = true
			if emit != nil {
				if store {
					emit.Load(6, spill(base+1), wide)
				}
				emit.Load(5, spill(base), false)
				emit.memoryAccessAt(memoryIndex, imm.MemOffset, size, wide, signed, store)
				if !store {
					emit.Store(spill(base), 5, true)
					emit.Store(spill(base)+8, uint8(enc.Zero), true)
				}
			}
			h = base
			if !store {
				if err := push(class); err != nil {
					return swarSummary{}, err
				}
			}
		case 0x1a:
			if err := flow.prepareDead(swarUnknown); err != nil {
				return swarSummary{}, err
			}
			if h == flow.floor() {
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
				if err := flow.prepareDead(localClasses[index]); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != localClasses[index] {
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
				if privateRefs && typ == 0x70 {
					class = swarFuncRef
				}
				if class == swarInvalid {
					return swarSummary{}, unsupported("SWAR select carrier")
				}
			}
			if !flow.live {
				if op == 0x1b {
					if err := flow.prepareDead(swarUnknown, swarUnknown, swarI32); err != nil {
						return swarSummary{}, err
					}
					class = stackClasses[h-3]
					if class == swarUnknown {
						class = stackClasses[h-2]
					}
				}
				if err := flow.prepareDead(class, class, swarI32); err != nil {
					return swarSummary{}, err
				}
			}
			if h-flow.floor() < 3 || stackClasses[h-1] != swarI32 {
				return swarSummary{}, unsupported("SWAR select condition class/underflow")
			}
			if op == 0x1b {
				class = stackClasses[h-3]
				if class == swarInvalid || class == swarFuncRef {
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
		case 0xd0, 0xd1, 0xd2, 0x25, 0x26:
			if !privateRefs {
				return swarSummary{}, unsupported("SWAR reference instruction without public reference profile")
			}
			if err := reserve(1024); err != nil {
				return swarSummary{}, err
			}
			switch op {
			case 0xd0:
				heap, err := r.S33()
				if err != nil {
					return swarSummary{}, err
				}
				if heap != -16 {
					return swarSummary{}, unsupported("SWAR private nullable abstract funcref only")
				}
				if err := push(swarFuncRef); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.Store(spill(h-1), uint8(enc.Zero), true)
					emit.Store(spill(h-1)+8, uint8(enc.Zero), true)
				}
			case 0xd1:
				if err := flow.prepareDead(swarFuncRef); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != swarFuncRef {
					return swarSummary{}, unsupported("SWAR ref.is_null operand class/underflow")
				}
				if emit != nil {
					emit.Load(5, spill(h-1), true)
					emit.a.Sltiu(enc.X5, enc.X5, 1)
					emit.Store(spill(h-1), 5, true)
					emit.Store(spill(h-1)+8, uint8(enc.Zero), true)
				}
				stackClasses[h-1] = swarI32
			case 0xd2:
				index, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if !descriptorRefs || uint64(index) < uint64(importedFunctions) && !foreignTableCalls || uint64(index) >= uint64(len(functions)) {
					return swarSummary{}, unsupported("SWAR ref.func index/profile")
				}
				usesTraps = true
				if err := push(swarFuncRef); err != nil {
					return swarSummary{}, err
				}
				if emit != nil {
					emit.functionReference(index)
					emit.Store(spill(h-1), 5, true)
					emit.Store(spill(h-1)+8, uint8(enc.Zero), true)
				}
			case 0x25, 0x26:
				index, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if table == nil || index != 0 {
					return swarSummary{}, unsupported("SWAR owned table index")
				}
				arity := 1
				operands := [2]swarClass{swarI32, swarFuncRef}
				if op == 0x26 {
					arity = 2
				}
				if err := flow.prepareDead(operands[:arity]...); err != nil {
					return swarSummary{}, err
				}
				base := h - arity
				if base < flow.floor() || stackClasses[base] != swarI32 || arity == 2 && stackClasses[base+1] != swarFuncRef {
					return swarSummary{}, unsupported("SWAR table operand class/underflow")
				}
				usesTraps = true
				if emit != nil {
					if arity == 2 {
						emit.Load(28, spill(base+1), true)
					}
					emit.Load(5, spill(base), false)
					emit.privateTableEntry()
					if arity == 1 {
						emit.a.Ld(enc.X5, enc.X6, 24)
						emit.Store(spill(base), 5, true)
						emit.Store(spill(base)+8, uint8(enc.Zero), true)
					} else {
						emit.storePrivateReference()
					}
				}
				h = base
				if arity == 1 {
					if err := push(swarFuncRef); err != nil {
						return swarSummary{}, err
					}
				}
			}
		case 0xfc:
			sub, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if sub == 12 || sub == 13 {
				index, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if sub == 12 && table == nil || uint64(index) >= uint64(elementCount) {
					return swarSummary{}, unsupported("SWAR owned element index/table")
				}
				usesTraps = true
				if sub == 13 {
					if err := reserve(512); err != nil {
						return swarSummary{}, err
					}
					if emit != nil {
						emit.privateElementDrop(index)
					}
					continue
				}
				tableIndex, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if tableIndex != 0 {
					return swarSummary{}, unsupported("SWAR table.init table index")
				}
				if err := reserve(1024); err != nil {
					return swarSummary{}, err
				}
				if err := flow.prepareRepeatedDead(swarI32, 3); err != nil {
					return swarSummary{}, err
				}
				base := h - 3
				if base < flow.floor() {
					return swarSummary{}, unsupported("SWAR table.init operand underflow")
				}
				for i := 0; i < 3; i++ {
					if stackClasses[base+i] != swarI32 {
						return swarSummary{}, unsupported("SWAR table.init carrier class")
					}
				}
				flow.usesLoops = true
				if emit != nil {
					emit.Load(6, spill(base), false)
					emit.Load(7, spill(base+1), false)
					emit.Load(5, spill(base+2), false)
					emit.privateTableInit(index)
				}
				h = base
				continue
			}
			if sub >= 14 && sub <= 17 {
				if table == nil || sub == 15 && !borrowedTable && !table.Limits.HasMax && table.Limits.Min > MaxOwnedTableEntries/2 {
					return swarSummary{}, unsupported("SWAR owned table mutation/reserve")
				}
				indices := 1
				if sub == 14 {
					indices = 2
				}
				for i := 0; i < indices; i++ {
					index, err := r.U32()
					if err != nil {
						return swarSummary{}, err
					}
					if index != 0 {
						return swarSummary{}, unsupported("SWAR bulk table index")
					}
				}
				if err := reserve(1024); err != nil {
					return swarSummary{}, err
				}
				var operands []swarClass
				switch sub {
				case 14:
					operands = []swarClass{swarI32, swarI32, swarI32}
				case 15:
					operands = []swarClass{swarFuncRef, swarI32}
				case 17:
					operands = []swarClass{swarI32, swarFuncRef, swarI32}
				}
				if err := flow.prepareDead(operands...); err != nil {
					return swarSummary{}, err
				}
				base := h - len(operands)
				if base < flow.floor() {
					return swarSummary{}, unsupported("SWAR bulk table operand underflow")
				}
				for i, class := range operands {
					if stackClasses[base+i] != class {
						return swarSummary{}, unsupported("SWAR bulk table carrier class")
					}
				}
				usesTraps = true
				flow.usesLoops = flow.usesLoops || sub != 16
				if emit != nil {
					switch sub {
					case 14, 17:
						emit.Load(6, spill(base), false)
						emit.Load(7, spill(base+1), sub == 17)
						emit.Load(5, spill(base+2), false)
						if sub == 14 {
							emit.privateTableCopy()
						} else {
							emit.privateTableFill()
						}
					case 15:
						emit.Load(7, spill(base), true)
						emit.Load(5, spill(base+1), false)
						emit.privateTableGrowAt(spill(base + 1))
					case 16:
						emit.a.Ld(enc.X6, enc.X25, -80)
						emit.trap(trapTableBounds, emit.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
						emit.a.Lwu(enc.X5, enc.X6, 0)
					}
					if sub == 15 || sub == 16 {
						emit.Store(spill(base), 5, true)
						emit.Store(spill(base)+8, uint8(enc.Zero), true)
					}
				}
				h = base
				if sub == 15 || sub == 16 {
					if err := push(swarI32); err != nil {
						return swarSummary{}, err
					}
				}
				continue
			}
			dataOffset := int32(0)
			if sub == 8 || sub == 9 {
				index, err := r.U32()
				if err != nil {
					return swarSummary{}, err
				}
				if uint64(index) >= uint64(dataCount) {
					return swarSummary{}, unsupported("SWAR passive data index")
				}
				dataOffset = int32(index) * 16
				if sub == 9 {
					if err := reserve(512); err != nil {
						return swarSummary{}, err
					}
					if emit != nil {
						emit.dataDrop(dataOffset)
					}
					continue
				}
			}
			if sub == 8 || sub == 10 || sub == 11 {
				if memoryCount == 0 {
					return swarSummary{}, unsupported("SWAR bulk memory without resource")
				}
				indices := 1
				if sub == 10 {
					indices = 2
				}
				memoryIndices := [2]uint32{}
				for i := 0; i < indices; i++ {
					index, err := r.U32()
					if err != nil {
						return swarSummary{}, err
					}
					if uint64(index) >= uint64(memoryCount) || !indexedMemories && index != 0 {
						return swarSummary{}, unsupported("SWAR bulk memory index")
					}
					memoryIndices[i] = index
				}
				if err := reserve(1024); err != nil {
					return swarSummary{}, err
				}
				if err := flow.prepareRepeatedDead(swarI32, 3); err != nil {
					return swarSummary{}, err
				}
				base := h - 3
				if base < flow.floor() {
					return swarSummary{}, unsupported("SWAR bulk memory operand underflow")
				}
				for i := 0; i < 3; i++ {
					if stackClasses[base+i] != swarI32 {
						return swarSummary{}, unsupported("SWAR bulk memory carrier class")
					}
				}
				usesTraps, flow.usesLoops = true, true
				if emit != nil {
					for i := 0; i < 3; i++ {
						emit.Load(uint8(5+i), spill(base+i), false)
					}
					kind := wasm.InstrMemoryFill
					if sub == 10 {
						kind = wasm.InstrMemoryCopy
					} else if sub == 8 {
						kind = wasm.InstrMemoryInit
					}
					emit.memoryBulkAt(kind, dataOffset, memoryIndices[0], memoryIndices[1])
				}
				h = base
				continue
			}
			conversion, ok := swarSaturatingFloatOpcode(sub)
			if !ok {
				return swarSummary{}, unsupported("SWAR non-conversion prefixed scalar lowering is pending")
			}
			if err := reserve(1024); err != nil {
				return swarSummary{}, err
			}
			if err := flow.prepareDead(conversion.input); err != nil {
				return swarSummary{}, err
			}
			if h == flow.floor() || stackClasses[h-1] != conversion.input {
				return swarSummary{}, unsupported("SWAR saturating scalar conversion carrier class/underflow")
			}
			usesFP = true
			if emit != nil {
				emit.Load(5, spill(h-1), !conversion.input.narrow())
				emit.floatTrunc(conversion.input == swarF64, conversion.output == swarI64, sub&1 != 0, true)
				emit.Store(spill(h-1), 5, true)
				emit.Store(spill(h-1)+8, uint8(enc.Zero), true)
			}
			stackClasses[h-1] = conversion.output
		case 0xfd:
			immediateReader := r
			kind, err := r.U32()
			if err != nil {
				return swarSummary{}, err
			}
			if core, ok := swarRelaxedCoreOpcode(kind); ok {
				kind = core
			}
			bound := 512
			_, memoryOp := swarMemoryOpcode(kind)
			if memoryOp || kind == 130 || kind == 186 || kind == 274 || kind == 275 {
				bound = 1024
			} else if _, _, ok := swarRelaxedMaddOpcode(kind); ok {
				bound = 1024
			} else if _, ok := swarFloatConversionOpcode(kind); ok {
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
			if kind == 274 || kind == 275 {
				arity := 2
				if kind == 275 {
					arity = 3
				}
				if err := flow.prepareRepeatedDead(swarV128, arity); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < arity || stackClasses[h-arity] != swarV128 || stackClasses[h-arity+1] != swarV128 || (arity == 3 && stackClasses[h-1] != swarV128) {
					return swarSummary{}, unsupported("SWAR relaxed dot carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-arity), left)
					emit.swarLoad(spill(h-arity+1), right)
					if arity == 3 {
						emit.swarLoad(spill(h-1), swarPair{enc.X12, enc.X13})
					}
					emit.swarRelaxedDot(kind)
					emit.swarStore(spill(h-arity), left)
				}
				h -= arity - 1
				continue
			}
			if width, negative, ok := swarRelaxedMaddOpcode(kind); ok {
				usesFP = true
				if err := flow.prepareDead(swarV128, swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 3 || stackClasses[h-3] != swarV128 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR relaxed multiply-add carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-3), left)
					emit.swarLoad(spill(h-2), right)
					emit.swarLoad(spill(h-1), swarPair{enc.X12, enc.X13})
					emit.swarRelaxedMadd(width, negative)
					emit.swarStore(spill(h-3), left)
				}
				h -= 2
				continue
			}
			if _, ok := swarFloatConversionOpcode(kind); ok {
				usesFP = true
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 1 || stackClasses[h-1] != swarV128 {
					return swarSummary{}, unsupported("SWAR floating conversion carrier class/underflow")
				}
				if emit != nil {
					emit.swarLoad(spill(h-1), left)
					emit.swarFloatConvert(kind)
					emit.swarStore(spill(h-1), left)
				}
				continue
			}
			if width, mode, ok := swarFloatRoundOpcode(kind); ok {
				usesFP = true
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 1 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareRepeatedDead(swarV128, arity); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < arity || stackClasses[h-arity] != swarV128 || (!unary && stackClasses[h-1] != swarV128) {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarV128); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarV128 {
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
				if err := flow.prepareDead(swarV128, swarI32); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < 2 || stackClasses[h-2] != swarV128 || stackClasses[h-1] != swarI32 {
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
				if err := flow.prepareDead(class); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != class {
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
				operands := [2]swarClass{swarV128, class}
				if err := flow.prepareDead(operands[:arity]...); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < arity || stackClasses[h-arity] != swarV128 || (replace && stackClasses[h-1] != class) {
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
				if memoryCount == 0 {
					return swarSummary{}, unsupported("SWAR memory32 descriptor is required")
				}
				var memoryIndex uint32
				var offset uint64
				laneOp := kind >= 84 && kind <= 91
				lane := byte(0)
				if indexedMemories {
					var imm wasm.InstructionImmediate
					if err := wasm.ClassifyInstructionImmediateIntoWithFeatures(&immediateReader, op, &imm, false, true); err != nil {
						return swarSummary{}, err
					}
					memoryIndex, offset = imm.MemIndex, imm.MemOffset
					if uint64(memoryIndex) >= uint64(memoryCount) || offset > uint64(^uint32(0)) || imm.MemAlign > maxAlign {
						return swarSummary{}, unsupported("SWAR indexed memarg")
					}
					r = immediateReader
					if laneOp {
						lane = fn.BodyBytes[r.Offset()-1]
					}
				} else {
					align, err := r.U32()
					if err != nil {
						return swarSummary{}, err
					}
					if align > maxAlign {
						return swarSummary{}, unsupported("SWAR memarg alignment/index")
					}
					off, err := r.U32()
					if err != nil {
						return swarSummary{}, err
					}
					offset = uint64(off)
					if laneOp {
						lane, err = r.Byte()
						if err != nil {
							return swarSummary{}, err
						}
					}
				}
				if laneOp && uint32(lane) >= 16>>maxAlign {
					return swarSummary{}, unsupported("SWAR memory lane index")
				}
				store := kind == 11 || (kind >= 88 && kind <= 91)
				arity := 1
				if store || laneOp {
					arity = 2
				}
				operands := [2]swarClass{swarI32, swarV128}
				if err := flow.prepareDead(operands[:arity]...); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < arity || stackClasses[h-arity] != swarI32 || ((store || laneOp) && stackClasses[h-1] != swarV128) {
					return swarSummary{}, unsupported("SWAR memory carrier class/underflow")
				}
				if emit != nil {
					if store || laneOp {
						emit.swarLoad(spill(h-1), swarPair{enc.X6, enc.X7})
					}
					emit.Load(5, spill(h-arity), false)
					if kind == 0 || kind == 11 {
						emit.swarMemoryAt(memoryIndex, offset, store)
					} else if laneOp {
						emit.swarLaneMemoryAt(memoryIndex, offset, uint8(8<<maxAlign), lane, store)
					} else if kind >= 7 && kind <= 10 {
						emit.swarSplatMemoryAt(memoryIndex, offset, uint8(8<<maxAlign))
					} else {
						emit.swarPartialLoadAt(memoryIndex, offset, kind)
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
				if err := flow.prepareDead(swarV128); err != nil {
					return swarSummary{}, err
				}
				if h == flow.floor() || stackClasses[h-1] != swarV128 {
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
			if err := flow.prepareRepeatedDead(swarV128, arity); err != nil {
				return swarSummary{}, err
			}
			if h-flow.floor() < arity {
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
			if scalar, ok := swarScalarFloatOpcode(op); ok {
				arity := 2
				if scalar.unary {
					arity = 1
				}
				if err := flow.prepareRepeatedDead(scalar.input, arity); err != nil {
					return swarSummary{}, err
				}
				if h-flow.floor() < arity {
					return swarSummary{}, unsupported("SWAR scalar FP underflow")
				}
				for j := h - arity; j < h; j++ {
					if stackClasses[j] != scalar.input {
						return swarSummary{}, unsupported("SWAR scalar FP carrier class")
					}
				}
				usesFP = usesFP || scalar.fp
				usesTraps = usesTraps || scalar.trapping
				if emit != nil {
					emit.Load(5, spill(h-arity), !scalar.input.narrow())
					if !scalar.unary {
						emit.Load(6, spill(h-1), !scalar.input.narrow())
					}
					emit.floatOperation(op)
					if scalar.output.narrow() {
						emit.NormalizeI32(5)
					}
					emit.Store(spill(h-arity), 5, true)
					emit.Store(spill(h-arity)+8, uint8(enc.Zero), true)
				}
				h -= arity - 1
				stackClasses[h-1] = scalar.output
				continue
			}
			operation, wide, unary, resultWide, ok := integerOpcode(op)
			if !ok {
				return swarSummary{}, unsupported("SWAR control/call/scalar lowering is pending")
			}
			arity, class := 2, swarI32
			if unary {
				arity = 1
			}
			if wide {
				class = swarI64
			}
			if err := flow.prepareRepeatedDead(class, arity); err != nil {
				return swarSummary{}, err
			}
			if h-flow.floor() < arity {
				return swarSummary{}, unsupported("SWAR scalar integer underflow")
			}
			for j := h - arity; j < h; j++ {
				if stackClasses[j] != class {
					return swarSummary{}, unsupported("SWAR scalar integer carrier class")
				}
			}
			switch operation {
			case shared.IntDivS, shared.IntDivU, shared.IntRemS, shared.IntRemU:
				usesTraps = true
			}
			if emit != nil {
				emit.Load(5, spill(h-arity), wide)
				if !unary {
					emit.Load(6, spill(h-1), wide)
				}
				emit.operation(operation, wide, unary, resultWide)
				emit.Store(spill(h-arity), 5, true)
				emit.Store(spill(h-arity)+8, uint8(enc.Zero), true)
			}
			h -= arity - 1
			stackClasses[h-1] = swarI32
			if resultWide {
				stackClasses[h-1] = swarI64
			}
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
	return compileSWARPairsWithTypes(fn, ft, memory, nil, mode)
}

// types is the caller's immutable frontend-validated, flattened function-type
// directory. Indexed signatures are never guessed from the current function.
func compileSWARPairsWithTypes(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types []*wasm.CompType, mode emissionMode) ([]byte, FunctionInfo, error) {
	code, info, _, err := compileSWARPairsWithCalls(fn, ft, memory, types, nil, mode)
	return code, info, err
}

// functions contains only the current instance's defined functions. Imports,
// cross-instance entries, executable pointers and guessed signatures are absent.
// All entries must be admitted by the module owner before publishing this code.
func compileSWARPairsWithCalls(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithGlobals(fn, ft, memory, types, functions, nil, mode)
}

// globals is the immutable declared type directory for the owner's aliased
// scalar (8-byte) and vector (16-byte) cells behind JobMemory.GlobalsPtr.
// This private helper does not admit global initializers or module state.
func compileSWARPairsWithGlobals(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithData(fn, ft, memory, types, functions, globals, 0, mode)
}

// dataCount names the immutable directory shape of owned passive descriptors.
func compileSWARPairsWithData(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithTable(fn, ft, memory, types, functions, globals, dataCount, nil, mode)
}

// table is an immutable declaration for a trusted live owned table and canonical
// function descriptors. References remain internal local/control values; public
// signatures, globals, table instantiation and indirect calls remain gated.
func compileSWARPairsWithTable(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithIndirectKeys(fn, ft, memory, types, functions, globals, dataCount, table, nil, mode)
}

// typeKeys must be the immutable checked identities for the exact flattened
// frontend type directory. Neither hashes nor signature provenance are guessed.
func compileSWARPairsWithIndirectKeys(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithElements(fn, ft, memory, types, functions, globals, dataCount, table, typeKeys, 0, mode)
}

// elementCount names immutable original indices of a trusted live owned16-byte
// descriptor directory; payloads are complete same-instance canonical32B rows.
func compileSWARPairsWithElements(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount int, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	return compileSWARPairsWithImports(fn, ft, memory, types, functions, globals, dataCount, table, typeKeys, elementCount, 0, mode)
}

// Like the other private helpers, this requires an already validated immutable
// module profile and live owner-provided dispatch/context directories. It does
// not admit imports in either private module pilot or the public SIMD pipeline.
func compileSWARPairsWithImports(fn *wasm.Func, ft *wasm.CompType, memory *wasm.MemType, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount, importedFunctions int, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	var memories []wasm.MemType
	if memory != nil {
		memories = []wasm.MemType{*memory}
	}
	return compileSWARPairsWithMemories(fn, ft, memories, false, types, functions, globals, dataCount, table, typeKeys, elementCount, importedFunctions, mode)
}
func compileSWARPairsWithMemories(fn *wasm.Func, ft *wasm.CompType, memories []wasm.MemType, indexedMemories bool, types, functions []*wasm.CompType, globals []wasm.GlobalType, dataCount int, table *wasm.TableType, typeKeys []uint64, elementCount, importedFunctions int, mode emissionMode) ([]byte, FunctionInfo, []directCall, error) {
	if len(memories) > MaxProductionMemories || !indexedMemories && len(memories) > 1 || indexedMemories && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR indexed memory profile")
	}

	if importedFunctions != 0 && (!mode.production || !mode.interruptible) {
		return nil, FunctionInfo{}, nil, unsupported("SWAR host calls require production interruption/park bridge")
	}
	if table != nil && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR owned table requires production owner")
	}
	if dataCount != 0 && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR passive data requires production owner")
	}
	if len(globals) != 0 && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR globals require production owner")
	}
	if len(functions) != 0 && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR direct calls require production bridge")
	}
	for _, memory := range memories {
		if !mode.production {
			return nil, FunctionInfo{}, nil, unsupported("SWAR production memory32 resource")
		}
		if err := admitMemory32(memory, true); err != nil {
			return nil, FunctionInfo{}, nil, err
		}
	}
	summary, err := walkSWARPairsWithMemoryMode(fn, ft, len(memories), indexedMemories, types, functions, globals, dataCount, table, typeKeys, elementCount, importedFunctions, mode.foreignTableCalls, mode.borrowedTable, nil)
	if err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	if summary.usesFP && !mode.production {
		// The isolated integer bridge does not preserve accrued FP flags/FCSR.
		return nil, FunctionInfo{}, nil, unsupported("SWAR FP requires production FCSR bridge")
	}
	if summary.usesLoops && (!mode.production || !mode.interruptible) {
		return nil, FunctionInfo{}, nil, unsupported("SWAR loops require production interruption bridge")
	}
	if summary.usesTraps && !mode.production {
		return nil, FunctionInfo{}, nil, unsupported("SWAR traps require production trap bridge")
	}
	f := integerEmitter{target: target{mode: mode}}
	if len(functions) != 0 {
		f.header = callFrameHeader
	}
	actual, err := walkSWARPairsWithMemoryMode(fn, ft, len(memories), indexedMemories, types, functions, globals, dataCount, table, typeKeys, elementCount, importedFunctions, mode.foreignTableCalls, mode.borrowedTable, &f)
	if err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	if actual != summary {
		return nil, FunctionInfo{}, nil, fmt.Errorf("rv64: SWAR admission changed")
	}
	if len(f.traps) != 0 {
		end := f.Jump()
		for _, tr := range f.traps {
			if err := f.Patch(tr.site, f.Position()); err != nil {
				return nil, FunctionInfo{}, nil, err
			}
			f.emitProductionTrap(productionTrapCode(tr.code), tr.sourcePC)
		}
		if err := f.Patch(end, f.Position()); err != nil {
			return nil, FunctionInfo{}, nil, err
		}
	}
	if f.err != nil {
		return nil, FunctionInfo{}, nil, f.err
	}
	if f.a.Err() != nil {
		return nil, FunctionInfo{}, nil, f.a.Err()
	}
	widths := make([]bool, summary.locals*2)
	for i := range widths {
		widths[i] = true
	}
	params, results := swarSignatureSlots(ft.Params), swarSignatureSlots(ft.Results)
	info := FunctionInfo{Params: params, Results: results, SpillSlots: summary.peak * 2, FrameHeaderBytes: f.frameHeader(), DirectCalls: len(f.calls), Fallback: true, mode: mode}
	code, info, err := frameFunctionCalls(f.a.B, widths, params, summary.peak*2, true, -1, info, f.calls)
	if err != nil {
		return nil, FunctionInfo{}, nil, err
	}
	if len(code) > summary.codeBound {
		return nil, FunctionInfo{}, nil, fmt.Errorf("rv64: SWAR code exceeded its admitted bound")
	}
	return code, info, f.calls, nil
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
