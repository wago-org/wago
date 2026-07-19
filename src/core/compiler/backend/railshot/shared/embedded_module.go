package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

type EmbeddedFunctionMetadata struct {
	FuncIndex   uint32
	Offset      uint32
	Size        uint32
	ParamSlots  uint16
	ResultSlots uint16
}

type EmbeddedDataSegment struct {
	Passive bool
	Offset  uint32
	Bytes   []byte
}

type EmbeddedGlobal struct {
	Type    wasm.ValType
	Mutable bool
	Slot    uint32
	Words   [4]uint32
}

type EmbeddedElementSegment struct {
	Offset uint32
	Values []uint32
}

type EmbeddedTable struct {
	Minimum    uint32
	Maximum    uint32
	HasMaximum bool
	Elements   []EmbeddedElementSegment
}

type EmbeddedExport struct {
	Name  string
	Kind  wasm.ExternKind
	Index uint32
}

type EmbeddedModule struct {
	Code              []byte
	Entry             []int
	Functions         []EmbeddedFunctionMetadata
	Data              []EmbeddedDataSegment
	Globals           []EmbeddedGlobal
	Table             *EmbeddedTable
	Exports           []EmbeddedExport
	Start             *uint32
	RequiredCodeBytes uint32
}

// InstantiateData preflights all active segments before mutating local memory,
// then returns index-preserving passive/dropped state for bulk-memory helpers.
func (m *EmbeddedModule) DataSegmentABI(bases []uint32) ([]embedded32.DataSegmentABI, error) {
	if m == nil || len(bases) < len(m.Data) {
		return nil, embedded32.ErrArenaCapacity
	}
	out := make([]embedded32.DataSegmentABI, len(m.Data))
	for i := range m.Data {
		out[i] = embedded32.DataSegmentABI{Base: bases[i], Length: uint32(len(m.Data[i].Bytes))}
		if !m.Data[i].Passive {
			out[i].Dropped = 1
		}
	}
	return out, nil
}

func (m *EmbeddedModule) InstantiateGlobals(cells []uint32) error {
	if m == nil {
		return embedded32.ErrArenaCapacity
	}
	var required uint32
	for i := range m.Globals {
		width, ok := MixedValueSlots(m.Globals[i].Type)
		if !ok || m.Globals[i].Slot > ^uint32(0)-uint32(width) {
			return fmt.Errorf("embedded global %d has invalid layout", i)
		}
		end := m.Globals[i].Slot + uint32(width)
		if end > required {
			required = end
		}
	}
	if uint64(required) > uint64(len(cells)) {
		return embedded32.ErrArenaCapacity
	}
	for i := range m.Globals {
		global := &m.Globals[i]
		width, _ := MixedValueSlots(global.Type)
		copy(cells[global.Slot:global.Slot+uint32(width)], global.Words[:width])
	}
	return nil
}

func (m *EmbeddedModule) InstantiateTable(entries []uint32) error {
	if m == nil || m.Table == nil {
		if len(entries) != 0 {
			clear(entries)
		}
		return nil
	}
	if uint64(m.Table.Minimum) > uint64(len(entries)) {
		return embedded32.ErrArenaCapacity
	}
	for i := range m.Table.Elements {
		segment := &m.Table.Elements[i]
		if uint64(segment.Offset)+uint64(len(segment.Values)) > uint64(m.Table.Minimum) {
			return fmt.Errorf("embedded element segment %d exceeds table minimum", i)
		}
	}
	clear(entries[:m.Table.Minimum])
	for i := range m.Table.Elements {
		segment := &m.Table.Elements[i]
		copy(entries[segment.Offset:], segment.Values)
	}
	return nil
}

func (m *EmbeddedModule) InstantiateData(memory *embedded32.LinearMemory) (*embedded32.DataStore, error) {
	if m == nil || memory == nil {
		return nil, embedded32.ErrInvalidArena
	}
	for i := range m.Data {
		segment := &m.Data[i]
		if !segment.Passive && (uint64(segment.Offset)+uint64(len(segment.Bytes)) > uint64(len(memory.Bytes()))) {
			return nil, fmt.Errorf("embedded32: active data segment %d out of bounds", i)
		}
	}
	inits := make([]embedded32.DataSegmentInit, len(m.Data))
	for i := range m.Data {
		segment := &m.Data[i]
		if !segment.Passive {
			copy(memory.Bytes()[segment.Offset:], segment.Bytes)
		}
		inits[i] = embedded32.DataSegmentInit{Bytes: segment.Bytes, Dropped: !segment.Passive}
	}
	return embedded32.NewDataStore(inits), nil
}

type PublishedEmbeddedModule struct {
	Block     embedded32.CodeBlock
	Entry     []uint32
	Functions []EmbeddedFunctionMetadata
	Data      []EmbeddedDataSegment
	Globals   []EmbeddedGlobal
	Table     *EmbeddedTable
	Exports   []EmbeddedExport
	Start     *uint32
}

func PublishEmbeddedModule(arena *embedded32.CodeArena, module *EmbeddedModule, publish embedded32.CodePublisher) (*PublishedEmbeddedModule, error) {
	if arena == nil || module == nil {
		return nil, embedded32.ErrInvalidArena
	}
	tx, err := arena.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	block, err := tx.Allocate(uint32(len(module.Code)), 16)
	if err != nil {
		return nil, err
	}
	copy(block.Bytes, module.Code)
	if err := tx.Commit(publish); err != nil {
		return nil, err
	}
	out := &PublishedEmbeddedModule{Block: block, Entry: make([]uint32, len(module.Entry)), Functions: make([]EmbeddedFunctionMetadata, len(module.Functions)), Data: module.Data, Globals: module.Globals, Table: module.Table, Exports: module.Exports, Start: module.Start}
	for i, entry := range module.Entry {
		out.Entry[i] = block.Offset + uint32(entry)
	}
	for i, meta := range module.Functions {
		meta.Offset += block.Offset
		out.Functions[i] = meta
	}
	return out, nil
}

type EmbeddedModuleOptions struct {
	CodeCapacity    uint32
	EnforceCapacity bool
}

type EmbeddedFunctionCompiler func(funcIdx int, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error)

// CompileEmbeddedModule validates and lays out a module while delegating exact
// homogeneous or mixed-width function admission to the target compiler.
func CompileEmbeddedModule(m *wasm.Module, opts EmbeddedModuleOptions, target string, expansion int, alignmentPad []byte, compile EmbeddedFunctionCompiler) (*EmbeddedModule, error) {
	if m == nil {
		return nil, fmt.Errorf("%s: nil module", target)
	}
	if compile == nil || len(alignmentPad) == 0 || 16%len(alignmentPad) != 0 {
		return nil, fmt.Errorf("%s: invalid module compiler configuration", target)
	}
	if err := wasm.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("%s: module validation: %w", target, err)
	}
	if len(m.Imports) != 0 {
		return nil, fmt.Errorf("%s: module imports are not supported", target)
	}
	if len(m.Tables) > 1 || len(m.Memories) > 1 || len(m.Tags) != 0 || len(m.StringRefs) != 0 {
		return nil, fmt.Errorf("%s: module contains unsupported runtime state", target)
	}
	if len(m.Memories) == 1 && (m.Memories[0].Limits.Addr64 || m.Memories[0].Shared) {
		return nil, fmt.Errorf("%s: target requires unshared memory32", target)
	}
	if len(m.FuncTypes) != len(m.Code) {
		return nil, fmt.Errorf("%s: function/code count mismatch", target)
	}

	totalBody := 0
	bodies := make([][]byte, len(m.Code))
	types := make([]*wasm.CompType, len(m.Code))
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok || ft.Kind != wasm.CompFunc {
			return nil, fmt.Errorf("%s: function %d has no function type", target, i)
		}
		body := appendULEB32(nil, uint32(len(m.Code[i].Locals.Runs)))
		for _, run := range m.Code[i].Locals.Runs {
			encoded, ok := wasm.EncodeValType(run.Type)
			if !ok {
				return nil, fmt.Errorf("%s: function %d local type %s has no embedded encoding", target, i, run.Type)
			}
			body = appendULEB32(body, run.Count)
			body = append(body, encoded)
		}
		if len(m.Code[i].BodyBytes) == 0 {
			return nil, fmt.Errorf("%s: function %d has no byte-backed body", target, i)
		}
		body = append(body, m.Code[i].BodyBytes...)
		bodies[i], types[i] = body, ft
		totalBody += len(body)
	}

	required := ModuleCodeCapacity(totalBody, len(bodies), expansion)
	if required == 0 || uint64(required) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("%s: module code capacity overflow", target)
	}
	bounded := opts.EnforceCapacity || opts.CodeCapacity != 0
	if bounded && uint32(required) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: code arena capacity %d is below preflight requirement %d", target, opts.CodeCapacity, required)
	}
	data, err := embeddedDataSegments(m, target)
	if err != nil {
		return nil, err
	}
	globals, err := embeddedGlobals(m, target)
	if err != nil {
		return nil, err
	}
	table, err := embeddedTable(m, target)
	if err != nil {
		return nil, err
	}
	exports := make([]EmbeddedExport, len(m.Exports))
	for i := range m.Exports {
		exports[i] = EmbeddedExport{Name: m.Exports[i].Name, Kind: m.Exports[i].Index.Kind, Index: m.Exports[i].Index.Index}
	}
	var start *uint32
	if m.Start != nil {
		index := uint32(*m.Start)
		if int(index) >= len(bodies) {
			return nil, fmt.Errorf("%s: start function %d is not local", target, index)
		}
		start = &index
	}
	out := &EmbeddedModule{Code: make([]byte, 0, required), Entry: make([]int, len(bodies)), Functions: make([]EmbeddedFunctionMetadata, len(bodies)), Data: data, Globals: globals, Table: table, Exports: exports, Start: start, RequiredCodeBytes: uint32(required)}
	for i, body := range bodies {
		pad := (16 - len(out.Code)%16) % 16
		if pad%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d has incompatible code alignment", target, i)
		}
		for j := 0; j < pad; j += len(alignmentPad) {
			out.Code = append(out.Code, alignmentPad...)
		}
		entry := len(out.Code)
		fnCode, err := compile(i, types[i], m.Code[i].Locals.Runs, body)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d: %w", target, i, err)
		}
		if len(fnCode)%len(alignmentPad) != 0 {
			return nil, fmt.Errorf("%s: function %d emitted misaligned code size %d", target, i, len(fnCode))
		}
		params, err := serializedSlots(types[i].Params)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d parameters: %w", target, i, err)
		}
		results, err := serializedSlots(types[i].Results)
		if err != nil {
			return nil, fmt.Errorf("%s: function %d results: %w", target, i, err)
		}
		out.Entry[i] = entry
		out.Code = append(out.Code, fnCode...)
		out.Functions[i] = EmbeddedFunctionMetadata{FuncIndex: uint32(i), Offset: uint32(entry), Size: uint32(len(fnCode)), ParamSlots: uint16(params), ResultSlots: uint16(results)}
	}
	if bounded && uint32(len(out.Code)) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: compiled code size %d exceeds arena capacity %d", target, len(out.Code), opts.CodeCapacity)
	}
	return out, nil
}

// CompileEmbeddedI32Module preserves the original strict i32 entry point for
// tests and callers which intentionally admit only the initial scalar subset.
func CompileEmbeddedI32Module(m *wasm.Module, opts EmbeddedModuleOptions, target string, maxParams, expansion int, alignmentPad []byte, compile func(int, []byte) ([]byte, error)) (*EmbeddedModule, error) {
	return CompileEmbeddedModule(m, opts, target, expansion, alignmentPad, func(_ int, ft *wasm.CompType, locals []wasm.LocalRun, body []byte) ([]byte, error) {
		if len(ft.Params) > maxParams {
			return nil, fmt.Errorf("has %d parameters, maximum is %d", len(ft.Params), maxParams)
		}
		for _, typ := range ft.Params {
			if typ != wasm.I32 {
				return nil, fmt.Errorf("parameter type %s is not yet supported", typ)
			}
		}
		if len(ft.Results) > 1 || len(ft.Results) == 1 && ft.Results[0] != wasm.I32 {
			return nil, fmt.Errorf("result signature is not yet supported")
		}
		for _, run := range locals {
			if run.Type != wasm.I32 {
				return nil, fmt.Errorf("local type %s is not yet supported", run.Type)
			}
		}
		return compile(len(ft.Params), body)
	})
}

func embeddedTable(m *wasm.Module, target string) (*EmbeddedTable, error) {
	if len(m.Tables) == 0 {
		if len(m.Elements) != 0 {
			return nil, fmt.Errorf("%s: element segments require a table", target)
		}
		return nil, nil
	}
	table := &m.Tables[0]
	if table.Type.Ref != wasm.FuncRef.Ref {
		return nil, fmt.Errorf("%s: table type %s is not supported", target, table.Type.Ref)
	}
	if table.Type.Limits.Addr64 || table.Type.Limits.Min > uint64(^uint32(0)) {
		return nil, fmt.Errorf("%s: table limits exceed the 32-bit target", target)
	}
	if table.Init != nil {
		return nil, fmt.Errorf("%s: table initializer expressions are not supported", target)
	}
	out := &EmbeddedTable{Minimum: uint32(table.Type.Limits.Min)}
	if table.Type.Limits.Max != nil {
		if *table.Type.Limits.Max > uint64(^uint32(0)) {
			return nil, fmt.Errorf("%s: table maximum exceeds the 32-bit target", target)
		}
		out.Maximum, out.HasMaximum = uint32(*table.Type.Limits.Max), true
	}
	out.Elements = make([]EmbeddedElementSegment, 0, len(m.Elements))
	for i := range m.Elements {
		elem := &m.Elements[i]
		if elem.Mode.Kind != wasm.ElemActive || elem.Mode.Table != 0 {
			return nil, fmt.Errorf("%s: element segment %d must be active for table zero", target, i)
		}
		offset, err := embeddedI32Const(elem.Mode.Offset)
		if err != nil {
			return nil, fmt.Errorf("%s: element segment %d offset: %w", target, i, err)
		}
		values := make([]uint32, 0)
		switch elem.Kind.Kind {
		case wasm.ElemFuncs:
			values = make([]uint32, len(elem.Kind.Funcs))
			for j, index := range elem.Kind.Funcs {
				if uint64(index) >= uint64(len(m.Code)) || uint32(index) == ^uint32(0) {
					return nil, fmt.Errorf("%s: element segment %d function %d is not local", target, i, index)
				}
				values[j] = uint32(index) + 1
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			if elem.Kind.Ref != wasm.FuncRef.Ref {
				return nil, fmt.Errorf("%s: element segment %d reference type %s is not supported", target, i, elem.Kind.Ref)
			}
			values = make([]uint32, len(elem.Kind.Exprs))
			for j := range elem.Kind.Exprs {
				value, err := embeddedFuncRefConst(elem.Kind.Exprs[j], len(m.Code))
				if err != nil {
					return nil, fmt.Errorf("%s: element segment %d value %d: %w", target, i, j, err)
				}
				values[j] = value
			}
		default:
			return nil, fmt.Errorf("%s: element segment %d kind is not supported", target, i)
		}
		if uint64(offset)+uint64(len(values)) > uint64(out.Minimum) {
			return nil, fmt.Errorf("%s: element segment %d exceeds table minimum", target, i)
		}
		out.Elements = append(out.Elements, EmbeddedElementSegment{Offset: offset, Values: values})
	}
	return out, nil
}

func embeddedFuncRefConst(expr wasm.Expr, functionCount int) (uint32, error) {
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err != nil {
			return 0, err
		}
		var value uint32
		switch op {
		case 0xd0:
			heap, err := r.S33()
			if err != nil || heap != -16 {
				return 0, fmt.Errorf("expected ref.null func")
			}
		case 0xd2:
			index, err := r.U32()
			if err != nil || uint64(index) >= uint64(functionCount) || index == ^uint32(0) {
				return 0, fmt.Errorf("ref.func index %d is not local", index)
			}
			value = index + 1
		default:
			return 0, fmt.Errorf("expected ref.null or ref.func")
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.HasNext() {
			return 0, fmt.Errorf("malformed reference const expression")
		}
		return value, nil
	}
	if len(expr.Instrs) != 1 {
		return 0, fmt.Errorf("unsupported reference const expression")
	}
	in := expr.Instrs[0]
	switch in.Kind {
	case wasm.InstrRefNull:
		if in.HeapType() != wasm.AbsHeap(wasm.HeapFunc) {
			return 0, fmt.Errorf("expected ref.null func")
		}
		return 0, nil
	case wasm.InstrRefFunc:
		if uint64(in.Index) >= uint64(functionCount) || in.Index == ^uint32(0) {
			return 0, fmt.Errorf("ref.func index %d is not local", in.Index)
		}
		return in.Index + 1, nil
	default:
		return 0, fmt.Errorf("expected ref.null or ref.func")
	}
}

func EmbeddedGlobalSlot(m *wasm.Module, index uint32) (uint32, bool) {
	if m == nil || uint64(index) >= uint64(len(m.Globals)) {
		return 0, false
	}
	var slot uint32
	for i := uint32(0); i < index; i++ {
		width, ok := MixedValueSlots(m.Globals[i].Type.Type)
		if !ok || slot > ^uint32(0)-uint32(width) {
			return 0, false
		}
		slot += uint32(width)
	}
	return slot, true
}

func embeddedGlobals(m *wasm.Module, target string) ([]EmbeddedGlobal, error) {
	out := make([]EmbeddedGlobal, len(m.Globals))
	for i := range m.Globals {
		global := &m.Globals[i]
		slot, ok := EmbeddedGlobalSlot(m, uint32(i))
		if !ok {
			return nil, fmt.Errorf("%s: global %d has unsupported type or layout", target, i)
		}
		words, err := embeddedConstWords(global.Init, global.Type.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: global %d initializer: %w", target, i, err)
		}
		out[i] = EmbeddedGlobal{Type: global.Type.Type, Mutable: global.Type.Mutable, Slot: slot, Words: words}
	}
	return out, nil
}

func embeddedConstWords(expr wasm.Expr, typ wasm.ValType) ([4]uint32, error) {
	var words [4]uint32
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err != nil {
			return words, err
		}
		switch typ {
		case wasm.I32:
			if op != 0x41 {
				return words, fmt.Errorf("expected i32.const")
			}
			value, err := r.I32()
			if err != nil {
				return words, err
			}
			words[0] = uint32(value)
		case wasm.I64:
			if op != 0x42 {
				return words, fmt.Errorf("expected i64.const")
			}
			value, err := r.I64()
			if err != nil {
				return words, err
			}
			words[0], words[1] = uint32(value), uint32(uint64(value)>>32)
		case wasm.F32:
			if op != 0x43 {
				return words, fmt.Errorf("expected f32.const")
			}
			bits, err := r.Bytes(4)
			if err != nil {
				return words, err
			}
			words[0] = binary.LittleEndian.Uint32(bits)
		case wasm.F64:
			if op != 0x44 {
				return words, fmt.Errorf("expected f64.const")
			}
			bits, err := r.Bytes(8)
			if err != nil {
				return words, err
			}
			value := binary.LittleEndian.Uint64(bits)
			words[0], words[1] = uint32(value), uint32(value>>32)
		case wasm.V128:
			if op != 0xfd {
				return words, fmt.Errorf("expected v128.const")
			}
			subop, err := r.U32()
			if err != nil || subop != 0x0c {
				return words, fmt.Errorf("expected v128.const")
			}
			bits, err := r.Bytes(16)
			if err != nil {
				return words, err
			}
			for i := range words {
				words[i] = binary.LittleEndian.Uint32(bits[i*4:])
			}
		default:
			return words, fmt.Errorf("global type %s is not supported", typ)
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.HasNext() {
			return words, fmt.Errorf("malformed const expression")
		}
		return words, nil
	}
	if len(expr.Instrs) != 1 {
		return words, fmt.Errorf("unsupported const expression")
	}
	in := expr.Instrs[0]
	switch typ {
	case wasm.I32:
		if in.Kind != wasm.InstrI32Const {
			return words, fmt.Errorf("expected i32.const")
		}
		words[0] = uint32(in.I32)
	case wasm.I64:
		if in.Kind != wasm.InstrI64Const {
			return words, fmt.Errorf("expected i64.const")
		}
		words[0], words[1] = uint32(in.I64), uint32(uint64(in.I64)>>32)
	case wasm.F32:
		if in.Kind != wasm.InstrF32Const {
			return words, fmt.Errorf("expected f32.const")
		}
		words[0] = in.F32Bits
	case wasm.F64:
		if in.Kind != wasm.InstrF64Const {
			return words, fmt.Errorf("expected f64.const")
		}
		words[0], words[1] = uint32(in.F64Bits), uint32(in.F64Bits>>32)
	case wasm.V128:
		if in.Kind != wasm.InstrV128Const || len(in.Bytes()) != 16 {
			return words, fmt.Errorf("expected v128.const")
		}
		for i := range words {
			words[i] = binary.LittleEndian.Uint32(in.Bytes()[i*4:])
		}
	default:
		return words, fmt.Errorf("global type %s is not supported", typ)
	}
	return words, nil
}

func embeddedDataSegments(m *wasm.Module, target string) ([]EmbeddedDataSegment, error) {
	out := make([]EmbeddedDataSegment, len(m.Data))
	for i := range m.Data {
		data := &m.Data[i]
		if uint64(len(data.Init)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("%s: data segment %d exceeds target address space", target, i)
		}
		out[i].Bytes = data.Init
		if data.Mode.Kind == wasm.DataPassive {
			out[i].Passive = true
			continue
		}
		if data.Mode.Mem != 0 {
			return nil, fmt.Errorf("%s: data segment %d targets unsupported memory", target, i)
		}
		offset, err := embeddedI32Const(data.Mode.Offset)
		if err != nil {
			return nil, fmt.Errorf("%s: data segment %d offset: %w", target, i, err)
		}
		out[i].Offset = offset
	}
	return out, nil
}

func embeddedI32Const(expr wasm.Expr) (uint32, error) {
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err != nil || op != 0x41 {
			return 0, fmt.Errorf("expected i32.const")
		}
		value, err := r.I32()
		if err != nil {
			return 0, err
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.HasNext() {
			return 0, fmt.Errorf("malformed const expression")
		}
		return uint32(value), nil
	}
	if len(expr.Instrs) == 1 && expr.Instrs[0].Kind == wasm.InstrI32Const {
		return uint32(expr.Instrs[0].I32), nil
	}
	return 0, fmt.Errorf("unsupported const expression")
}

func serializedSlots(types []wasm.ValType) (int, error) {
	n := 0
	for _, typ := range types {
		switch typ {
		case wasm.I32, wasm.F32:
			n++
		case wasm.I64, wasm.F64:
			n += 2
		case wasm.V128:
			n += 4
		default:
			if typ.Kind == wasm.ValRef {
				n++
			} else {
				return 0, fmt.Errorf("unsupported value type %s", typ)
			}
		}
		if n > int(^uint16(0)) {
			return 0, fmt.Errorf("serialized slot count overflow")
		}
	}
	return n, nil
}

func appendULEB32(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}
