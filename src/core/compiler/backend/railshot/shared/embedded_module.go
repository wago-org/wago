package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

type EmbeddedImport struct {
	Module         string
	Name           string
	Kind           wasm.ExternKind
	Index          uint32
	Params         []wasm.ValType
	Results        []wasm.ValType
	FunctionTypeID uint32
	Type           wasm.ValType
	Mutable        bool
	Reference      wasm.RefType
	Minimum        uint32
	Maximum        uint32
	HasMaximum     bool
}

type EmbeddedFunctionSignature struct {
	Params  []wasm.ValType
	Results []wasm.ValType
	TypeID  uint32
}

type EmbeddedFunctionMetadata struct {
	FuncIndex    uint32
	Offset       uint32
	Size         uint32
	ParamSlots   uint16
	ResultSlots  uint16
	CallOffset   uint32
	HasCallEntry bool
}

type EmbeddedDataSegment struct {
	Passive bool
	Offset  uint32
	Bytes   []byte
}

type EmbeddedMemory struct {
	Imported   bool
	Minimum    uint32
	Maximum    uint32
	HasMaximum bool
}

type EmbeddedGlobal struct {
	Type          wasm.ValType
	Mutable       bool
	Slot          uint32
	Words         [4]uint32
	HasInitGlobal bool
	InitGlobal    uint32
}

type EmbeddedElementMode uint8

const (
	EmbeddedElementActive EmbeddedElementMode = iota
	EmbeddedElementPassive
	EmbeddedElementDeclarative
)

type EmbeddedElementSegment struct {
	Mode      EmbeddedElementMode
	Reference wasm.RefType
	Offset    uint32
	Values    []uint32
}

type EmbeddedTable struct {
	Imported   bool
	Reference  wasm.RefType
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
	Code               []byte
	Entry              []int
	Functions          []EmbeddedFunctionMetadata
	FunctionTypeIDs    []uint32
	FunctionSignatures []EmbeddedFunctionSignature
	ImportedFunctions  uint32
	Imports            []EmbeddedImport
	MemoryImported     bool
	Memory             *EmbeddedMemory
	Data               []EmbeddedDataSegment
	ImportedGlobals    []EmbeddedGlobal
	Globals            []EmbeddedGlobal
	Table              *EmbeddedTable
	Exports            []EmbeddedExport
	Start              *uint32
	StartEntry         *int
	RequiredCodeBytes  uint32
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
	return m.InstantiateGlobalsWithImports(cells, nil)
}

func (m *EmbeddedModule) InstantiateGlobalsWithImports(cells []uint32, importedCells [][]uint32) error {
	if m == nil {
		return embedded32.ErrArenaCapacity
	}
	var required uint32
	for i := range m.Globals {
		global := &m.Globals[i]
		width, ok := MixedValueSlots(global.Type)
		if !ok || global.Slot > ^uint32(0)-uint32(width) {
			return fmt.Errorf("embedded global %d has invalid layout", i)
		}
		end := global.Slot + uint32(width)
		if end > required {
			required = end
		}
		if global.HasInitGlobal {
			if uint64(global.InitGlobal) >= uint64(len(importedCells)) || len(importedCells[global.InitGlobal]) < int(width) {
				return fmt.Errorf("embedded global %d imported initializer %d is unavailable", i, global.InitGlobal)
			}
		}
	}
	if uint64(required) > uint64(len(cells)) {
		return embedded32.ErrArenaCapacity
	}
	for i := range m.Globals {
		global := &m.Globals[i]
		width, _ := MixedValueSlots(global.Type)
		if global.HasInitGlobal {
			copy(cells[global.Slot:global.Slot+uint32(width)], importedCells[global.InitGlobal][:width])
		} else {
			copy(cells[global.Slot:global.Slot+uint32(width)], global.Words[:width])
		}
	}
	return nil
}

func (m *EmbeddedModule) ElementSegmentABI(bases []uint32) ([]embedded32.DataSegmentABI, error) {
	if m == nil || m.Table == nil {
		if len(bases) != 0 {
			return nil, embedded32.ErrArenaCapacity
		}
		return nil, nil
	}
	if len(bases) < len(m.Table.Elements) {
		return nil, embedded32.ErrArenaCapacity
	}
	out := make([]embedded32.DataSegmentABI, len(m.Table.Elements))
	for i := range m.Table.Elements {
		segment := &m.Table.Elements[i]
		out[i] = embedded32.DataSegmentABI{Base: bases[i], Length: uint32(len(segment.Values))}
		if segment.Mode != EmbeddedElementPassive {
			out[i].Dropped = 1
		}
	}
	return out, nil
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
	activeLimit := uint64(m.Table.Minimum)
	if m.Table.Imported {
		activeLimit = uint64(len(entries))
	}
	for i := range m.Table.Elements {
		segment := &m.Table.Elements[i]
		if segment.Mode == EmbeddedElementActive && uint64(segment.Offset)+uint64(len(segment.Values)) > activeLimit {
			return fmt.Errorf("embedded element segment %d exceeds table length", i)
		}
	}
	if !m.Table.Imported {
		clear(entries[:m.Table.Minimum])
	}
	for i := range m.Table.Elements {
		segment := &m.Table.Elements[i]
		if segment.Mode == EmbeddedElementActive {
			copy(entries[segment.Offset:], segment.Values)
		}
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
	Block              embedded32.CodeBlock
	Entry              []uint32
	Functions          []EmbeddedFunctionMetadata
	FunctionTypeIDs    []uint32
	FunctionSignatures []EmbeddedFunctionSignature
	ImportedFunctions  uint32
	Imports            []EmbeddedImport
	MemoryImported     bool
	Memory             *EmbeddedMemory
	Data               []EmbeddedDataSegment
	ImportedGlobals    []EmbeddedGlobal
	Globals            []EmbeddedGlobal
	Table              *EmbeddedTable
	Exports            []EmbeddedExport
	Start              *uint32
	StartEntry         *uint32
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
	out := &PublishedEmbeddedModule{Block: block, Entry: make([]uint32, len(module.Entry)), Functions: make([]EmbeddedFunctionMetadata, len(module.Functions)), FunctionTypeIDs: append([]uint32(nil), module.FunctionTypeIDs...), FunctionSignatures: module.FunctionSignatures, ImportedFunctions: module.ImportedFunctions, Imports: module.Imports, MemoryImported: module.MemoryImported, Memory: module.Memory, Data: module.Data, ImportedGlobals: module.ImportedGlobals, Globals: module.Globals, Table: module.Table, Exports: module.Exports, Start: module.Start}
	if module.StartEntry != nil {
		entry := block.Offset + uint32(*module.StartEntry)
		out.StartEntry = &entry
	}
	for i, entry := range module.Entry {
		out.Entry[i] = block.Offset + uint32(entry)
	}
	for i, meta := range module.Functions {
		meta.Offset += block.Offset
		if meta.HasCallEntry {
			meta.CallOffset += block.Offset
		}
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
	importedFunctions := uint32(0)
	importedTables, importedMemories := 0, 0
	var importedMemory *wasm.MemType
	var importedGlobals []EmbeddedGlobal
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternFunc:
			importedFunctions++
		case wasm.ExternTable:
			importedTables++
		case wasm.ExternMem:
			importedMemories++
			memory := m.Imports[i].Type.Mem
			importedMemory = &memory
		case wasm.ExternGlobal:
			global := m.Imports[i].Type.Global
			if _, ok := MixedValueSlots(global.Type); !ok {
				return nil, fmt.Errorf("%s: imported global %d type %s is not supported", target, len(importedGlobals), global.Type)
			}
			importedGlobals = append(importedGlobals, EmbeddedGlobal{Type: global.Type, Mutable: global.Mutable, Slot: uint32(len(importedGlobals))})
		default:
			return nil, fmt.Errorf("%s: import %d kind %d is not supported", target, i, m.Imports[i].Type.Kind)
		}
	}
	if importedTables+len(m.Tables) > 1 || importedMemories+len(m.Memories) > 1 || len(m.Tags) != 0 || len(m.StringRefs) != 0 {
		return nil, fmt.Errorf("%s: module contains unsupported runtime state", target)
	}
	imports, err := embeddedImports(m, target)
	if err != nil {
		return nil, err
	}
	if importedMemory != nil && (importedMemory.Limits.Addr64 || importedMemory.Shared) {
		return nil, fmt.Errorf("%s: target requires unshared imported memory32", target)
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
	memory, err := embeddedMemory(m, target)
	if err != nil {
		return nil, err
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
	functionTypeIDs, err := embeddedFunctionTypeIDs(m)
	if err != nil {
		return nil, fmt.Errorf("%s: function type IDs: %w", target, err)
	}
	functionSignatures, err := embeddedFunctionSignatures(m, functionTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("%s: function signatures: %w", target, err)
	}
	exports := make([]EmbeddedExport, len(m.Exports))
	for i := range m.Exports {
		exports[i] = EmbeddedExport{Name: m.Exports[i].Name, Kind: m.Exports[i].Index.Kind, Index: m.Exports[i].Index.Index}
	}
	var start *uint32
	if m.Start != nil {
		index := uint32(*m.Start)
		if index < importedFunctions || uint64(index-importedFunctions) >= uint64(len(bodies)) {
			return nil, fmt.Errorf("%s: start function %d is not local", target, index)
		}
		start = &index
	}
	out := &EmbeddedModule{Code: make([]byte, 0, required), Entry: make([]int, len(bodies)), Functions: make([]EmbeddedFunctionMetadata, len(bodies)), FunctionTypeIDs: functionTypeIDs, FunctionSignatures: functionSignatures, ImportedFunctions: importedFunctions, Imports: imports, MemoryImported: importedMemories == 1, Memory: memory, Data: data, ImportedGlobals: importedGlobals, Globals: globals, Table: table, Exports: exports, Start: start, RequiredCodeBytes: uint32(required)}
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
		out.Functions[i] = EmbeddedFunctionMetadata{FuncIndex: importedFunctions + uint32(i), Offset: uint32(entry), Size: uint32(len(fnCode)), ParamSlots: uint16(params), ResultSlots: uint16(results)}
	}
	if bounded && uint32(len(out.Code)) > opts.CodeCapacity {
		return nil, fmt.Errorf("%s: compiled code size %d exceeds arena capacity %d", target, len(out.Code), opts.CodeCapacity)
	}
	return out, nil
}

// CompileEmbeddedI32Module preserves the original strict i32 entry point for
// tests and callers which intentionally admit only the initial scalar subset.
func CompileEmbeddedI32Module(m *wasm.Module, opts EmbeddedModuleOptions, target string, maxParams, expansion int, alignmentPad []byte, compile func(int, []byte) ([]byte, error)) (*EmbeddedModule, error) {
	if m != nil {
		for i := range m.Imports {
			if m.Imports[i].Type.Kind == wasm.ExternTable {
				if typ, ok := embeddedReferenceValueType(m.Imports[i].Type.Table.Ref); !ok || typ != wasm.FuncRef {
					return nil, fmt.Errorf("%s: table type %s is not supported", target, m.Imports[i].Type.Table.Ref)
				}
			}
		}
		for i := range m.Tables {
			if typ, ok := embeddedReferenceValueType(m.Tables[i].Type.Ref); !ok || typ != wasm.FuncRef {
				return nil, fmt.Errorf("%s: table type %s is not supported", target, m.Tables[i].Type.Ref)
			}
		}
	}
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

func embeddedFunctionTypeKey(ft *wasm.CompType) (string, error) {
	if ft == nil || ft.Kind != wasm.CompFunc {
		return "", fmt.Errorf("type is not a function")
	}
	key := appendULEB32(nil, uint32(len(ft.Params)))
	for _, typ := range ft.Params {
		if _, ok := MixedValueSlots(typ); !ok {
			return "", fmt.Errorf("parameter type %s is not supported", typ)
		}
		encoded, ok := wasm.EncodeValType(typ)
		if !ok {
			return "", fmt.Errorf("parameter type %s has no embedded encoding", typ)
		}
		key = append(key, encoded)
	}
	key = appendULEB32(key, uint32(len(ft.Results)))
	for _, typ := range ft.Results {
		if _, ok := MixedValueSlots(typ); !ok {
			return "", fmt.Errorf("result type %s is not supported", typ)
		}
		encoded, ok := wasm.EncodeValType(typ)
		if !ok {
			return "", fmt.Errorf("result type %s has no embedded encoding", typ)
		}
		key = append(key, encoded)
	}
	return string(key), nil
}

func embeddedFunctionTypeMap(m *wasm.Module) (map[uint32]uint32, error) {
	ids := make(map[uint32]uint32)
	seen := make(map[uint32]string)
	var index uint32
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			comp := &m.Types[i].SubTypes[j].Comp
			if comp.Kind == wasm.CompFunc {
				key, err := embeddedFunctionTypeKey(comp)
				if err != nil {
					return nil, fmt.Errorf("type %d: %w", index, err)
				}
				id := wasm.StructuralFuncTypeID(comp)
				if previous, ok := seen[id]; ok && previous != key {
					return nil, fmt.Errorf("type %d structural id collision", index)
				}
				seen[id] = key
				ids[index] = id
			}
			index++
		}
	}
	return ids, nil
}

func EmbeddedFunctionTypeID(m *wasm.Module, index uint32) (uint32, bool) {
	if m == nil {
		return 0, false
	}
	ids, err := embeddedFunctionTypeMap(m)
	if err != nil {
		return 0, false
	}
	id, ok := ids[index]
	return id, ok
}

func embeddedImports(m *wasm.Module, target string) ([]EmbeddedImport, error) {
	out := make([]EmbeddedImport, len(m.Imports))
	var functionIndex, tableIndex, memoryIndex, globalIndex uint32
	for i := range m.Imports {
		in := &m.Imports[i]
		entry := EmbeddedImport{Module: in.Module, Name: in.Name, Kind: in.Type.Kind}
		switch in.Type.Kind {
		case wasm.ExternFunc:
			ft, ok := m.FuncSignature(functionIndex)
			if !ok || ft.Kind != wasm.CompFunc {
				return nil, fmt.Errorf("%s: imported function %d has no function type", target, functionIndex)
			}
			entry.Index = functionIndex
			entry.Params = append([]wasm.ValType(nil), ft.Params...)
			entry.Results = append([]wasm.ValType(nil), ft.Results...)
			entry.FunctionTypeID = wasm.StructuralFuncTypeID(ft)
			functionIndex++
		case wasm.ExternTable:
			entry.Index = tableIndex
			entry.Reference = in.Type.Table.Ref
			entry.Minimum = uint32(in.Type.Table.Limits.Min)
			if in.Type.Table.Limits.Max != nil {
				entry.Maximum, entry.HasMaximum = uint32(*in.Type.Table.Limits.Max), true
			}
			tableIndex++
		case wasm.ExternMem:
			entry.Index = memoryIndex
			entry.Minimum = uint32(in.Type.Mem.Limits.Min)
			if in.Type.Mem.Limits.Max != nil {
				entry.Maximum, entry.HasMaximum = uint32(*in.Type.Mem.Limits.Max), true
			}
			memoryIndex++
		case wasm.ExternGlobal:
			entry.Index = globalIndex
			entry.Type = in.Type.Global.Type
			entry.Mutable = in.Type.Global.Mutable
			globalIndex++
		default:
			return nil, fmt.Errorf("%s: import %d kind %d is not supported", target, i, in.Type.Kind)
		}
		out[i] = entry
	}
	return out, nil
}

func embeddedFunctionTypeIDs(m *wasm.Module) ([]uint32, error) {
	ids, err := embeddedFunctionTypeMap(m)
	if err != nil {
		return nil, err
	}
	imported := m.ImportedFuncCount()
	out := make([]uint32, imported+len(m.FuncTypes))
	function := 0
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternFunc {
			continue
		}
		index := m.Imports[i].Type.Type
		if index.Rec {
			return nil, fmt.Errorf("imported function %d uses a recursion-local type index", function)
		}
		id, ok := ids[index.Index]
		if !ok {
			return nil, fmt.Errorf("imported function %d type %d is not an embedded function type", function, index.Index)
		}
		out[function] = id
		function++
	}
	for i, index := range m.FuncTypes {
		if index.Rec {
			return nil, fmt.Errorf("function %d uses a recursion-local type index", i)
		}
		id, ok := ids[index.Index]
		if !ok {
			return nil, fmt.Errorf("function %d type %d is not an embedded function type", i, index.Index)
		}
		out[imported+i] = id
	}
	return out, nil
}

func embeddedFunctionSignatures(m *wasm.Module, ids []uint32) ([]EmbeddedFunctionSignature, error) {
	if m == nil || len(ids) != m.FuncCount() {
		return nil, fmt.Errorf("function signature/type id count mismatch")
	}
	out := make([]EmbeddedFunctionSignature, len(ids))
	for i := range out {
		ft, ok := m.FuncSignature(uint32(i))
		if !ok || ft.Kind != wasm.CompFunc {
			return nil, fmt.Errorf("function %d has no function signature", i)
		}
		out[i] = EmbeddedFunctionSignature{
			Params:  append([]wasm.ValType(nil), ft.Params...),
			Results: append([]wasm.ValType(nil), ft.Results...),
			TypeID:  ids[i],
		}
	}
	return out, nil
}

func EmbeddedTableValueType(m *wasm.Module, index uint32) (wasm.ValType, bool) {
	if m == nil || index != 0 {
		return wasm.ValType{}, false
	}
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternTable {
			return embeddedReferenceValueType(m.Imports[i].Type.Table.Ref)
		}
	}
	if len(m.Tables) == 1 {
		return embeddedReferenceValueType(m.Tables[0].Type.Ref)
	}
	return wasm.ValType{}, false
}

func embeddedReferenceValueType(ref wasm.RefType) (wasm.ValType, bool) {
	if !ref.Nullable || ref.Exact || ref.Heap.Kind != wasm.HeapAbs {
		return wasm.ValType{}, false
	}
	switch ref.Heap.Abs {
	case wasm.HeapFunc:
		return wasm.FuncRef, true
	case wasm.HeapExtern:
		return wasm.ExternRef, true
	default:
		return wasm.ValType{}, false
	}
}

func embeddedTable(m *wasm.Module, target string) (*EmbeddedTable, error) {
	var tableType *wasm.TableType
	var tableInit *wasm.Expr
	imported := false
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternTable {
			typ := m.Imports[i].Type.Table
			tableType, imported = &typ, true
			break
		}
	}
	if len(m.Tables) != 0 {
		tableType = &m.Tables[0].Type
		tableInit = m.Tables[0].Init
	}
	if tableType == nil && len(m.Elements) == 0 {
		return nil, nil
	}
	var tableValueType wasm.ValType
	if tableType == nil {
		tableType = &wasm.TableType{Ref: wasm.FuncRef.Ref}
		tableValueType = wasm.FuncRef
	} else {
		var ok bool
		tableValueType, ok = embeddedReferenceValueType(tableType.Ref)
		if !ok {
			return nil, fmt.Errorf("%s: table type %s is not supported", target, tableType.Ref)
		}
	}
	const maxTableSlots = uint64(^uint32(0) / 4)
	if tableType.Limits.Addr64 || tableType.Limits.Min > maxTableSlots {
		return nil, fmt.Errorf("%s: table limits exceed the addressable 32-bit slot range", target)
	}
	if tableInit != nil {
		return nil, fmt.Errorf("%s: table initializer expressions are not supported", target)
	}
	out := &EmbeddedTable{Imported: imported, Reference: tableType.Ref, Minimum: uint32(tableType.Limits.Min)}
	if tableType.Limits.Max != nil {
		if *tableType.Limits.Max > maxTableSlots {
			return nil, fmt.Errorf("%s: table maximum exceeds the addressable 32-bit slot range", target)
		}
		out.Maximum, out.HasMaximum = uint32(*tableType.Limits.Max), true
	}
	functionCount := m.ImportedFuncCount() + len(m.Code)
	out.Elements = make([]EmbeddedElementSegment, 0, len(m.Elements))
	for i := range m.Elements {
		elem := &m.Elements[i]
		mode := EmbeddedElementActive
		var offset uint32
		switch elem.Mode.Kind {
		case wasm.ElemActive:
			if elem.Mode.Table != 0 {
				return nil, fmt.Errorf("%s: element segment %d targets unsupported table", target, i)
			}
			var err error
			offset, err = embeddedI32Const(elem.Mode.Offset)
			if err != nil {
				return nil, fmt.Errorf("%s: element segment %d offset: %w", target, i, err)
			}
		case wasm.ElemPassive:
			mode = EmbeddedElementPassive
		case wasm.ElemDeclarative:
			mode = EmbeddedElementDeclarative
		default:
			return nil, fmt.Errorf("%s: element segment %d mode is not supported", target, i)
		}
		reference, valueType := wasm.FuncRef.Ref, wasm.FuncRef
		switch elem.Kind.Kind {
		case wasm.ElemFuncs, wasm.ElemFuncExprs:
		case wasm.ElemTypedExprs:
			var ok bool
			valueType, ok = embeddedReferenceValueType(elem.Kind.Ref)
			if !ok {
				return nil, fmt.Errorf("%s: element segment %d reference type %s is not supported", target, i, elem.Kind.Ref)
			}
			reference = elem.Kind.Ref
		default:
			return nil, fmt.Errorf("%s: element segment %d kind is not supported", target, i)
		}
		if mode == EmbeddedElementActive && tableValueType != valueType {
			return nil, fmt.Errorf("%s: element segment %d type does not match table", target, i)
		}
		values := make([]uint32, 0)
		if elem.Kind.Kind == wasm.ElemFuncs {
			values = make([]uint32, len(elem.Kind.Funcs))
			for j, index := range elem.Kind.Funcs {
				if uint64(index) >= uint64(functionCount) || uint32(index) == ^uint32(0) {
					return nil, fmt.Errorf("%s: element segment %d function %d is unavailable", target, i, index)
				}
				values[j] = uint32(index) + 1
			}
		} else {
			values = make([]uint32, len(elem.Kind.Exprs))
			for j := range elem.Kind.Exprs {
				var value uint32
				var err error
				if valueType == wasm.FuncRef {
					value, err = embeddedFuncRefConst(elem.Kind.Exprs[j], functionCount)
				} else {
					err = embeddedExternRefConst(elem.Kind.Exprs[j])
				}
				if err != nil {
					return nil, fmt.Errorf("%s: element segment %d value %d: %w", target, i, j, err)
				}
				values[j] = value
			}
		}
		out.Elements = append(out.Elements, EmbeddedElementSegment{Mode: mode, Reference: reference, Offset: offset, Values: values})
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

func embeddedExternRefConst(expr wasm.Expr) error {
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err != nil || op != 0xd0 {
			return fmt.Errorf("expected ref.null extern")
		}
		heap, err := r.S33()
		if err != nil || heap != -17 {
			return fmt.Errorf("expected ref.null extern")
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.HasNext() {
			return fmt.Errorf("malformed reference const expression")
		}
		return nil
	}
	if len(expr.Instrs) != 1 || expr.Instrs[0].Kind != wasm.InstrRefNull || expr.Instrs[0].HeapType() != wasm.AbsHeap(wasm.HeapExtern) {
		return fmt.Errorf("expected ref.null extern")
	}
	return nil
}

func embeddedLocalGlobalSlot(m *wasm.Module, index uint32) (uint32, bool) {
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

func EmbeddedGlobalLocation(m *wasm.Module, index uint32) (typ wasm.ValType, mutable bool, target uint32, imported bool, ok bool) {
	if m == nil {
		return wasm.ValType{}, false, 0, false, false
	}
	var importedIndex uint32
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != wasm.ExternGlobal {
			continue
		}
		if importedIndex == index {
			global := m.Imports[i].Type.Global
			if _, supported := MixedValueSlots(global.Type); !supported {
				return wasm.ValType{}, false, 0, false, false
			}
			return global.Type, global.Mutable, importedIndex, true, true
		}
		importedIndex++
	}
	if index < importedIndex {
		return wasm.ValType{}, false, 0, false, false
	}
	localIndex := index - importedIndex
	if uint64(localIndex) >= uint64(len(m.Globals)) {
		return wasm.ValType{}, false, 0, false, false
	}
	slot, ok := embeddedLocalGlobalSlot(m, localIndex)
	if !ok {
		return wasm.ValType{}, false, 0, false, false
	}
	global := m.Globals[localIndex]
	return global.Type.Type, global.Type.Mutable, slot, false, true
}

func embeddedGlobals(m *wasm.Module, target string) ([]EmbeddedGlobal, error) {
	out := make([]EmbeddedGlobal, len(m.Globals))
	for i := range m.Globals {
		global := &m.Globals[i]
		slot, ok := embeddedLocalGlobalSlot(m, uint32(i))
		if !ok {
			return nil, fmt.Errorf("%s: global %d has unsupported type or layout", target, i)
		}
		words, initGlobal, hasInitGlobal, err := embeddedGlobalInit(m, global.Init, global.Type.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: global %d initializer: %w", target, i, err)
		}
		out[i] = EmbeddedGlobal{Type: global.Type.Type, Mutable: global.Type.Mutable, Slot: slot, Words: words, HasInitGlobal: hasInitGlobal, InitGlobal: initGlobal}
	}
	return out, nil
}

func embeddedGlobalInit(m *wasm.Module, expr wasm.Expr, typ wasm.ValType) ([4]uint32, uint32, bool, error) {
	var words [4]uint32
	if len(expr.BodyBytes) != 0 {
		r := wasm.NewReader(expr.BodyBytes)
		op, err := r.Byte()
		if err == nil && op == 0x23 {
			index, err := r.U32()
			if err != nil {
				return words, 0, false, err
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.HasNext() {
				return words, 0, false, fmt.Errorf("malformed global.get const expression")
			}
			globalType, mutable, target, imported, ok := EmbeddedGlobalLocation(m, index)
			if !ok || !imported || mutable || globalType != typ {
				return words, 0, false, fmt.Errorf("global.get initializer %d is not an immutable imported %s", index, typ)
			}
			return words, target, true, nil
		}
	}
	if len(expr.Instrs) == 1 && expr.Instrs[0].Kind == wasm.InstrGlobalGet {
		index := expr.Instrs[0].Index
		globalType, mutable, target, imported, ok := EmbeddedGlobalLocation(m, index)
		if !ok || !imported || mutable || globalType != typ {
			return words, 0, false, fmt.Errorf("global.get initializer %d is not an immutable imported %s", index, typ)
		}
		return words, target, true, nil
	}
	if typ.Kind == wasm.ValRef {
		valueType, ok := embeddedReferenceValueType(typ.Ref)
		if !ok {
			return words, 0, false, fmt.Errorf("global type %s is not supported", typ)
		}
		if valueType == wasm.FuncRef {
			value, err := embeddedFuncRefConst(expr, m.FuncCount())
			words[0] = value
			return words, 0, false, err
		}
		return words, 0, false, embeddedExternRefConst(expr)
	}
	words, err := embeddedConstWords(expr, typ)
	return words, 0, false, err
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

func embeddedMemory(m *wasm.Module, target string) (*EmbeddedMemory, error) {
	var memoryType *wasm.MemType
	imported := false
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternMem {
			typ := m.Imports[i].Type.Mem
			memoryType, imported = &typ, true
			break
		}
	}
	if len(m.Memories) != 0 {
		memoryType = &m.Memories[0]
	}
	if memoryType == nil {
		return nil, nil
	}
	if memoryType.Limits.Addr64 || memoryType.Shared || memoryType.Limits.Min > uint64(^uint32(0)) {
		return nil, fmt.Errorf("%s: memory limits exceed the embedded memory32 profile", target)
	}
	out := &EmbeddedMemory{Imported: imported, Minimum: uint32(memoryType.Limits.Min)}
	if memoryType.Limits.Max != nil {
		if *memoryType.Limits.Max > uint64(^uint32(0)) {
			return nil, fmt.Errorf("%s: memory maximum exceeds the embedded memory32 profile", target)
		}
		out.Maximum, out.HasMaximum = uint32(*memoryType.Limits.Max), true
	}
	return out, nil
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
