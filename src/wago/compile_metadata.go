package wago

import (
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Keep metadata construction separate from native compilation so its temporary
// descriptors and validation paths do not inflate the native compile function.
//
//go:noinline
func compileModuleMetadata(c *Compiled, constExprCtx *constExprCompileContext, features frontend.Features, requirements *moduleRequirements, unsafeDirectTailImports []uint64, gcArrayProduct stagedGCArrayProduct, gcTypeSubtypingProduct stagedGCTypeSubtypingProduct, indexedFunctionRefOps bool) error {
	m, typeConverter := constExprCtx.module, constExprCtx.converter
	importedFuncs := c.NumImports
	moduleFacts := requirements.moduleFacts
	if importedFuncs > 0 {
		c.importFuncSigs = make([]FuncSig, importedFuncs)
		functionIndex := 0
		for importIndex := range m.Imports {
			if m.Imports[importIndex].Type.Kind != wasm.ExternFunc {
				continue
			}
			typeIdx := m.Imports[importIndex].Type.FuncType()
			var ft wasm.CompType
			if !m.ResolveTypeFunc(typeIdx.Index, &ft) {
				functionIndex++
				continue
			}
			params, err := typeConverter.abiTypes(ft.Params, c.Types)
			if err != nil {
				return fmt.Errorf("imported function %d params: %w", functionIndex, err)
			}
			results, err := typeConverter.abiTypes(ft.Results, c.Types)
			if err != nil {
				return fmt.Errorf("imported function %d results: %w", functionIndex, err)
			}
			unsafeCrossTail := false
			word := functionIndex >> 6
			if word < len(unsafeDirectTailImports) {
				unsafeCrossTail = unsafeDirectTailImports[word]&(uint64(1)<<uint(functionIndex&63)) != 0
			}
			c.importFuncSigs[functionIndex] = FuncSig{Params: params, Results: results, TypeIndex: typeIdx.Index, HasTypeIndex: true, unsafeCrossTail: unsafeCrossTail}
			functionIndex++
		}
	}
	importedTables := m.ImportedTableCount()
	importedMemories := m.ImportedMemCount()
	importedTags := m.ImportedTagCount()
	if importNameCount := importedFuncs + importedTables + importedMemories + importedTags; importNameCount != 0 {
		c.validateMemo.importModuleEnds = make([]uint64, importNameCount)
	}
	var valueTypes valueTypeInterner
	var additionalTableImports []tableImportDef
	if importedTables > 1 {
		additionalTableImports = make([]tableImportDef, 0, importedTables-1)
	}
	tableImportIndex := 0
	funcImportIndex := 0
	memoryImportIndex := 0
	tagImportIndex := 0
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, im.Module+"."+im.Name)
			c.validateMemo.importModuleEnds[funcImportIndex] = exactImportModuleEnd(im.Module)
			funcImportIndex++
		case wasm.ExternGlobal:
			exact, err := typeConverter.valueType(im.Type.GlobalType().Type, -1)
			if err != nil {
				return fmt.Errorf("global import %q.%q type: %w", im.Module, im.Name, err)
			}
			typeIndex := valueTypes.internCompiled(c, exact)
			abiType, err := typeConverter.abiType(im.Type.GlobalType().Type, c.Types)
			if err != nil {
				return fmt.Errorf("global import %q.%q ABI type: %w", im.Module, im.Name, err)
			}
			imp := GlobalImportDef{Module: im.Module, Name: im.Name, Type: abiType, ValueTypeIndex: typeIndex, HasValueType: true, Mutable: im.Type.GlobalType().Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, ValueTypeIndex: typeIndex, HasValueType: true, Mutable: imp.Mutable})
		case wasm.ExternMem:
			def := memoryDefFromWasm(im.Type.MemType())
			def.ImportKey = im.Module + "." + im.Name
			c.validateMemo.importModuleEnds[importedFuncs+importedTables+memoryImportIndex] = exactImportModuleEnd(im.Module)
			memoryImportIndex++
			c.memoryDir.defs = append(c.memoryDir.defs, def)
			if c.memoryImport == "" {
				c.memoryImport = def.ImportKey
			}
		case wasm.ExternTable:
			exact, err := typeConverter.valueType(wasm.RefVal(im.Type.TableType().Ref), -1)
			if err != nil {
				return fmt.Errorf("table import %q.%q type: %w", im.Module, im.Name, err)
			}
			abiType, err := typeConverter.abiType(wasm.RefVal(im.Type.TableType().Ref), c.Types)
			if err != nil {
				return fmt.Errorf("table import %q.%q ABI type: %w", im.Module, im.Name, err)
			}
			def := tableImportDef{Key: im.Module + "." + im.Name, Type: abiType, ValueTypeIndex: valueTypes.internCompiled(c, exact), HasValueType: true, Addr64: im.Type.TableType().Limits.Addr64}
			c.validateMemo.importModuleEnds[importedFuncs+tableImportIndex] = exactImportModuleEnd(im.Module)
			min := im.Type.TableType().Limits.Min
			if min > uint64(maxInt()) {
				return fmt.Errorf("table import %q.%q minimum %d overflows int", im.Module, im.Name, min)
			}
			def.Min = min
			if im.Type.TableType().Limits.HasMax {
				max := im.Type.TableType().Limits.Max
				if max > uint64(maxInt()) {
					return fmt.Errorf("table import %q.%q maximum %d overflows int", im.Module, im.Name, max)
				}
				def.Max = max
				def.HasMax = true
			}
			if tableImportIndex == 0 {
				c.tableImport = def.Key
				c.tableImportMin = int(def.Min)
				c.tableImportMax = int(def.Max)
				c.tableImportHasMax = def.HasMax
				c.TableAddr64 = def.Addr64
			} else {
				additionalTableImports = append(additionalTableImports, def)
			}
			tableImportIndex++
		case wasm.ExternTag:
			c.memoryDir.ehTags = append(c.memoryDir.ehTags, compiledTagDef{ImportKey: im.Module + "." + im.Name, TypeIndex: im.Type.TagType().Type.Index})
			c.validateMemo.importModuleEnds[importedFuncs+importedTables+importedMemories+tagImportIndex] = exactImportModuleEnd(im.Module)
			tagImportIndex++
		}
	}
	if features.ExceptionHandling {
		for i := range m.Tags {
			c.memoryDir.ehTags = append(c.memoryDir.ehTags, compiledTagDef{TypeIndex: m.Tags[i].Type.Index})
		}
	}
	funcABIValueCount := 0
	for li := range m.FuncTypes {
		ft, ok := m.LocalFuncType(li)
		if !ok {
			return fmt.Errorf("function %d: unknown type", li)
		}
		funcABIValueCount += len(ft.Params) + len(ft.Results)
	}
	c.Funcs = make([]FuncSig, len(m.FuncTypes))
	funcABIValues := make([]ValType, funcABIValueCount)
	funcABIValueAt := 0
	for li := range m.FuncTypes {
		var ft wasm.CompType
		if !m.ResolveLocalFuncType(li, &ft) {
			return fmt.Errorf("function %d: unknown type", li)
		}
		paramsEnd := funcABIValueAt + len(ft.Params)
		params := funcABIValues[funcABIValueAt:paramsEnd:paramsEnd]
		if err := typeConverter.abiTypesInto(params, ft.Params, c.Types); err != nil {
			return fmt.Errorf("function %d params: %w", li, err)
		}
		resultsEnd := paramsEnd + len(ft.Results)
		results := funcABIValues[paramsEnd:resultsEnd:resultsEnd]
		if err := typeConverter.abiTypesInto(results, ft.Results, c.Types); err != nil {
			return fmt.Errorf("function %d results: %w", li, err)
		}
		c.Funcs[li] = FuncSig{Params: params, Results: results, TypeIndex: m.FuncTypes[li].Index, HasTypeIndex: true}
		funcABIValueAt = resultsEnd
	}
	for i := range m.Globals {
		globalIndex := len(c.GlobalImports) + i
		_, gcStructGlobal := c.gcStructGlobalInit(globalIndex)
		_, gcArrayGlobal := c.gcArrayGlobalInit(globalIndex)
		gcGlobal := gcStructGlobal || gcArrayGlobal
		v := constExprResult{GlobalIndex: -1, FuncIndex: -1}
		if !gcGlobal {
			var err error
			v, err = evalConstExprWithContext(m.Globals[i].Init, m.Globals[i].Type.Type, constExprCtx)
			if err != nil {
				return fmt.Errorf("global %d initializer: %w", i, err)
			}
		}
		exact, err := typeConverter.valueType(m.Globals[i].Type.Type, -1)
		if err != nil {
			return fmt.Errorf("global %d type: %w", i, err)
		}
		abiType, err := typeConverter.abiType(m.Globals[i].Type.Type, c.Types)
		if err != nil {
			return fmt.Errorf("global %d ABI type: %w", i, err)
		}
		g := GlobalDef{Type: abiType, ValueTypeIndex: valueTypes.internCompiled(c, exact), HasValueType: true, Mutable: m.Globals[i].Type.Mutable}
		applyGlobalInit(&g, v.Init())
		c.Globals = append(c.Globals, g)
	}
	for i := range m.Exports {
		switch m.Exports[i].Index.Kind {
		case wasm.ExternFunc:
			c.Exports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternGlobal:
			c.GlobalExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternTable:
			if c.tableExports == nil {
				c.tableExports = make(map[string]int)
			}
			c.tableExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternMem:
			if c.memoryDir.exports == nil {
				c.memoryDir.exports = make(map[string]int)
			}
			c.memoryDir.exports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		case wasm.ExternTag:
			if c.memoryDir.ehTagExports == nil {
				c.memoryDir.ehTagExports = make(map[string]int)
			}
			c.memoryDir.ehTagExports[m.Exports[i].Name] = int(m.Exports[i].Index.Index)
		}
	}

	tableShapes, err := frontend.SupportedTableRuntimeShapesFromFacts(m, moduleFacts)
	if err != nil {
		return wrapContextError("compile", err)
	}
	c.HasTable = len(tableShapes) != 0
	if len(tableShapes) != 0 {
		c.TableSize = tableShapes[0].Size
		tt, ok := m.TableType(0)
		if !ok {
			return errors.New("table 0 type unavailable")
		}
		c.TableType, err = typeConverter.abiType(wasm.RefVal(tt.Ref), c.Types)
		if err != nil {
			return wrapContextError("table 0 ABI type", err)
		}
		exact, err := typeConverter.valueType(wasm.RefVal(tt.Ref), -1)
		if err != nil {
			return wrapContextError("table 0 type", err)
		}
		c.TableValueTypeIndex = valueTypes.internCompiled(c, exact)
		c.TableHasValueType = true
		c.TableAddr64 = tt.Limits.Addr64
		if c.tableImport == "" {
			c.TableHasMax = tt.Limits.HasMax
			c.TableMax = uint64(tableShapes[0].Capacity)
			if tt.Limits.HasMax {
				c.TableMax = tt.Limits.Max
			}
		}
	}
	if len(tableShapes) > 1 {
		c.extraTables = make([]tableDef, len(tableShapes)-1)
		for i := 1; i < len(tableShapes); i++ {
			tt, ok := m.TableType(uint32(i))
			if !ok {
				return fmt.Errorf("table %d type unavailable", i)
			}
			exact, err := typeConverter.valueType(wasm.RefVal(tt.Ref), -1)
			if err != nil {
				return fmt.Errorf("table %d type: %w", i, err)
			}
			abiType, err := typeConverter.abiType(wasm.RefVal(tt.Ref), c.Types)
			if err != nil {
				return fmt.Errorf("table %d ABI type: %w", i, err)
			}
			persistedMax := uint64(tableShapes[i].Capacity)
			if tt.Limits.HasMax {
				persistedMax = tt.Limits.Max
			}
			c.extraTables[i-1] = tableDef{Size: tableShapes[i].Size, Max: persistedMax, Type: abiType, ValueTypeIndex: valueTypes.internCompiled(c, exact), HasValueType: true, HasMax: tt.Limits.HasMax, Addr64: tt.Limits.Addr64}
		}
		for i, def := range additionalTableImports {
			c.extraTables[i] = tableDef{ImportKey: def.Key, Size: int(def.Min), Max: def.Max, Type: def.Type, ValueTypeIndex: def.ValueTypeIndex, HasValueType: def.HasValueType, ImportHasMax: def.HasMax, Addr64: def.Addr64}
		}
	}
	// Dynamic call_ref dispatch needs only the fixed arena header: ARM64
	// home-aware calls read its guaranteed instance-context cell even when the
	// module has no local ref.func/table descriptor payloads of its own.
	c.needsFuncRefContextHeader = moduleFacts.UsesCallRef
	c.NeedsFuncRefDescs = frontend.RequiresFuncRefDescriptorsFromFacts(m, moduleFacts) || gcTypeSubtypingProduct.usesLinkFunctionIdentity() || indexedFunctionRefOps
	for i := range m.Tables {
		tableIndex := importedTables + i
		if m.Tables[i].Init == nil {
			continue
		}
		initBody := m.Tables[i].Init.BodyBytes
		if len(initBody) == 0 {
			initBody, _ = wasm.EncodeExpr(*m.Tables[i].Init)
		}
		if _, matched, i31Err := evalI31ConstExprBytes(initBody, wasm.RefVal(m.Tables[i].Type.Ref), constExprCtx); matched && i31Err == nil {
			def := c.tableDef(tableIndex)
			values := make([]RefInit, def.Size)
			for j := range values {
				values[j] = RefInit{Expr: append([]byte(nil), initBody...)}
			}
			c.Elems = append(c.Elems, ElemInit{TableIndex: uint32(tableIndex), RefType: def.Type, ValueTypeIndex: def.ValueTypeIndex, HasValueType: def.HasValueType, Mode: ElemModeActive, Values: values})
			continue
		}
		payload, err := funcrefExprPayload(*m.Tables[i].Init)
		if err != nil {
			body := m.Tables[i].Init.BodyBytes
			if len(body) == 0 {
				body, _ = wasm.EncodeExpr(*m.Tables[i].Init)
			}
			r := wasm.NewReader(body)
			op, opErr := r.Byte()
			globalIndex, indexErr := r.U32()
			end, endErr := r.Byte()
			if opErr != nil || op != 0x23 || indexErr != nil || endErr != nil || end != 0x0b || r.BytesLeft() != 0 {
				return fmt.Errorf("table %d initializer: %w", tableIndex, err)
			}
			def := c.tableDef(tableIndex)
			values := make([]RefInit, def.Size)
			for j := range values {
				values[j] = RefInit{GlobalIndex: globalIndex, HasGlobal: true}
			}
			c.Elems = append(c.Elems, ElemInit{TableIndex: uint32(tableIndex), RefType: def.Type, ValueTypeIndex: def.ValueTypeIndex, HasValueType: def.HasValueType, Mode: ElemModeActive, Values: values})
			continue
		}
		if payload == nullFuncRefIndex {
			continue
		}
		if tableIndex == 0 {
			c.HasTableInitFunc = true
			c.TableInitFunc = payload
		} else {
			c.extraTables[tableIndex-1].HasInitFunc = true
			c.extraTables[tableIndex-1].InitFunc = payload
		}
	}
	for i := range m.Memories {
		c.memoryDir.defs = append(c.memoryDir.defs, memoryDefFromWasm(m.Memories[i]))
	}
	if len(c.memoryDir.defs) > 0 {
		memory0 := c.memoryDir.defs[0]
		c.HasMemory = true
		if memory0.Min <= uint64(^uint32(0)) {
			c.MemMinPages = uint32(memory0.Min)
		}
		c.MemMaxPages = 65536 // full memory32 reservation ceiling
		if memory0.Addr64 {
			c.MemMaxPages = 65535 // staged memory64 keeps its finite product ceiling
		}
		c.MemHasMax = memory0.HasMax
		if memory0.HasMax && memory0.Max < uint64(c.MemMaxPages) {
			c.MemMaxPages = uint32(memory0.Max)
		}
		// Pin a local memory-0 reservation to its initial size only when this
		// module never grows or exports it. Exact declared limits remain in the
		// directory for inspection, policy, linking, and codec round trips.
		memory0Observable := len(moduleFacts.MemoryGrowUsed) != 0 && (moduleFacts.MemoryGrowUsed[0] || moduleFacts.MemoryExported[0])
		if memory0.ImportKey == "" && !memory0Observable {
			c.MemMaxPages = c.MemMinPages
		}
	}
	if m.Start != nil {
		c.HasStart = true
		if int(*m.Start) < importedFuncs {
			// Imported start: run the imported function's host binding at instantiate
			// (validation guarantees () -> ()).
			c.StartIsImport = true
			c.StartImportIdx = int(*m.Start)
		} else {
			c.StartLocalFunc = int(*m.Start) - importedFuncs // validated local & () -> ()
		}
	}
	// Function descriptors back every executable funcref table. Table 0 keeps the
	// direct runtime slot; later table indexes use the bounded directory.
	c.FuncTypeID = make([]uint64, importedFuncs+len(m.FuncTypes))
	funcTypeIDAt := 0
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			typeIndex := m.Imports[i].Type.FuncType().Index
			key, ok := m.StructuralTypeKeyChecked(typeIndex)
			if !ok {
				return fmt.Errorf("import function type %d exceeds bounded native identity", typeIndex)
			}
			c.FuncTypeID[funcTypeIDAt] = key
			funcTypeIDAt++
		}
	}
	for li := range m.FuncTypes {
		typeIndex := m.FuncTypes[li].Index
		key, ok := m.StructuralTypeKeyChecked(typeIndex)
		if !ok {
			return fmt.Errorf("function type %d exceeds bounded native identity", typeIndex)
		}
		c.FuncTypeID[funcTypeIDAt] = key
		funcTypeIDAt++
	}
	elemStateCount, dataStateCount := requirements.elemStateCount, requirements.dataStateCount
	if elemStateCount > 0 {
		// table.init/elem.drop immediates address the module's original element
		// index space. Active/declarative slots remain zero-length (dropped).
		c.passiveElems = make([]ElemInit, elemStateCount)
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		var refType ValType
		var exactType ValueTypeDescriptor
		var values []RefInit
		if gcArrayProduct == stagedGCArrayProductReferenceElements && c.memoryDir.gcArrayElement != nil && uint32(i) == c.memoryDir.gcArrayElement.SegmentIndex {
			var err error
			exactType, err = typeConverter.valueType(wasm.RefVal(e.Kind.Ref), -1)
			if err != nil {
				return fmt.Errorf("element %d type: %w", i, err)
			}
			refType, err = typeConverter.abiType(wasm.RefVal(e.Kind.Ref), c.Types)
			if err != nil {
				return fmt.Errorf("element %d ABI type: %w", i, err)
			}
		} else {
			var err error
			refType, exactType, values, err = elementPayloads(m, c.Types, constExprCtx, e)
			if err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		hasValueType := e.Kind.Kind != wasm.ElemFuncs
		var valueTypeIndex uint32
		if hasValueType {
			valueTypeIndex = valueTypes.internCompiled(c, exactType)
		}
		init := ElemInit{TableIndex: uint32(e.Mode.Table), RefType: refType, ValueTypeIndex: valueTypeIndex, HasValueType: hasValueType, Mode: elemModeFromWasm(e.Mode.Kind), Values: values}
		if i < len(c.passiveElems) {
			state := init
			if e.Mode.Kind != wasm.ElemPassive {
				state.Values = nil // active/declarative segments start dropped
			}
			c.passiveElems[i] = state
		}
		switch e.Mode.Kind {
		case wasm.ElemPassive, wasm.ElemDeclarative:
			continue
		case wasm.ElemActive:
			want := wasm.I32
			table64 := false
			if tt, ok := m.TableType(uint32(e.Mode.Table)); ok && tt.Limits.Addr64 {
				want = wasm.I64
				table64 = true
			}
			base, err := evalConstExprWithContext(e.Mode.Offset, want, constExprCtx)
			if err != nil {
				return fmt.Errorf("element %d offset: %w", i, err)
			}
			if table64 {
				// OffsetInit's compact Base/HasGlobal forms are i32-only. Preserve the
				// validated i64 expression so codec version 4 and instantiation retain every bit.
				if len(e.Mode.Offset.BodyBytes) != 0 {
					init.Offset.Expr = append([]byte(nil), e.Mode.Offset.BodyBytes...)
				} else {
					encoded, err := wasm.EncodeExpr(e.Mode.Offset)
					if err != nil {
						return fmt.Errorf("element %d offset encode: %w", i, err)
					}
					init.Offset.Expr = encoded
				}
			} else {
				applyElemOffset(&init, base.Init())
			}
			// Preserve even empty active segments: the offset must still be bounds-
			// checked against the actual table length at instantiation time.
			c.Elems = append(c.Elems, init)
		}
	}
	if dataStateCount > 0 {
		// memory.init/data.drop immediates address the module's original data
		// index space. Active slots remain zero-length (dropped).
		c.PassiveData = make([]PassiveDataInit, dataStateCount)
	}
	// Active data dominates metadata in Go-produced modules (esbuild has tens of
	// thousands of segments). Reserve once instead of geometrically copying the
	// growing descriptor slice.
	activeData := 0
	for i := range m.Data {
		if m.Data[i].Mode.Kind != wasm.DataPassive {
			activeData++
		}
	}
	c.Data = make([]DataInit, 0, activeData)
	for i := range m.Data {
		d := &m.Data[i]
		if d.Mode.Kind == wasm.DataPassive {
			c.PassiveData[i] = PassiveDataInit{Bytes: append([]byte(nil), d.Init...)}
			continue
		}
		want := wasm.I32
		memory64 := false
		if mt, ok := m.MemoryType(uint32(d.Mode.Mem)); ok && mt.Limits.Addr64 {
			want = wasm.I64
			memory64 = true
		}
		off, err := evalConstExprWithContext(d.Mode.Offset, want, constExprCtx)
		if err != nil {
			return fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{MemoryIndex: uint32(d.Mode.Mem), Bytes: d.Init}
		if memory64 {
			// OffsetInit's compact Base/HasGlobal forms are intentionally i32-only.
			// Preserve the already validated i64 program in the existing Expr field so
			// codec version 4 retains the existing expression field while instantiation preserves all 64 address bits.
			if len(d.Mode.Offset.BodyBytes) != 0 {
				init.Offset.Expr = append([]byte(nil), d.Mode.Offset.BodyBytes...)
			} else {
				encoded, err := wasm.EncodeExpr(d.Mode.Offset)
				if err != nil {
					return fmt.Errorf("data %d offset encode: %w", i, err)
				}
				init.Offset.Expr = encoded
			}
		} else {
			applyDataOffset(&init, off.Init())
		}
		c.Data = append(c.Data, init)
	}
	return nil
}
