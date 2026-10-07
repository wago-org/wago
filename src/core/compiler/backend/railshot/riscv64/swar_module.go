//go:build linux && !tinygo

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarModuleImage struct {
	code               []byte
	functions          []FunctionInfo
	globals            []swarGlobalInitializer
	data               [][]byte
	activeData         []swarActiveData
	memory             *wasm.MemType
	table              *wasm.TableType
	elements           []swarElement
	publicInitializers bool // shared runtime owns global and data initialization
	publicTables       bool // shared runtime owns initializer metadata; private owner must not bind this image
	functionKeys       []uint64
	hosts              []swarHostSignature
	hostCode           []byte
	hostEntries        []int
}

type swarModulePolicy uint8

const (
	swarModulePlain swarModulePolicy = iota
	swarModuleTables
	swarModuleHosts
)

// This lowering image owns a bounded source snapshot and admits every
// function before allocating code. The production adapter additionally applies
// shared compiler options; public admission and instance setup are separate. Only defined functions, their exports, inert metadata
// one optional nonshared memory32, numeric globals and active/passive data are supported.
// Private images retain literal state for the idle execution owners; the public
// runtime owns its initializer metadata and resolves imported-global reads.
// The execution owner must provide matching call arenas and JobMemory layout.
func compileSWARModule(data []byte, mode emissionMode) (*swarModuleImage, error) {
	return compileSWARModulePolicy(data, mode, swarModulePlain)
}

// This explicit private policy additionally owns one bounded table and legacy
// elements. It does not change the default pilot or public feature admission.
func compileSWARTableModule(data []byte, mode emissionMode) (*swarModuleImage, error) {
	return compileSWARModulePolicy(data, mode, swarModuleTables)
}

// This separate private policy permits numeric/vector function imports, with defined
// mixed functions and owned synchronous dispatch. Tables stay closed; public reexports use owned native bridges.
func compileSWARHostModule(data []byte, mode emissionMode) (*swarModuleImage, error) {
	return compileSWARModulePolicy(data, mode, swarModuleHosts)
}

func compileSWARModulePolicy(data []byte, mode emissionMode, policy swarModulePolicy) (*swarModuleImage, error) {
	if !mode.production || len(data) > MaxModuleBytes || policy > swarModuleHosts {
		return nil, unsupported("SWAR module production/source bound")
	}
	owned := append([]byte(nil), data...)
	if err := admitSWARWirePolicy(owned, policy); err != nil {
		return nil, err
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(owned, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		return nil, err
	}
	return compileSWARDecodedModule(decoded.Module, mode, policy)
}

// Private decoded-module adapter for the compiler pipeline prerequisite. The
// caller owns immutable source while this call validates and lowers it; every
// retained image/state payload is copied. Wire provenance still requires the
// separate binary admission above (empty sections cannot be recovered here).
// Public integration must additionally enforce source, artifact and CPU admission.
func compileSWARDecodedModule(m *wasm.Module, mode emissionMode, policy swarModulePolicy) (*swarModuleImage, error) {
	return compileSWARDecodedModuleProfile(m, mode, policy, swarStateProfile{})
}

type swarStateProfile struct {
	publicInitializers   bool
	publicStarts         bool
	exports              bool
	importedMemory       bool
	importedGlobals      bool
	importedTable        bool
	functionTableImports bool // public retained controls only; private pilots stay separate
}

// Production integrates qualified state with current public lifecycles.
// Private pilots retain their original import/export policy.
func compileSWARDecodedModuleProfile(m *wasm.Module, mode emissionMode, policy swarModulePolicy, state swarStateProfile) (*swarModuleImage, error) {
	if m == nil || !mode.production || policy > swarModuleHosts {
		return nil, unsupported("SWAR decoded module production/policy")
	}
	if err := admitSWARDecodedBudgetProfile(m, state.publicStarts); err != nil {
		return nil, err
	}
	if state.publicStarts {
		if err := admitProductionStart(m); err != nil {
			return nil, err
		}
	}
	tables, hosts := policy == swarModuleTables, policy == swarModuleHosts
	hosts = hosts || tables && state.functionTableImports && state.publicStarts && state.exports && mode.interruptible && mode.foreignTableCalls
	mode.borrowedTable = state.importedTable && mode.foreignTableCalls && mode.interruptible && m.ImportedTableCount() != 0
	var err error
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{ExtendedConstGlobals: state.publicInitializers}, 1, wasm.ValidationLimits{MaxFunctionLocals: maxSWARLocals}); err != nil {
		return nil, err
	}
	if len(m.Code) == 0 && (!state.publicStarts || !state.exports) || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Imports) > MaxProductionImports || m.UsesCompactImports || len(m.Tables) > 1 || len(m.Globals) > 256 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil && !state.publicStarts || len(m.Elements) > MaxOwnedElementSegments || len(m.Data) > 256 || len(m.BranchHints) != 0 || len(m.Memories) > 1 {
		return nil, unsupported("SWAR module state/features/function bound")
	}
	imported, memoryImports, globalImports := 0, 0, 0
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternFunc:
			if !hosts {
				return nil, unsupported("SWAR module import policy")
			}
			imported++
		case wasm.ExternTable:
			if !tables || !state.importedTable || !mode.interruptible || !mode.foreignTableCalls {
				return nil, unsupported("SWAR imported table requires the retained public table ABI")
			}
		case wasm.ExternMem:
			if !state.importedMemory {
				return nil, unsupported("SWAR module scalar function imports only")
			}
			memoryImports++
		case wasm.ExternGlobal:
			reference := state.exports && state.publicInitializers && state.importedTable && mode.foreignTableCalls && admitImportedFunctionGlobal(m, im.Type.GlobalType())
			if !state.importedGlobals || swarClassOf(im.Type.GlobalType().Type) == swarInvalid && !reference {
				return nil, unsupported("SWAR numeric global import profile")
			}
			globalImports++
		default:
			return nil, unsupported("SWAR module scalar function imports only")
		}
	}
	// Only the public retained profile combines function imports and tables.
	// Function indices count only function imports, never state imports.
	if memoryImports > 1 || memoryImports+len(m.Memories) > 1 {
		return nil, unsupported("SWAR imported memory profile")
	}
	if imported+len(m.Code) == 0 && !state.publicStarts || imported > MaxHostFunctionImports || imported+len(m.Code) > MaxFunctions || imported != 0 && !mode.interruptible {
		return nil, unsupported("SWAR module aggregate functions/host bridge")
	}
	if !tables && (len(m.Tables) != 0 || len(m.Elements) != 0) {
		return nil, unsupported("SWAR default table/element policy")
	}
	var table *wasm.TableType
	var elements []swarElement
	var typeKeys, functionKeys []uint64
	if tables {
		ownedTable, err := admitTableProfile(m, true, state.importedTable && mode.interruptible && mode.foreignTableCalls)
		if err != nil {
			return nil, err
		}
		if ownedTable {
			resolved, _ := m.TableType(0)
			table = &resolved
			if err := swarOwnedTableType(table); err != nil {
				return nil, err
			}
			if !mode.foreignTableCalls {
				elements, err = swarModuleElements(m.Elements)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	for _, custom := range m.Customs {
		// Metadata bytes were bounded before validation. Opaque public custom
		// sections do not select lowering; parsed branch hints remain gated.
		if custom.Name == "metadata.code.branch_hint" || !state.publicStarts && custom.Name != "name" && custom.Name != "producers" {
			return nil, unsupported("SWAR custom section " + custom.Name)
		}
	}
	types := make([]*wasm.CompType, len(m.Types))
	for i := range m.Types {
		rec := &m.Types[i]
		if len(rec.SubTypes) != 1 {
			return nil, unsupported("SWAR recursive type group")
		}
		sub := &rec.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return nil, unsupported("SWAR type metadata")
		}
		if err := swarCallSignature(&sub.Comp); err != nil {
			return nil, err
		}
		types[i] = &sub.Comp
	}
	for _, ex := range m.Exports {
		if state.exports && tables && table != nil && ex.Index.Kind == wasm.ExternTable && ex.Index.Index == 0 {
			continue
		}
		if state.exports && ex.Index.Kind == wasm.ExternGlobal && uint64(ex.Index.Index) < uint64(globalImports+len(m.Globals)) {
			typ, _ := m.GlobalTypeByIndex(ex.Index.Index)
			if wasm.EqualValType(typ.Type, wasm.FuncRef) {
				if err := admitReferenceGlobalExport(m, ex.Index.Index); err != nil {
					return nil, err
				}
			}
			continue
		}
		if state.exports && ex.Index.Kind == wasm.ExternMem && ex.Index.Index == 0 && memoryImports+len(m.Memories) == 1 {
			continue
		}
		if ex.Index.Kind != wasm.ExternFunc || (uint64(ex.Index.Index) < uint64(imported) && !state.exports) || uint64(ex.Index.Index) >= uint64(imported+len(m.Code)) {
			return nil, unsupported("SWAR module export shape")
		}
	}
	var memory *wasm.MemType
	if memoryImports+len(m.Memories) != 0 {
		resolved, ok := m.MemoryType(0)
		if !ok {
			return nil, unsupported("SWAR memory index")
		}
		memory = &resolved
		if memory.Shared || memory.Limits.Addr64 || memory.Limits.Min > 65536 || (memory.Limits.HasMax && (memory.Limits.Max < memory.Limits.Min || memory.Limits.Max > 65536)) {
			return nil, unsupported("SWAR module memory32 resource")
		}
	}
	var dataSegments [][]byte
	var activeData []swarActiveData
	var globals []swarGlobalInitializer
	if state.publicInitializers {
		if err := admitSWARPublicInitializers(m, memory); err != nil {
			return nil, err
		}
	} else {
		var err error
		dataSegments, activeData, err = swarModuleData(m.Data, memory)
		if err != nil {
			return nil, err
		}
		globals, err = swarLiteralGlobals(m.Globals)
		if err != nil {
			return nil, err
		}
	}
	globalTypes := make([]wasm.GlobalType, globalImports+len(m.Globals))
	for i := range globalTypes {
		globalTypes[i], _ = m.GlobalTypeByIndex(uint32(i))
	}
	functions := make([]*wasm.CompType, imported+len(m.Code))
	hostSignatures := make([]swarHostSignature, imported)
	functionImport := 0
	for entry, im := range m.Imports {
		if im.Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(entry)
		if !ok || swarImportSignature(ft) != nil {
			return nil, unsupported("SWAR module imported carrier signature")
		}
		functions[functionImport] = ft
		hostSignatures[functionImport] = swarHostSignature{params: append([]wasm.ValType(nil), ft.Params...), results: append([]wasm.ValType(nil), ft.Results...)}
		functionImport++
	}
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil, unsupported("SWAR module function type")
		}
		functions[imported+i] = ft
	}
	if table != nil {
		typeKeys = make([]uint64, len(types))
		for i := range typeKeys {
			key, ok := m.StructuralTypeKeyChecked(uint32(i))
			if !ok {
				return nil, unsupported("SWAR checked table type identity")
			}
			typeKeys[i] = key
		}
		functionKeys = make([]uint64, len(functions))
		for i, ft := range functions {
			// The function namespace includes imported ordinals; all admitted
			// signatures are numeric and use their complete structural identity.
			functionKeys[i] = wasm.StructuralFuncTypeKey(ft)
		}
	}
	summaries := make([]swarSummary, len(m.Code))
	bound := 0
	for i := range m.Code {
		summaries[i], err = walkSWARPairsWithImportMode(&m.Code[i], functions[imported+i], memory != nil, types, functions, globalTypes, len(m.Data), table, typeKeys, len(m.Elements), imported, mode.foreignTableCalls, mode.borrowedTable, nil)
		if err != nil {
			return nil, fmt.Errorf("rv64: SWAR function %d: %w", i, err)
		}
		if summaries[i].usesLoops && !mode.interruptible {
			return nil, unsupported("SWAR module loops require interruption bridge")
		}
		if summaries[i].codeBound > MaxCodeBytes-bound-15 {
			return nil, unsupported("SWAR module aggregate code bound")
		}
		bound += summaries[i].codeBound + 15
	}
	hostCode, hostEntries, err := swarHostThunks(hostSignatures, MaxCodeBytes-bound)
	if err != nil {
		return nil, err
	}
	image := &swarModuleImage{publicInitializers: state.publicInitializers, functions: make([]FunctionInfo, len(m.Code)), globals: globals, data: dataSegments, activeData: activeData, elements: elements, functionKeys: functionKeys, hosts: hostSignatures, hostCode: hostCode, hostEntries: hostEntries}
	if memory != nil {
		ownedMemory := *memory
		image.memory = &ownedMemory
	}
	if table != nil {
		ownedTable := *table
		image.table = &ownedTable
		image.publicTables = mode.foreignTableCalls
	}
	var calls []directCall
	for i := range m.Code {
		functionMode := mode
		functionMode.functionIndex = uint32(imported + i)
		functionMode.entryPC = uint32(m.Code[i].LocalDeclBytes)
		code, info, relocations, err := compileSWARPairsWithImports(&m.Code[i], functions[imported+i], memory, types, functions, globalTypes, len(m.Data), table, typeKeys, len(m.Elements), imported, functionMode)
		if err != nil {
			return nil, fmt.Errorf("rv64: SWAR function %d: %w", i, err)
		}
		padding := (-len(image.code)) & 15
		if len(code) > MaxCodeBytes-len(image.hostCode)-len(image.code)-padding {
			return nil, unsupported("SWAR module image bound")
		}
		for ; padding > 0; padding -= 4 {
			image.code = append(image.code, 0x13, 0, 0, 0)
		}
		info.Entry, info.CodeBytes = len(image.code), len(code)
		image.functions[i] = info
		for j := range relocations {
			relocations[j].site += info.Entry
		}
		calls = append(calls, relocations...)
		image.code = append(image.code, code...)
	}
	a := enc.Asm{B: image.code}
	for _, call := range calls {
		if err := a.PatchLong(call.site, int64(image.functions[call.callee].Entry)); err != nil {
			return nil, fmt.Errorf("rv64: SWAR direct relocation: %w", err)
		}
	}
	return image, nil
}

func admitSWARDecodedBudget(m *wasm.Module) error {
	return admitSWARDecodedBudgetProfile(m, false)
}

func admitSWARDecodedBudgetProfile(m *wasm.Module, publicStarts bool) error {
	// Count and byte budgets precede validation, metadata expansion and payload
	// copies. Decoded callers do not necessarily retain a bounded source blob.
	if len(m.Code) == 0 && !publicStarts || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Imports) > MaxProductionImports || len(m.Tables) > 1 || len(m.Memories) > 1 || len(m.Globals) > 256 || len(m.Data) > 256 || len(m.Elements) > MaxOwnedElementSegments || len(m.Exports) > MaxFunctions || len(m.Customs) > MaxFunctions || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil && !publicStarts || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return unsupported("SWAR module state/features/function bound")
	}
	globalCount, functionCount := len(m.Globals), 0
	for _, im := range m.Imports {
		switch im.Type.Kind {
		case wasm.ExternGlobal:
			globalCount++
		case wasm.ExternFunc:
			functionCount++
		}
	}
	if functionCount+len(m.Code) == 0 && !publicStarts {
		return unsupported("SWAR module state/features/function bound")
	}
	if functionCount > MaxHostFunctionImports {
		return unsupported("SWAR function import bound")
	}
	if globalCount > 256 {
		return unsupported("SWAR aggregate global bound")
	}
	remaining := MaxModuleBytes
	consume := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 || swarCallSignature(&group.SubTypes[0].Comp) != nil {
			return unsupported("SWAR decoded numeric function types")
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || len(sub.Comp.Fields) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return unsupported("SWAR decoded type metadata")
		}
		if !consume(len(group.SubTypes[0].Comp.Params) + len(group.SubTypes[0].Comp.Results)) {
			return unsupported("SWAR decoded source byte bound")
		}
	}
	for _, fn := range m.Code {
		if fn.BodyBytes == nil || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || len(fn.BodyBytes) > 64<<10 || len(fn.Locals.Runs) > maxSWARLocals || fn.LocalDeclBytes > MaxModuleBytes || !consume(len(fn.BodyBytes)) || !consume(int(fn.LocalDeclBytes)) {
			return unsupported("SWAR decoded byte-backed function bound")
		}
	}
	for _, table := range m.Tables {
		// Bound expressions before normal validation scans declared functions.
		if table.Init != nil {
			if !publicStarts || !consume(len(table.Init.BodyBytes)) {
				return unsupported("SWAR decoded table initializer")
			}
			if err := admitTableInitializer(*table.Init, m); err != nil {
				return err
			}
		}
	}
	for _, global := range m.Globals {
		if global.Init.BodyBytes == nil || len(global.Init.Instrs) != 0 || !consume(len(global.Init.BodyBytes)) {
			return unsupported("SWAR decoded global initializer byte bound")
		}
	}
	for _, data := range m.Data {
		if len(data.Mode.Offset.Instrs) != 0 || !consume(len(data.Mode.Offset.BodyBytes)) || !consume(len(data.Init)) {
			return unsupported("SWAR decoded data byte bound")
		}
	}
	elementEntries := 0
	for _, element := range m.Elements {
		if len(element.Mode.Offset.Instrs) != 0 || !consume(len(element.Mode.Offset.BodyBytes)) {
			return unsupported("SWAR decoded element offset byte bound")
		}
		if len(element.Kind.Funcs) > MaxOwnedTableEntries-elementEntries || len(element.Kind.Exprs) > MaxOwnedTableEntries-elementEntries-len(element.Kind.Funcs) || !consume(len(element.Kind.Funcs)*4) {
			return unsupported("SWAR decoded element payload bound")
		}
		elementEntries += len(element.Kind.Funcs) + len(element.Kind.Exprs)
		switch element.Kind.Kind {
		case wasm.ElemFuncs:
			if len(element.Kind.Exprs) != 0 {
				return unsupported("SWAR legacy expression payload")
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			if !publicStarts || len(element.Kind.Funcs) != 0 {
				return unsupported("SWAR public element expressions required")
			}
			for _, expr := range element.Kind.Exprs {
				if _, err := elementInitializerExpression(expr, m, element.Mode.Kind, publicStarts); err != nil {
					return err
				}
				if !consume(len(expr.BodyBytes)) {
					return unsupported("SWAR element expression byte budget")
				}
			}
		default:
			return unsupported("SWAR element payload kind")
		}
	}
	return admitSWARDecodedMetadataBudget(m, &remaining)
}

// Reject unsupported sections even when empty. A decoded module alone cannot
// distinguish absent unsupported features from present empty feature sections.
func admitSWARWirePolicy(data []byte, policy swarModulePolicy) error {
	if len(data) < 8 || string(data[:8]) != "\x00asm\x01\x00\x00\x00" {
		return fmt.Errorf("rv64: invalid SWAR module header")
	}
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return err
		}
		size, err := r.U32()
		if err != nil || uint64(size) > uint64(r.BytesLeft()) {
			return fmt.Errorf("rv64: truncated SWAR section")
		}
		switch id {
		case 0, 3, 5, 6, 7, 10, 11, 12:
		case 1:
			start := 8 + r.Offset()
			types := wasm.ReaderFrom(data[start : start+int(size)])
			count, err := types.U32()
			if err != nil || count > MaxFunctions {
				return unsupported("SWAR wire type count")
			}
			for range count {
				kind, err := types.Byte()
				if err != nil || kind != 0x60 {
					return unsupported("SWAR plain numeric type grammar")
				}
				for range 2 {
					n, err := types.U32()
					if err != nil || n > maxSWARLocals {
						return unsupported("SWAR wire signature bound")
					}
					for range n {
						typ, err := types.Byte()
						if err != nil || typ < 0x7b || typ > 0x7f {
							return unsupported("SWAR wire numeric carrier")
						}
					}
				}
			}
			if types.HasNext() {
				return fmt.Errorf("rv64: trailing SWAR type bytes")
			}
		case 2:
			if policy != swarModuleHosts {
				return unsupported("SWAR function import section")
			}
		case 4, 9:
			if policy != swarModuleTables {
				return unsupported(fmt.Sprintf("SWAR section %d", id))
			}
		default:
			return unsupported(fmt.Sprintf("SWAR section %d", id))
		}
		if err := r.JumpTo(r.Offset() + int(size)); err != nil {
			return err
		}
	}
	return nil
}
