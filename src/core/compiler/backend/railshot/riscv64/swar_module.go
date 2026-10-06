//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

type swarModuleImage struct {
	code         []byte
	functions    []FunctionInfo
	globals      []swarGlobalInitializer
	data         [][]byte
	activeData   []swarActiveData
	memory       *wasm.MemType
	table        *wasm.TableType
	elements     []swarElement
	functionKeys []uint64
	hosts        []swarHostSignature
	hostCode     []byte
	hostEntries  []int
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
// one optional nonshared memory32 bounded literal globals and active/passive data are supported.
// Global cells are initialized separately by the private idle execution owner.
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
	publicStarts    bool
	exports         bool
	importedMemory  bool
	importedGlobals bool
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
	var err error
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: maxSWARLocals}); err != nil {
		return nil, err
	}
	if len(m.Code) == 0 && (!state.publicStarts || !state.exports || !hosts) || len(m.Code) > MaxFunctions || len(m.FuncTypes) != len(m.Code) || len(m.Types) > MaxFunctions || len(m.Imports) > MaxProductionImports || m.UsesCompactImports || len(m.Tables) > 1 || len(m.Globals) > 256 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil && !state.publicStarts || len(m.Elements) > MaxOwnedElementSegments || len(m.Data) > 256 || len(m.BranchHints) != 0 || len(m.Memories) > 1 {
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
		case wasm.ExternMem:
			if !state.importedMemory {
				return nil, unsupported("SWAR module scalar function imports only")
			}
			memoryImports++
		case wasm.ExternGlobal:
			if !state.importedGlobals || swarClassOf(im.Type.GlobalType().Type) == swarInvalid {
				return nil, unsupported("SWAR numeric global import profile")
			}
			globalImports++
		default:
			return nil, unsupported("SWAR module scalar function imports only")
		}
	}
	// State imports qualify with scalar host dispatch or a private owned table.
	// Function imports and tables remain separate policies.
	// Function indices count only function imports, never state imports.
	if memoryImports > 1 || memoryImports+len(m.Memories) > 1 {
		return nil, unsupported("SWAR imported memory profile")
	}
	if imported+len(m.Code) == 0 || imported > MaxHostFunctionImports || imported+len(m.Code) > MaxFunctions || imported != 0 && !mode.interruptible {
		return nil, unsupported("SWAR module aggregate functions/host bridge")
	}
	if !tables && (len(m.Tables) != 0 || len(m.Elements) != 0) {
		return nil, unsupported("SWAR default table/element policy")
	}
	var table *wasm.TableType
	var elements []swarElement
	var typeKeys, functionKeys []uint64
	if tables {
		ownedTable, err := admitOwnedTable(m, true)
		if err != nil {
			return nil, err
		}
		if ownedTable {
			table = &m.Tables[0].Type
			if err := swarOwnedTableType(table); err != nil {
				return nil, err
			}
			elements, err = swarModuleElements(m.Elements)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, custom := range m.Customs {
		if custom.Name != "name" && custom.Name != "producers" {
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
		if state.exports && ex.Index.Kind == wasm.ExternGlobal && uint64(ex.Index.Index) < uint64(globalImports+len(m.Globals)) {
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
	dataSegments, activeData, err := swarModuleData(m.Data, memory)
	if err != nil {
		return nil, err
	}
	globals, err := swarLiteralGlobals(m.Globals)
	if err != nil {
		return nil, err
	}
	globalTypes := make([]wasm.GlobalType, globalImports+len(globals))
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
		for i, index := range m.FuncTypes {
			if index.Rec || uint64(index.Index) >= uint64(len(typeKeys)) {
				return nil, unsupported("SWAR table function type index")
			}
			functionKeys[i] = typeKeys[index.Index]
		}
	}
	summaries := make([]swarSummary, len(m.Code))
	bound := 0
	for i := range m.Code {
		summaries[i], err = walkSWARPairsWithImports(&m.Code[i], functions[imported+i], memory != nil, types, functions, globalTypes, len(dataSegments), table, typeKeys, len(elements), imported, nil)
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
	image := &swarModuleImage{functions: make([]FunctionInfo, len(m.Code)), globals: globals, data: dataSegments, activeData: activeData, elements: elements, functionKeys: functionKeys, hosts: hostSignatures, hostCode: hostCode, hostEntries: hostEntries}
	if memory != nil {
		ownedMemory := *memory
		image.memory = &ownedMemory
	}
	if table != nil {
		ownedTable := *table
		image.table = &ownedTable
	}
	var calls []directCall
	for i := range m.Code {
		functionMode := mode
		functionMode.functionIndex = uint32(imported + i)
		functionMode.entryPC = uint32(m.Code[i].LocalDeclBytes)
		code, info, relocations, err := compileSWARPairsWithImports(&m.Code[i], functions[imported+i], memory, types, functions, globalTypes, len(dataSegments), table, typeKeys, len(elements), imported, functionMode)
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
	if functionCount+len(m.Code) == 0 {
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
		// Normal validation collects declared functions by scanning every
		// initializer. This pilot never implements table initializers.
		if table.Init != nil {
			return unsupported("SWAR decoded table initializer")
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
		// The pilot permits bounded legacy function-index payloads only.
		if element.Kind.Kind != wasm.ElemFuncs || len(element.Kind.Exprs) != 0 || len(element.Kind.Funcs) > MaxOwnedTableEntries-elementEntries || !consume(len(element.Kind.Funcs)*4) {
			return unsupported("SWAR decoded element payload bound")
		}
		elementEntries += len(element.Kind.Funcs)
	}
	rawNameAccounted := false
	for _, custom := range m.Customs {
		if !consume(len(custom.Name)) || !consume(len(custom.Data)) {
			return unsupported("SWAR decoded custom byte bound")
		}
		if custom.Name == "name" && bytes.Equal(custom.Data, m.RawNameSecPayload) {
			rawNameAccounted = true
		}
	}
	for _, im := range m.Imports {
		if !consume(len(im.Module)) || !consume(len(im.Name)) {
			return unsupported("SWAR decoded import name byte bound")
		}
	}
	for _, export := range m.Exports {
		if !consume(len(export.Name)) {
			return unsupported("SWAR decoded export name byte bound")
		}
	}
	if !rawNameAccounted && !consume(len(m.RawNameSecPayload)) {
		return unsupported("SWAR decoded name metadata byte bound")
	}
	if names := m.NameSec; names != nil {
		// Account decoded maps/strings as well as encoded payloads. Hand-built
		// callers may provide either representation, or inconsistent copies.
		// Small entry overhead also bounds work for arbitrary empty names.
		nameMap := func(rows wasm.NameMap) bool {
			if len(rows) > MaxFunctions {
				return false
			}
			for _, row := range rows {
				if !consume(2) || !consume(len(row.Name)) {
					return false
				}
			}
			return true
		}
		if names.ModuleName != nil && !consume(len(*names.ModuleName)) {
			return unsupported("SWAR decoded module name byte bound")
		}
		for _, rows := range []wasm.NameMap{names.FunctionNames, names.TypeNames, names.TableNames, names.MemoryNames, names.GlobalNames, names.ElementNames, names.DataNames, names.TagNames} {
			if !nameMap(rows) {
				return unsupported("SWAR decoded name map byte/count bound")
			}
		}
		for _, groups := range []wasm.IndirectNameMap{names.LocalNames, names.LabelNames, names.FieldNames} {
			if len(groups) > MaxFunctions {
				return unsupported("SWAR decoded indirect name count bound")
			}
			for _, group := range groups {
				if !consume(2) || !nameMap(group.Names) {
					return unsupported("SWAR decoded indirect name byte/count bound")
				}
			}
		}
	}
	return nil
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
