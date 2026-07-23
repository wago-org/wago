package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

type GCConfig = gc.Config
type GCProfile = gc.Profile
type GCAllocatorKind = gc.AllocatorKind
type GCRuntimeKind = gc.RuntimeKind

const (
	GCAllocatorPagedSizeClass     = gc.AllocatorPagedSizeClass
	GCAllocatorTinyFixedBlock     = gc.AllocatorTinyFixedBlock
	GCProfileThroughput           = gc.ProfileThroughput
	GCProfileTiny                 = gc.ProfileTiny
	GCRuntimeGenerational         = gc.RuntimeGenerational
	GCRuntimeIncrementalMarkSweep = gc.RuntimeIncrementalMarkSweep
)

// Compile decodes, validates, and compiles a wasm module to native code.
//
// On success ownership of wasmBytes transfers to the returned Compiled. The
// caller must not mutate or reuse its backing array: decoded segment metadata
// may retain slices into that storage. This avoids an input-sized copy while the
// compiled artifact is live.
//
// It accepts both the current explicit form:
//
//	Compile(cfg, wasmBytes)
//
// and the original default-config shorthand:
//
//	Compile(wasmBytes)
//
// Pass nil as cfg, or omit it, to use NewRuntimeConfig.
func Compile(args ...any) (*Compiled, error) {
	cfg, wasmBytes, err := compileArgs(args)
	if err != nil {
		return nil, err
	}
	return compileWithConfig(cfg, wasmBytes)
}

// CompileWithConfig is the named compatibility form of Compile(cfg, wasmBytes).
// It has the same successful-call ownership transfer for wasmBytes. The
// config's feature set gates which modules are accepted and its bounds-check
// mode selects the code-generation strategy.
func CompileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
	return compileWithConfig(cfg, wasmBytes)
}

func compileArgs(args []any) (*RuntimeConfig, []byte, error) {
	switch len(args) {
	case 1:
		wasmBytes, ok := args[0].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %T", args[0])
		}
		return nil, wasmBytes, nil
	case 2:
		var cfg *RuntimeConfig
		if args[0] != nil {
			var ok bool
			cfg, ok = args[0].(*RuntimeConfig)
			if !ok {
				return nil, nil, fmt.Errorf("wago: Compile config must be *RuntimeConfig or nil, got %T", args[0])
			}
		}
		wasmBytes, ok := args[1].([]byte)
		if !ok {
			return nil, nil, fmt.Errorf("wago: Compile wasm bytes must be []byte, got %T", args[1])
		}
		return cfg, wasmBytes, nil
	default:
		return nil, nil, fmt.Errorf("wago: Compile expects []byte or (*RuntimeConfig, []byte), got %d arguments", len(args))
	}
}

// functionWorkersForModule resolves the configured policy once so validation
// and codegen use the same bounded worker count for a module.
func functionWorkersForModule(m *wasm.Module, policy int) int {
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	return functionworkers.Resolve(policy, len(m.Code), totalBody)
}

func compileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
	return compileWithConfigAndInstructions(cfg, wasmBytes, nil)
}

func compileWithConfigAndInstructions(cfg *RuntimeConfig, wasmBytes []byte, instructions map[string]*registeredInstruction) (*Compiled, error) {
	if cfg == nil {
		cfg = NewRuntimeConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	workers := functionWorkersForModule(m, cfg.functionWorkers)
	if err := wasm.ValidateModuleWithWorkers(m, workers); err != nil {
		// Proposal-aware decoding can expose a structurally unsupported module to
		// validation before the validator has complete proposal subtyping rules.
		// Compile must still fail clearly as unsupported rather than mislabeling a
		// valid proposal module as an ordinary core type error.
		if unsupported := frontend.RejectUnsupportedWithFeatures(m, cfg.frontendFeatures()); unsupported != nil && isUnsupportedProposalError(unsupported) {
			return nil, fmt.Errorf("compile: %w", unsupported)
		}
		return nil, fmt.Errorf("validate: %w", err)
	}
	gcDescs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		return nil, fmt.Errorf("gc descriptors: %w", err)
	}
	if err := frontend.RejectUnsupportedWithFeatures(m, cfg.frontendFeatures()); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	var functionIndex uint32
	for i := range m.Imports {
		imp := &m.Imports[i]
		if imp.Type.Kind != wasm.ExternFunc {
			continue
		}
		if ins := instructions[imp.Module+"."+imp.Name]; ins != nil {
			ft, ok := m.FuncSignature(functionIndex)
			if !ok {
				return nil, fmt.Errorf("compile: instruction import %q has no function signature", imp.Module+"."+imp.Name)
			}
			sig := FuncSig{Params: valTypesFromWasm(ft.Params), Results: valTypesFromWasm(ft.Results)}
			if err := validateInstructionSignature(imp.Module+"."+imp.Name, ins.spec, sig); err != nil {
				return nil, err
			}
		}
		functionIndex++
	}
	customInstructions := resolveInstructionLowerings(m, instructions)
	// Architectures that always use the sync-host dispatcher can compile host
	// defaults up front; others defer returning imports until link time.
	boundsMode := effectiveCompileBoundsMode(cfg.boundsChecks, m)
	elide := boundsMode == BoundsChecksSignalsBased
	importedFuncs := m.ImportedFuncCount()
	dynamicBindings := make([]railshotImportBinding, importedFuncs)
	for i := range dynamicBindings {
		dynamicBindings[i] = railshotImportBinding{Dynamic: true, ImportIndex: uint32(i)}
	}
	pressureAt, pressure := compileMemoryPressure(len(wasmBytes))
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{Workers: workers, ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds, ImportBindings: dynamicBindings, Interruptible: true, MemoryPressureAt: pressureAt, MemoryPressure: pressure, CustomInstructions: customInstructions})
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	code, entry, internalEntry := cm.Code, cm.Entry, cm.InternalEntry

	c := &Compiled{Code: code, Entry: entry, InternalEntry: internalEntry, NumImports: importedFuncs, Exports: map[string]int{}, Names: m.NameSec, GlobalExports: map[string]int{}, hasTableExportMetadata: true, boundsMode: boundsMode, GCTypeDescs: gcDescs, requiredFeatures: uint8(moduleRequiredFeatures(m)), dynamicImports: importedFuncs > 0, customInstructions: customInstructions, requiresAVX2: cm.RequiresAVX2, requiresAVX512: cm.RequiresAVX512}
	if importedFuncs > 0 {
		c.importFuncSigs = make([]FuncSig, importedFuncs)
		for i := 0; i < importedFuncs; i++ {
			if ft, ok := m.FuncSignature(uint32(i)); ok {
				c.importFuncSigs[i] = FuncSig{valTypesFromWasm(ft.Params), valTypesFromWasm(ft.Results)}
			}
		}
	}
	importedTables := m.ImportedTableCount()
	var additionalTableImports []tableImportDef
	if importedTables > 1 {
		additionalTableImports = make([]tableImportDef, 0, importedTables-1)
	}
	tableImportIndex := 0
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case wasm.ExternFunc:
			c.Imports = append(c.Imports, im.Module+"."+im.Name)
		case wasm.ExternGlobal:
			imp := GlobalImportDef{Module: im.Module, Name: im.Name, Type: valTypeFromWasm(im.Type.Global.Type), Mutable: im.Type.Global.Mutable}
			c.GlobalImports = append(c.GlobalImports, imp)
			c.Globals = append(c.Globals, GlobalDef{Type: imp.Type, Mutable: imp.Mutable})
		case wasm.ExternMem:
			c.memoryImport = im.Module + "." + im.Name
			c.HasMemory = true
			c.MemMinPages = uint32(im.Type.Mem.Limits.Min)
			c.MemMaxPages = 65535
			if im.Type.Mem.Limits.Max != nil {
				c.MemMaxPages = uint32(*im.Type.Mem.Limits.Max)
				c.MemHasMax = true
			}
		case wasm.ExternTable:
			def := tableImportDef{Key: im.Module + "." + im.Name, Type: valTypeFromWasm(wasm.RefVal(im.Type.Table.Ref))}
			min := im.Type.Table.Limits.Min
			if min > uint64(maxInt()) {
				return nil, fmt.Errorf("table import %q.%q minimum %d overflows int", im.Module, im.Name, min)
			}
			def.Min = int(min)
			if im.Type.Table.Limits.Max != nil {
				max := *im.Type.Table.Limits.Max
				if max > uint64(maxInt()) {
					return nil, fmt.Errorf("table import %q.%q maximum %d overflows int", im.Module, im.Name, max)
				}
				def.Max = int(max)
				def.HasMax = true
			}
			if tableImportIndex == 0 {
				c.tableImport = def.Key
				c.tableImportMin = def.Min
				c.tableImportMax = def.Max
				c.tableImportHasMax = def.HasMax
			} else {
				additionalTableImports = append(additionalTableImports, def)
			}
			tableImportIndex++
		}
	}
	for li := range m.FuncTypes {
		ft, ok := m.LocalFuncType(li)
		if !ok {
			return nil, fmt.Errorf("function %d: unknown type", li)
		}
		c.Funcs = append(c.Funcs, FuncSig{valTypesFromWasm(ft.Params), valTypesFromWasm(ft.Results)})
	}
	for i := range m.Globals {
		v, err := evalConstExprWithModule(m.Globals[i].Init, m.Globals[i].Type.Type, m)
		if err != nil {
			return nil, fmt.Errorf("global %d initializer: %w", i, err)
		}
		g := GlobalDef{Type: valTypeFromWasm(m.Globals[i].Type.Type), Mutable: m.Globals[i].Type.Mutable}
		applyGlobalInit(&g, v.Init())
		c.Globals = append(c.Globals, g)
	}
	memoryExported := false
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
			if c.tableExports == nil {
				c.tableExports = make(map[string]int)
			}
			c.tableExports[m.Exports[i].Name] = memoryExportSentinel
			memoryExported = true
		}
	}

	tableShapes, err := frontend.SupportedTableRuntimeShapes(m)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	c.HasTable = len(tableShapes) != 0
	if len(tableShapes) != 0 {
		c.TableSize = tableShapes[0].Size
		c.TableMax = tableShapes[0].Capacity
		tt, ok := m.TableType(0)
		if !ok {
			return nil, fmt.Errorf("table 0 type unavailable")
		}
		c.TableType = valTypeFromWasm(wasm.RefVal(tt.Ref))
		if c.tableImport == "" {
			c.TableHasMax = tt.Limits.Max != nil
		}
	}
	if len(tableShapes) > 1 {
		c.extraTables = make([]tableDef, len(tableShapes)-1)
		for i := 1; i < len(tableShapes); i++ {
			tt, ok := m.TableType(uint32(i))
			if !ok {
				return nil, fmt.Errorf("table %d type unavailable", i)
			}
			c.extraTables[i-1] = tableDef{Size: tableShapes[i].Size, Max: tableShapes[i].Capacity, Type: valTypeFromWasm(wasm.RefVal(tt.Ref)), HasMax: tt.Limits.Max != nil}
		}
		for i, def := range additionalTableImports {
			c.extraTables[i] = tableDef{ImportKey: def.Key, Size: def.Min, Max: def.Max, Type: def.Type, ImportHasMax: def.HasMax}
		}
	}
	c.NeedsFuncRefDescs = frontend.RequiresFuncRefDescriptors(m)
	for i := range m.Tables {
		tableIndex := importedTables + i
		if m.Tables[i].Init == nil {
			continue
		}
		payload, err := funcrefExprPayload(*m.Tables[i].Init)
		if err != nil {
			return nil, fmt.Errorf("table %d initializer: %w", tableIndex, err)
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
	if len(m.Memories) > 0 {
		lim := m.Memories[0].Limits
		c.HasMemory = true
		c.MemMinPages = uint32(lim.Min)
		c.MemMaxPages = 65535 // default ceiling when the module declares no max
		if lim.Max != nil {
			c.MemMaxPages = uint32(*lim.Max)
			c.MemHasMax = true
		}
		// Pin the reservation to the initial size only when this module never grows
		// the memory AND doesn't export it — an exported memory may be grown by
		// another instance that imports it (cross-instance shared memory).
		if !moduleUsesMemoryGrow(m) && !memoryExported {
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
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			c.FuncTypeID = append(c.FuncTypeID, m.StructuralTypeID(m.Imports[i].Type.Type.Index))
		}
	}
	for li := range m.FuncTypes {
		c.FuncTypeID = append(c.FuncTypeID, m.StructuralTypeID(m.FuncTypes[li].Index))
	}
	elemStateCount, dataStateCount := moduleSegmentStateCounts(m)
	if elemStateCount > 0 {
		// table.init/elem.drop immediates address the module's original element
		// index space. Active/declarative slots remain zero-length (dropped).
		c.passiveElems = make([]ElemInit, elemStateCount)
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		refType, values, err := elementPayloads(m, e)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		init := ElemInit{TableIndex: uint32(e.Mode.Table), RefType: refType, Mode: elemModeFromWasm(e.Mode.Kind), Values: values}
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
			base, err := evalConstExprWithModule(e.Mode.Offset, wasm.I32, m)
			if err != nil {
				return nil, fmt.Errorf("element %d offset: %w", i, err)
			}
			applyElemOffset(&init, base.Init())
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
		off, err := evalConstExprWithModule(d.Mode.Offset, wasm.I32, m)
		if err != nil {
			return nil, fmt.Errorf("data %d offset: %w", i, err)
		}
		init := DataInit{Bytes: d.Init}
		applyDataOffset(&init, off.Init())
		c.Data = append(c.Data, init)
	}
	return installCompiledFinalizer(c), nil
}

func isUnsupportedProposalError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"tag section", "exception", "throw", "try_table",
		"return_call", "tail call",
		"reference instruction", "reference type", "ref func", "ref type ", "call_ref", "br_on_null", "br_on_non_null", "ref.as_non_null",
		"shared memory", "atomic instruction", "atomic.",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// effectiveCompileBoundsMode keeps zero-minimum memories correct on ARM64.
// The current ARM64 guard entry places control words immediately below linMem;
// when linMem begins on the first inaccessible linear page, entry is not reliable
// across the supported Linux and Darwin signal trampolines. Compile those rare
// modules with explicit checks and classic growable memory instead.
func effectiveCompileBoundsMode(requested BoundsCheckMode, m *wasm.Module) BoundsCheckMode {
	if requested != BoundsChecksSignalsBased || goruntime.GOARCH != "arm64" {
		return requested
	}
	if min, ok := moduleInitialMemoryPages(m); ok && min == 0 {
		return BoundsChecksExplicit
	}
	return requested
}

func moduleInitialMemoryPages(m *wasm.Module) (uint64, bool) {
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternMem {
			return m.Imports[i].Type.Mem.Limits.Min, true
		}
	}
	if len(m.Memories) != 0 {
		return m.Memories[0].Limits.Min, true
	}
	return 0, false
}

func compileMemoryPressure(sourceBytes int) (int, func()) {
	// One checkpoint is enough to prevent dead per-function state from riding the
	// GC growth curve to the end of real-world modules. Small modules stay on the
	// zero-overhead path. runtime.GC does not modify GOGC or GOMEMLIMIT.
	if sourceBytes < 8<<20 {
		return 0, nil
	}
	return 0, goruntime.GC
}

func elemModeFromWasm(mode wasm.ElemModeKind) ElemMode {
	switch mode {
	case wasm.ElemPassive:
		return ElemModePassive
	case wasm.ElemDeclarative:
		return ElemModeDeclarative
	default:
		return ElemModeActive
	}
}

func elementPayloads(m *wasm.Module, e *wasm.Elem) (ValType, []RefInit, error) {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		out := make([]RefInit, len(e.Kind.Funcs))
		for i, fidx := range e.Kind.Funcs {
			out[i] = RefInit{FuncIndex: uint32(fidx)}
		}
		return ValFuncRef, out, nil
	case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
		refType := ValFuncRef
		if e.Kind.Kind == wasm.ElemTypedExprs {
			refType = valTypeFromWasm(wasm.RefVal(e.Kind.Ref))
		}
		out := make([]RefInit, len(e.Kind.Exprs))
		for i, ex := range e.Kind.Exprs {
			payload, err := wasm.ParseElementExpr(ex)
			if err != nil {
				return 0, nil, fmt.Errorf("expression %d: %w", i, err)
			}
			if payload.HasGlobal {
				gt, ok := m.GlobalTypeByIndex(payload.GlobalIndex)
				if !ok {
					return 0, nil, fmt.Errorf("expression %d global index %d out of range", i, payload.GlobalIndex)
				}
				if int(payload.GlobalIndex) >= m.ImportedGlobalCount() || gt.Mutable {
					return 0, nil, fmt.Errorf("expression %d global %d is not an immutable import", i, payload.GlobalIndex)
				}
				if valTypeFromWasm(gt.Type) != refType {
					return 0, nil, fmt.Errorf("expression %d global type does not match segment type %s", i, refType)
				}
				out[i] = RefInit{HasGlobal: true, GlobalIndex: payload.GlobalIndex}
				continue
			}
			if valTypeFromWasm(wasm.RefVal(payload.RefType)) != refType {
				return 0, nil, fmt.Errorf("expression %d type does not match segment type %s", i, refType)
			}
			out[i] = RefInit{FuncIndex: payload.FuncIndex, Null: payload.Null}
		}
		return refType, out, nil
	default:
		return 0, nil, fmt.Errorf("unsupported element kind %d", e.Kind.Kind)
	}
}

func funcrefExprPayload(e wasm.Expr) (uint32, error) {
	payload, err := wasm.ParseFuncrefElementExpr(e)
	if err != nil {
		return 0, err
	}
	if payload.Null {
		return nullFuncRefIndex, nil
	}
	return payload.FuncIndex, nil
}

func funcTypeUsesV128(ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, t := range ft.Params {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	for _, t := range ft.Results {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	return false
}

// asyncReplayable reports whether a host import's signature can be served by the
// async log-and-replay path, which captures a single i32 argument and no results.
// Every other signature must run through the synchronous host dispatcher.
func asyncReplayable(sig FuncSig) bool {
	return len(sig.Results) == 0 && len(sig.Params) <= 1 &&
		(len(sig.Params) == 0 || sig.Params[0] == ValI32)
}

func (c *Compiled) importsRequireSync(imports Imports, force bool) bool {
	if force || forceSyncHostImports || c.needsPublicFuncrefHostReentry() {
		return true
	}
	for _, key := range c.Imports {
		if export, cross := imports[key].(*InstanceExport); cross {
			// A cross-instance-only consumer needs the parked-host loop only when
			// its target can itself park. syncMode is immutable after the producer
			// is instantiated and already includes its transitive direct imports.
			if export == nil || export.inst == nil || export.inst.syncMode {
				return true
			}
			continue
		}
		// Every actual host binding remains synchronous. In particular, a legacy
		// replayable HostFunc may later be reached through an InstanceExport, where
		// logging into the callee's private buffer would be invisible to the public
		// root. Only host-free cross-instance links take the fast native path.
		return true
	}
	// An imported funcref table can be mutated to contain a host or
	// cross-instance descriptor after instantiation, so it remains conservative.
	for i := 0; i < c.tableImportCount(); i++ {
		if c.tableElementType(i) == ValFuncRef {
			return true
		}
	}
	return false
}

// validateImportBindings checks cross-instance signatures and reference-store
// compatibility. Imported calls are already compiled; instantiation only writes
// concrete targets into the per-instance dispatch table.
func (c *Compiled) validateImportBindings(imports Imports, store *referenceStore) error {
	for i, key := range c.Imports {
		ex, ok := imports[key].(*InstanceExport)
		if !ok {
			continue
		}
		if ex == nil || ex.inst == nil {
			return fmt.Errorf("cross-instance import %q is nil", key)
		}
		if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
			return fmt.Errorf("cross-instance import %q references an unavailable function", key)
		}
		if i >= len(c.importFuncSigs) {
			return fmt.Errorf("cross-instance import %q is missing its signature", key)
		}
		sig := c.importFuncSigs[i]
		if len(sig.Params) != len(ex.params) || len(sig.Results) != len(ex.results) {
			return fmt.Errorf("cross-instance import %q signature mismatch", key)
		}
		for j := range sig.Params {
			if sig.Params[j] != ex.params[j] {
				return fmt.Errorf("cross-instance import %q signature mismatch", key)
			}
		}
		for j := range sig.Results {
			if sig.Results[j] != ex.results[j] {
				return fmt.Errorf("cross-instance import %q signature mismatch", key)
			}
		}
		if hasValType(sig.Params, ValExternRef) || hasValType(sig.Results, ValExternRef) {
			if store == nil || ex.inst.refStore != store {
				return fmt.Errorf("cross-instance externref import %q requires the same reference store", key)
			}
		}
	}
	return nil
}

func sigMatches(ft *wasm.CompType, ex *InstanceExport) bool {
	if len(ft.Params) != len(ex.params) || len(ft.Results) != len(ex.results) {
		return false
	}
	for i := range ft.Params {
		if valTypeFromWasm(ft.Params[i]) != ex.params[i] {
			return false
		}
	}
	for i := range ft.Results {
		if valTypeFromWasm(ft.Results[i]) != ex.results[i] {
			return false
		}
	}
	return true
}

func moduleSegmentStateCounts(m *wasm.Module) (elemCount, dataCount int) {
	for i := range m.Elements {
		if m.Elements[i].Mode.Kind == wasm.ElemPassive {
			elemCount = i + 1
		}
	}
	for i := range m.Data {
		if m.Data[i].Mode.Kind == wasm.DataPassive {
			dataCount = i + 1
		}
	}
	if elemCount == len(m.Elements) && dataCount == len(m.Data) {
		// Every declared index already has a passive descriptor slot (including
		// the common zero-segment case), so no instruction walk is needed.
		return elemCount, dataCount
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.BodyBytes) != 0 {
			if !bodyBytesSegmentStateCounts(fn.BodyBytes, &elemCount, &dataCount) {
				// Validation already walked this body successfully. If a later walker
				// nevertheless disagrees, reserve every declared slot rather than emit
				// code that can index beyond the runtime descriptor arrays.
				elemCount = len(m.Elements)
				dataCount = len(m.Data)
			}
		} else {
			instrsSegmentStateCounts(fn.Body.Instrs, &elemCount, &dataCount)
		}
	}
	return elemCount, dataCount
}

func bodyBytesSegmentStateCounts(body []byte, elemCount, dataCount *int) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false
		}
		segmentStateCount(imm.Kind, imm.Index, elemCount, dataCount)
	}
	return true
}

func instrsSegmentStateCounts(instrs []wasm.Instruction, elemCount, dataCount *int) {
	for i := range instrs {
		in := &instrs[i]
		segmentStateCount(in.Kind, in.Index, elemCount, dataCount)
		instrsSegmentStateCounts(in.Body().Instrs, elemCount, dataCount)
		instrsSegmentStateCounts(in.Then(), elemCount, dataCount)
		instrsSegmentStateCounts(in.Else(), elemCount, dataCount)
	}
}

func segmentStateCount(kind wasm.InstrKind, index uint32, elemCount, dataCount *int) {
	count := int(index) + 1
	switch kind {
	case wasm.InstrTableInit, wasm.InstrElemDrop:
		if count > *elemCount {
			*elemCount = count
		}
	case wasm.InstrMemoryInit, wasm.InstrDataDrop:
		if count > *dataCount {
			*dataCount = count
		}
	}
}

func moduleUsesMemoryGrow(m *wasm.Module) bool {
	for i := range m.Code {
		fn := &m.Code[i]
		// Byte-backed decode keeps function bodies as raw bytecode and leaves
		// Body.Instrs empty, so walk the encoded stream when present and only fall
		// back to the instruction tree for programmatically built bodies.
		if len(fn.BodyBytes) != 0 {
			if bodyBytesUseMemoryGrow(fn.BodyBytes) {
				return true
			}
			continue
		}
		if instrsUseMemoryGrow(fn.Body.Instrs) {
			return true
		}
	}
	return false
}

// bodyBytesUseMemoryGrow reports whether a validated, byte-backed function body
// contains a memory.grow. The body is already validated, so a decode hiccup is
// not expected; if one occurs it conservatively returns true so the caller does
// not pin the memory reservation to its minimum size and break memory.grow.
func bodyBytesUseMemoryGrow(body []byte) bool {
	r := wasm.NewReader(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return true
		}
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return true
		}
		if imm.Kind == wasm.InstrMemoryGrow {
			return true
		}
	}
	return false
}

func instrsUseMemoryGrow(instrs []wasm.Instruction) bool {
	for i := range instrs {
		in := &instrs[i]
		if in.Kind == wasm.InstrMemoryGrow {
			return true
		}
		if instrsUseMemoryGrow(in.Body().Instrs) || instrsUseMemoryGrow(in.Then()) || instrsUseMemoryGrow(in.Else()) {
			return true
		}
	}
	return false
}

// MustCompile is like Compile with the default config but panics on error, for
// tests, examples, and package-level initialization.
func MustCompile(wasmBytes []byte) *Compiled {
	c, err := Compile(nil, wasmBytes)
	if err != nil {
		panic("wago: MustCompile: " + err.Error())
	}
	return c
}

// ExportedFunctions returns the names of the module's exported functions, sorted.
func (c *Compiled) ExportedFunctions() []string { return sortedKeys(c.Exports) }

// ExportedGlobals returns the names of the module's exported globals, sorted.
func (c *Compiled) ExportedGlobals() []string { return sortedKeys(c.GlobalExports) }

// MemoryImport returns the "module.name" key of the module's imported memory, if
// it imports one; Instantiate then requires a *Memory for that key. The boolean
// is false for a module that defines its own memory or none.
func (c *Compiled) MemoryImport() (string, bool) {
	return c.memoryImport, c.memoryImport != ""
}

// TableImport returns the "module.name" key when the module imports exactly one
// table. Instantiate then requires a *Table for that key. Modules with zero or
// multiple table imports return false; use TableImports for the complete list.
func (c *Compiled) TableImport() (string, bool) {
	if c == nil {
		return "", false
	}
	if c.tableImportCount() != 1 {
		return "", false
	}
	return c.tableImport, true
}

// TableImports returns every imported table key in Wasm table-index order.
// Duplicate keys are preserved because two declarations may intentionally alias
// the same shared table object.
func (c *Compiled) TableImports() []string {
	if c == nil {
		return nil
	}
	count := c.tableImportCount()
	if count == 0 {
		return nil
	}
	keys := make([]string, count)
	for i := range keys {
		def, _ := c.tableImportAt(i)
		keys[i] = def.Key
	}
	return keys
}

func sortedKeys(m map[string]int) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Signature returns the parameter and result types of an exported function.
func (c *Compiled) Signature(export string) (params, results []ValType, err error) {
	if c == nil {
		return nil, nil, fmt.Errorf("compiled module is nil")
	}
	gfi, ok := c.Exports[export]
	if !ok {
		return nil, nil, fmt.Errorf("no exported function %q", export)
	}
	if gfi < 0 {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if gfi < c.NumImports {
		if gfi >= len(c.importFuncSigs) {
			return nil, nil, fmt.Errorf("export %q imported function index %d has no signature", export, gfi)
		}
		sig := c.importFuncSigs[gfi]
		return sig.Params, sig.Results, nil
	}
	li := gfi - c.NumImports
	if li < 0 || li >= len(c.Funcs) {
		return nil, nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	return c.Funcs[li].Params, c.Funcs[li].Results, nil
}

// FuncName returns the name-section name for a global function index.
func (c *Compiled) FuncName(funcIdx uint32) (string, bool) {
	if c == nil || c.Names == nil {
		return "", false
	}
	return c.Names.FuncName(funcIdx)
}

// LocalFuncName returns the name-section name for a locally-defined function
// index (that is, an index into Compiled.Funcs rather than wasm's global
// function-index space).
func (c *Compiled) LocalFuncName(localIdx int) (string, bool) {
	if c == nil || localIdx < 0 {
		return "", false
	}
	return c.FuncName(uint32(localIdx + c.NumImports))
}

// FuncDebugName returns a stable display name for a global function index,
// preferring the wasm name section and falling back to exports or funcN.
func (c *Compiled) FuncDebugName(funcIdx uint32) string {
	if name, ok := c.FuncName(funcIdx); ok && name != "" {
		return name
	}
	if c != nil {
		var exports []string
		for name, idx := range c.Exports {
			if idx == int(funcIdx) {
				exports = append(exports, name)
			}
		}
		if len(exports) > 0 {
			sort.Strings(exports)
			return exports[0]
		}
	}
	return fmt.Sprintf("func%d", funcIdx)
}

func (c *Compiled) validate() error {
	if c == nil {
		return fmt.Errorf("compiled module is nil")
	}
	if c.NumImports < 0 {
		return fmt.Errorf("compiled metadata invalid: negative NumImports %d", c.NumImports)
	}
	if len(c.Imports) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: Imports length %d != NumImports %d", len(c.Imports), c.NumImports)
	}
	if len(c.importFuncSigs) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: importFuncSigs length %d != NumImports %d", len(c.importFuncSigs), c.NumImports)
	}
	if c.dynamicImports != (c.NumImports > 0) {
		return fmt.Errorf("compiled metadata invalid: dynamic import dispatch=%v with %d function import(s)", c.dynamicImports, c.NumImports)
	}
	if c.NumImports > maxInt()-len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: function count overflows int")
	}
	required := CoreFeatures(c.requiredFeatures)
	if required&^coreFeaturesWago != 0 {
		return fmt.Errorf("compiled metadata invalid: unknown required feature bits 0x%x", uint64(required&^coreFeaturesWago))
	}
	if c.memoryImport != "" && !c.HasMemory {
		return fmt.Errorf("compiled metadata invalid: memory import without memory limits")
	}
	if c.HasMemory {
		if c.MemMaxPages < c.MemMinPages {
			return fmt.Errorf("compiled metadata invalid: memory max %d < min %d", c.MemMaxPages, c.MemMinPages)
		}
	} else if c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax {
		return fmt.Errorf("compiled metadata invalid: memory limits without memory")
	}
	if c.TableSize < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableSize %d", c.TableSize)
	}
	if c.TableMax < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableMax %d", c.TableMax)
	}
	if c.TableMax != 0 && c.TableMax < c.TableSize {
		return fmt.Errorf("compiled metadata invalid: TableMax %d < TableSize %d", c.TableMax, c.TableSize)
	}
	if len(c.extraTables) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d extra table(s) without table 0", len(c.extraTables))
	}
	if c.HasTable && c.TableType != 0 && c.TableType != ValFuncRef && c.TableType != ValExternRef {
		return fmt.Errorf("compiled metadata invalid: table 0 element type %s is unsupported", c.TableType)
	}
	for i, table := range c.extraTables {
		if table.Size < 0 || table.Max < 0 {
			return fmt.Errorf("compiled metadata invalid: negative table %d limits", i+1)
		}
		if table.Max != 0 && table.Max < table.Size {
			return fmt.Errorf("compiled metadata invalid: table %d maximum %d < size %d", i+1, table.Max, table.Size)
		}
		if table.Type != 0 && table.Type != ValFuncRef && table.Type != ValExternRef {
			return fmt.Errorf("compiled metadata invalid: table %d element type %s is unsupported", i+1, table.Type)
		}
	}
	if !c.HasTable && c.TableSize != 0 {
		return fmt.Errorf("compiled metadata invalid: TableSize %d without table", c.TableSize)
	}
	if !c.HasTable && c.TableMax != 0 {
		return fmt.Errorf("compiled metadata invalid: TableMax %d without table", c.TableMax)
	}
	if !c.HasTable && c.tableImport != "" {
		return fmt.Errorf("compiled metadata invalid: table import %q without table", c.tableImport)
	}
	if c.tableImport == "" {
		if c.tableImportMin != 0 || c.tableImportMax != 0 || c.tableImportHasMax {
			return fmt.Errorf("compiled metadata invalid: table import limits without table import")
		}
		if c.TableHasMax && !c.HasTable {
			return fmt.Errorf("compiled metadata invalid: table maximum without table")
		}
	} else {
		if c.TableHasMax {
			return fmt.Errorf("compiled metadata invalid: local table maximum flag on imported table 0")
		}
		if c.TableSize != 0 || c.TableMax != 0 {
			return fmt.Errorf("compiled metadata invalid: local table limits present on imported table")
		}
		if c.tableImportMin < 0 || c.tableImportMax < 0 {
			return fmt.Errorf("compiled metadata invalid: negative imported table limit")
		}
		if !c.tableImportHasMax && c.tableImportMax != 0 {
			return fmt.Errorf("compiled metadata invalid: imported table max without max flag")
		}
		if c.tableImportHasMax && c.tableImportMax < c.tableImportMin {
			return fmt.Errorf("compiled metadata invalid: imported table max %d < min %d", c.tableImportMax, c.tableImportMin)
		}
	}
	seenLocalTable := false
	for i, table := range c.extraTables {
		index := i + 1
		if table.ImportKey == "" {
			seenLocalTable = true
			if table.ImportHasMax {
				return fmt.Errorf("compiled metadata invalid: table %d import max flag without import key", index)
			}
			continue
		}
		if c.tableImport == "" {
			return fmt.Errorf("compiled metadata invalid: imported table %d without imported table 0", index)
		}
		if seenLocalTable {
			return fmt.Errorf("compiled metadata invalid: imported table %d follows a local table", index)
		}
		if table.HasInitFunc {
			return fmt.Errorf("compiled metadata invalid: initializer on imported table %d", index)
		}
		if table.HasMax {
			return fmt.Errorf("compiled metadata invalid: local max flag on imported table %d", index)
		}
		if !table.ImportHasMax && table.Max != 0 {
			return fmt.Errorf("compiled metadata invalid: imported table %d max without max flag", index)
		}
		if table.ImportHasMax && table.Max < table.Size {
			return fmt.Errorf("compiled metadata invalid: imported table %d max %d < min %d", index, table.Max, table.Size)
		}
	}
	if len(c.Elems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d element segment(s) without table", len(c.Elems))
	}
	if len(c.Entry) != len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: Entry length %d != Funcs length %d", len(c.Entry), len(c.Funcs))
	}
	for i, off := range c.Entry {
		if off < 0 || off >= len(c.Code) {
			return fmt.Errorf("compiled metadata invalid: Entry[%d] offset %d out of code range %d", i, off, len(c.Code))
		}
	}
	totalFuncs := c.NumImports + len(c.Funcs)
	if len(c.FuncTypeID) != totalFuncs {
		return fmt.Errorf("compiled metadata invalid: FuncTypeID length %d != function count %d", len(c.FuncTypeID), totalFuncs)
	}
	if c.HasTableInitFunc {
		if !c.HasTable {
			return fmt.Errorf("compiled metadata invalid: table initializer without table")
		}
		if c.tableImport != "" {
			return fmt.Errorf("compiled metadata invalid: table initializer on imported table")
		}
		if uint64(c.TableInitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table initializer function index %d out of range", c.TableInitFunc)
		}
	}
	for i, table := range c.extraTables {
		if table.HasInitFunc && uint64(table.InitFunc) >= uint64(totalFuncs) {
			return fmt.Errorf("compiled metadata invalid: table %d initializer function index %d out of range", i+1, table.InitFunc)
		}
	}
	for name, gfi := range c.Exports {
		if gfi < 0 || gfi >= totalFuncs {
			return fmt.Errorf("compiled metadata invalid: function export %q index %d out of range", name, gfi)
		}
	}
	if len(c.tableExports) != 0 && !c.hasTableExportMetadata {
		return fmt.Errorf("compiled metadata invalid: table/memory exports without exact export metadata marker")
	}
	for name, tableIndex := range c.tableExports {
		if tableIndex == memoryExportSentinel {
			if !c.HasMemory {
				return fmt.Errorf("compiled metadata invalid: memory export %q without memory", name)
			}
			continue
		}
		if tableIndex < 0 || tableIndex >= c.tableCount() {
			return fmt.Errorf("compiled metadata invalid: table export %q index %d out of range", name, tableIndex)
		}
	}
	if err := c.validateRuntimeReferenceGlobalMetadata(); err != nil {
		return err
	}
	if len(c.GlobalImports) > len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: GlobalImports length %d > Globals length %d", len(c.GlobalImports), len(c.Globals))
	}
	for i, imp := range c.GlobalImports {
		g := c.Globals[i]
		if g.Type != imp.Type || g.Mutable != imp.Mutable {
			return fmt.Errorf("compiled metadata invalid: imported global %d metadata mismatch", i)
		}
	}
	for name, idx := range c.GlobalExports {
		if idx < 0 || idx >= len(c.Globals) {
			return fmt.Errorf("compiled metadata invalid: global export %q index %d out of range", name, idx)
		}
	}
	for i, g := range c.Globals {
		initializerKinds := 0
		if g.HasInitGlobal {
			initializerKinds++
		}
		if g.HasInitFunc {
			initializerKinds++
		}
		if len(g.InitExpr) != 0 {
			initializerKinds++
		}
		if initializerKinds > 1 {
			return fmt.Errorf("compiled metadata invalid: global %d has multiple initializer references", i)
		}
		if len(g.InitExpr) != 0 {
			if err := validateCompiledConstExpr(g.InitExpr, g.Type, c); err != nil {
				return fmt.Errorf("compiled metadata invalid: global %d extended initializer: %w", i, err)
			}
		}
		if g.HasInitGlobal {
			if g.InitGlobal < 0 || g.InitGlobal >= i || g.InitGlobal >= len(c.Globals) {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references unavailable global %d", i, g.InitGlobal)
			}
			src := c.Globals[g.InitGlobal]
			if g.InitGlobal >= len(c.GlobalImports) || src.Mutable {
				return fmt.Errorf("compiled metadata invalid: global %d initializer references non-imported or mutable global %d", i, g.InitGlobal)
			}
			if src.Type != g.Type {
				return fmt.Errorf("compiled metadata invalid: global %d initializer type %s != source global %d type %s", i, g.Type, g.InitGlobal, src.Type)
			}
		}
		if g.HasInitFunc {
			if g.Type != ValFuncRef {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer has type %s", i, g.Type)
			}
			if uint64(g.InitFunc) >= uint64(totalFuncs) {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer index %d out of range", i, g.InitFunc)
			}
			if !c.needsFuncRefDescs() {
				return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer without descriptor arena", i)
			}
		}
	}
	validateElementValues := func(kind string, seg int, refType ValType, values []RefInit) error {
		if refType != ValFuncRef && refType != ValExternRef {
			return fmt.Errorf("compiled metadata invalid: %s element %d has unsupported reference type %s", kind, seg, refType)
		}
		for k, value := range values {
			if value.Null {
				if value.HasGlobal {
					return fmt.Errorf("compiled metadata invalid: %s element %d value %d has both null and global initializers", kind, seg, k)
				}
				continue
			}
			if value.HasGlobal {
				if err := c.validateElementGlobalInitializer(kind, seg, k, refType, value.GlobalIndex); err != nil {
					return err
				}
				continue
			}
			if refType != ValFuncRef {
				return fmt.Errorf("compiled metadata invalid: %s element %d value %d is non-null %s", kind, seg, k, refType)
			}
			if int(value.FuncIndex) >= totalFuncs {
				return fmt.Errorf("compiled metadata invalid: %s element %d function %d index %d out of range", kind, seg, k, value.FuncIndex)
			}
		}
		return nil
	}
	for seg, el := range c.Elems {
		refType := normalizedElemRefType(el.RefType)
		if el.Mode != ElemModeActive {
			return fmt.Errorf("compiled metadata invalid: active element %d has mode %d", seg, el.Mode)
		}
		if uint64(el.TableIndex) >= uint64(c.tableCount()) {
			return fmt.Errorf("compiled metadata invalid: active element %d table index %d out of range", seg, el.TableIndex)
		}
		if c.tableElementType(int(el.TableIndex)) != refType {
			return fmt.Errorf("compiled metadata invalid: active element %d type %s does not match table %d type %s", seg, refType, el.TableIndex, c.tableElementType(int(el.TableIndex)))
		}
		if el.Offset.HasGlobal && len(el.Offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: element %d has multiple offset initializer forms", seg)
		}
		if el.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("element", seg, el.Offset.Global); err != nil {
				return err
			}
		}
		if len(el.Offset.Expr) != 0 {
			if err := validateCompiledConstExpr(el.Offset.Expr, ValI32, c); err != nil {
				return fmt.Errorf("compiled metadata invalid: element %d extended offset: %w", seg, err)
			}
		}
		if err := validateElementValues("active", seg, refType, el.Values); err != nil {
			return err
		}
	}
	for seg, el := range c.passiveElems {
		refType := normalizedElemRefType(el.RefType)
		mode := el.Mode
		if mode == ElemModeActive || mode == ElemModeDeclarative {
			if len(el.Values) != 0 {
				return fmt.Errorf("compiled metadata invalid: dropped element %d retains %d value(s)", seg, len(el.Values))
			}
			if mode == ElemModeActive && uint64(el.TableIndex) >= uint64(c.tableCount()) {
				return fmt.Errorf("compiled metadata invalid: dropped active element %d table index %d out of range", seg, el.TableIndex)
			}
		} else if mode != ElemModePassive {
			return fmt.Errorf("compiled metadata invalid: element-state slot %d has mode %d", seg, mode)
		}
		if err := validateElementValues("element-state", seg, refType, el.Values); err != nil {
			return err
		}
	}
	for seg, d := range c.Data {
		if d.Offset.HasGlobal && len(d.Offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: data %d has multiple offset initializer forms", seg)
		}
		if d.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("data", seg, d.Offset.Global); err != nil {
				return err
			}
		}
		if len(d.Offset.Expr) != 0 {
			if err := validateCompiledConstExpr(d.Offset.Expr, ValI32, c); err != nil {
				return fmt.Errorf("compiled metadata invalid: data %d extended offset: %w", seg, err)
			}
		}
	}
	for seg, d := range c.PassiveData {
		if uint64(len(d.Bytes)) > uint64(^uint32(0)) {
			return fmt.Errorf("compiled metadata invalid: passive data %d length %d overflows descriptor", seg, len(d.Bytes))
		}
	}
	if err := gc.ValidateTypeDescs(c.GCTypeDescs); err != nil {
		return fmt.Errorf("compiled metadata invalid: GCTypeDescs: %w", err)
	}
	if err := c.validateArenaFootprint(); err != nil {
		return err
	}
	return nil
}

func (c *Compiled) validateRuntimeReferenceGlobalMetadata() error {
	for i, g := range c.Globals {
		if g.Type == ValExternRef && g.Bits != 0 {
			return fmt.Errorf("compiled metadata invalid: non-null externref global initializer at global %d is unsupported", i)
		}
		if g.Type == ValFuncRef && g.Bits != 0 {
			return fmt.Errorf("compiled metadata invalid: non-structural funcref global initializer at global %d is unsupported", i)
		}
	}
	return nil
}

func (c *Compiled) validateCodecV23Metadata() error {
	if unsupported := compiledStructuralRequiredFeatures(c) &^ coreFeaturesWago; unsupported != 0 {
		return fmt.Errorf("compiled metadata invalid: unknown required feature bits 0x%x", uint64(unsupported))
	}
	if err := c.validateRuntimeReferenceGlobalMetadata(); err != nil {
		return err
	}
	for i, g := range c.Globals {
		initializerKinds := 0
		if g.HasInitGlobal {
			initializerKinds++
		}
		if g.HasInitFunc {
			initializerKinds++
		}
		if len(g.InitExpr) != 0 {
			initializerKinds++
		}
		if initializerKinds > 1 {
			return fmt.Errorf("compiled metadata invalid: global %d has multiple initializer references", i)
		}
		if len(g.InitExpr) != 0 {
			if err := validateCompiledConstExpr(g.InitExpr, g.Type, c); err != nil {
				return fmt.Errorf("compiled metadata invalid: global %d extended initializer: %w", i, err)
			}
		}
		if g.HasInitFunc && g.Type != ValFuncRef {
			return fmt.Errorf("compiled metadata invalid: global %d ref.func initializer has type %s", i, g.Type)
		}
	}
	for name, tableIndex := range c.tableExports {
		if tableIndex == memoryExportSentinel {
			if !c.HasMemory {
				return fmt.Errorf("compiled metadata invalid: memory export %q without memory", name)
			}
			continue
		}
		if tableIndex < 0 || tableIndex >= c.tableCount() {
			return fmt.Errorf("compiled metadata invalid: table export %q index %d out of range", name, tableIndex)
		}
	}
	checkElems := func(kind string, elems []ElemInit) error {
		for i, elem := range elems {
			if elem.Offset.HasGlobal && len(elem.Offset.Expr) != 0 {
				return fmt.Errorf("compiled metadata invalid: %s element %d has multiple offset initializer forms", kind, i)
			}
			if len(elem.Offset.Expr) != 0 {
				if err := validateCompiledConstExpr(elem.Offset.Expr, ValI32, c); err != nil {
					return fmt.Errorf("compiled metadata invalid: %s element %d extended offset: %w", kind, i, err)
				}
			}
			refType := normalizedElemRefType(elem.RefType)
			for j, value := range elem.Values {
				if value.HasGlobal {
					if err := c.validateElementGlobalInitializer(kind, i, j, refType, value.GlobalIndex); err != nil {
						return err
					}
					continue
				}
				if !value.Null && refType != ValFuncRef {
					return fmt.Errorf("compiled metadata invalid: %s element %d value %d is non-null %s", kind, i, j, refType)
				}
			}
		}
		return nil
	}
	if err := checkElems("active", c.Elems); err != nil {
		return err
	}
	if err := checkElems("element-state", c.passiveElems); err != nil {
		return err
	}
	for i, data := range c.Data {
		if data.Offset.HasGlobal && len(data.Offset.Expr) != 0 {
			return fmt.Errorf("compiled metadata invalid: data %d has multiple offset initializer forms", i)
		}
		if len(data.Offset.Expr) != 0 {
			if err := validateCompiledConstExpr(data.Offset.Expr, ValI32, c); err != nil {
				return fmt.Errorf("compiled metadata invalid: data %d extended offset: %w", i, err)
			}
		}
	}
	return nil
}

func (c *Compiled) validateElementGlobalInitializer(kind string, seg, valueIndex int, refType ValType, globalIndex uint32) error {
	if uint64(globalIndex) >= uint64(len(c.Globals)) {
		return fmt.Errorf("compiled metadata invalid: %s element %d global %d index %d out of range", kind, seg, valueIndex, globalIndex)
	}
	source := c.Globals[globalIndex]
	if uint64(globalIndex) >= uint64(len(c.GlobalImports)) || source.Mutable {
		return fmt.Errorf("compiled metadata invalid: %s element %d global %d must reference an immutable imported global", kind, seg, globalIndex)
	}
	if source.Type != refType {
		return fmt.Errorf("compiled metadata invalid: %s element %d global %d type %s does not match %s", kind, seg, globalIndex, source.Type, refType)
	}
	return nil
}

func (c *Compiled) validateSnapshotReferenceGlobals() error {
	for i, g := range c.GlobalImports {
		if isReferenceValType(g.Type) {
			return fmt.Errorf("snapshot reference global metadata at import %d is unsupported until a live-state resolver exists", i)
		}
	}
	for i, g := range c.Globals {
		if isReferenceValType(g.Type) {
			return fmt.Errorf("snapshot reference global metadata at global %d is unsupported until a live-state resolver exists", i)
		}
	}
	return nil
}

func maxInt() int { return int(^uint(0) >> 1) }

func valTypeSlots(t ValType) int {
	if t == ValV128 {
		return 2
	}
	return 1
}

func valTypesSlots(ts []ValType) (int, error) {
	n := 0
	for _, t := range ts {
		s := valTypeSlots(t)
		if n > maxInt()-s {
			return 0, fmt.Errorf("value slot count overflows int")
		}
		n += s
	}
	return n, nil
}

func (c *Compiled) needsFuncRefDescs() bool {
	return c.NeedsFuncRefDescs || c.hasFuncrefTable()
}

func normalizedElemRefType(t ValType) ValType {
	if t == ValExternRef {
		return ValExternRef
	}
	return ValFuncRef
}

func normalizedTableElementType(t ValType) ValType {
	if t == ValExternRef {
		return ValExternRef
	}
	return ValFuncRef
}

func (c *Compiled) tableElementType(index int) ValType {
	if index == 0 {
		return normalizedTableElementType(c.TableType)
	}
	return normalizedTableElementType(c.extraTables[index-1].Type)
}

func (c *Compiled) tableEntryBytes(index int) int {
	if c.tableElementType(index) == ValExternRef {
		return 8
	}
	return wruntime.TableEntryBytes
}

func (c *Compiled) hasFuncrefTable() bool {
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValFuncRef {
			return true
		}
	}
	return false
}

func (c *Compiled) hasExternrefTable() bool {
	for i := 0; i < c.tableCount(); i++ {
		if c.tableElementType(i) == ValExternRef {
			return true
		}
	}
	return false
}

func (c *Compiled) tableCount() int {
	if !c.HasTable {
		return 0
	}
	return 1 + len(c.extraTables)
}

func (c *Compiled) tableImportCount() int {
	if c == nil || c.tableImport == "" {
		return 0
	}
	count := 1
	for i := range c.extraTables {
		if c.extraTables[i].ImportKey == "" {
			break
		}
		count++
	}
	return count
}

func (c *Compiled) tableImportAt(index int) (tableImportDef, bool) {
	if c == nil || index < 0 {
		return tableImportDef{}, false
	}
	if index == 0 && c.tableImport != "" {
		return tableImportDef{Key: c.tableImport, Min: c.tableImportMin, Max: c.tableImportMax, Type: c.tableElementType(0), HasMax: c.tableImportHasMax}, true
	}
	if index > 0 && index-1 < len(c.extraTables) {
		table := c.extraTables[index-1]
		if table.ImportKey != "" {
			return tableImportDef{Key: table.ImportKey, Min: table.Size, Max: table.Max, Type: c.tableElementType(index), HasMax: table.ImportHasMax}, true
		}
	}
	return tableImportDef{}, false
}

func (c *Compiled) tableDef(index int) tableDef {
	if index == 0 {
		return tableDef{Size: c.TableSize, Max: c.TableMax, Type: c.TableType, HasInitFunc: c.HasTableInitFunc, HasMax: c.TableHasMax, InitFunc: c.TableInitFunc}
	}
	return c.extraTables[index-1]
}

func (c *Compiled) tableMinimum(index int) int {
	if def, ok := c.tableImportAt(index); ok {
		return def.Min
	}
	return c.tableDef(index).Size
}

func (c *Compiled) validateArenaFootprint() error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	funcRefCount := 0
	if c.needsFuncRefDescs() {
		funcRefCount = len(c.FuncTypeID) + 1
	}
	tableSize, tableCapacity := c.TableSize, c.TableMax
	var tableCaps []int
	var tableEntryBytes []int
	if c.HasTable {
		tableEntryBytes = make([]int, c.tableCount())
		for i := range tableEntryBytes {
			tableEntryBytes[i] = c.tableEntryBytes(i)
		}
	}
	if len(c.extraTables) != 0 {
		tableSize, tableCapacity = 0, 0
		tableCaps = make([]int, c.tableCount())
		for i := range tableCaps {
			def := c.tableDef(i)
			tableCaps[i] = def.Max
			if tableCaps[i] == 0 {
				tableCaps[i] = def.Size
			}
		}
	}
	passiveElemBytes := 0
	for i, elem := range c.passiveElems {
		stride := wruntime.TableEntryBytes
		if normalizedElemRefType(elem.RefType) == ValExternRef {
			stride = 8
		}
		if len(elem.Values) > (maxInt()-passiveElemBytes)/stride {
			return fmt.Errorf("compiled metadata invalid: passive element %d payload overflows arena allocation", i)
		}
		passiveElemBytes += len(elem.Values) * stride
	}
	hostCallBytes := 0
	if c.needsPublicFuncrefHostReentry() {
		hostCallBytes = wruntime.HostCtrlFrameBytes
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		FuncImportCount:    len(c.Imports),
		HostCallBytes:      hostCallBytes,
		FuncRefCount:       funcRefCount,
		GlobalCount:        len(c.Globals),
		HasTable:           c.HasTable,
		TableSize:          tableSize,
		TableCapacity:      tableCapacity,
		TableCapacities:    tableCaps,
		TableEntryBytes:    tableEntryBytes,
		ImportedTableCount: c.tableImportCount(),
		ElemCount:          len(c.Elems),
		PassiveElemCount:   len(c.passiveElems),
		PassiveElemBytes:   passiveElemBytes,
		PassiveDataCount:   len(c.PassiveData),
		MaxParamSlots:      maxParams,
		MaxResultSlots:     maxResults,
	})
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	if need > maxInt()-wruntime.InstanceContextBytes {
		return fmt.Errorf("compiled metadata invalid: instantiate arena need overflows instance context")
	}
	need += wruntime.InstanceContextBytes
	if need > wruntime.InstantiateArenaSize {
		return fmt.Errorf("compiled metadata invalid: instantiate arena need %d > limit %d", need, wruntime.InstantiateArenaSize)
	}
	c.maxParamSlots = maxParams
	c.maxResultSlots = maxResults
	c.instantiateArenaNeed = need
	return nil
}

func (c *Compiled) maxCallSlots() (params, results int, err error) {
	for i, fn := range c.Funcs {
		paramSlots, err := valTypesSlots(fn.Params)
		if err != nil || paramSlots > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d parameter slots overflow call buffer", i)
		}
		resultSlots, err := valTypesSlots(fn.Results)
		if err != nil || resultSlots > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d result slots overflow call buffer", i)
		}
		if paramSlots > params {
			params = paramSlots
		}
		if resultSlots > results {
			results = resultSlots
		}
	}
	return params, results, nil
}

func (c *Compiled) validateDeferredOffsetGlobal(kind string, seg, idx int) error {
	if idx < 0 || idx >= len(c.Globals) {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d out of range", kind, seg, idx)
	}
	g := c.Globals[idx]
	if idx >= len(c.GlobalImports) || g.Mutable || g.Type != ValI32 {
		return fmt.Errorf("compiled metadata invalid: %s %d offset global %d must be imported immutable i32", kind, seg, idx)
	}
	return nil
}

const wagoMagic = "WAGO"

// Version 23 adds extended constant-expression metadata to the imported-global
// element initializer, binding-independent imported-call, structural-reference,
// indexed-table, typed-element, and feature data. It never serializes live reference tokens,
// target addresses, thunk addresses, owners, or store identity.
const wagoVersion = 23

// MarshalBinary serializes the precompiled module to a ".wago" blob.
//
// Signals-based (guard-page) modules cannot be serialized: their code has the
// inline bounds checks elided and is only safe against a guard-page memory,
// which a loaded blob has no way to record. Recompile from wasm with the desired
// config at load time instead.
func (c *Compiled) MarshalBinary() ([]byte, error) {
	if c.boundsMode == BoundsChecksSignalsBased {
		return nil, errors.New("wago: signals-based compiled modules cannot be serialized; recompile from wasm at load time")
	}
	if c.requiresAVX2 || c.requiresAVX512 {
		return nil, errors.New("wago: wide-SIMD plugin code cannot be serialized until the artifact format records CPU requirements")
	}
	if len(c.Entry) == 0 && len(c.Funcs) > 0 {
		return nil, errors.New("wago: compiled module has functions but no native entries")
	}
	if c.NumImports > 0 && !c.dynamicImports {
		return nil, errors.New("wago: imported-function code lacks dynamic dispatch metadata")
	}
	if err := c.validateCodecV23Metadata(); err != nil {
		return nil, err
	}
	return marshalCompiled(c)
}

// UnmarshalBinary loads a ".wago" blob produced by MarshalBinary.
func (c *Compiled) UnmarshalBinary(data []byte) error {
	if !IsCompiled(data) {
		return fmt.Errorf("not a wago module")
	}
	if data[4] != wagoVersion {
		return fmt.Errorf("wago module version %d unsupported (want %d)", data[4], wagoVersion)
	}
	var decoded Compiled
	if err := unmarshalCompiled(&decoded, data[5:]); err != nil {
		return err
	}
	if len(decoded.tableExports) == 0 {
		decoded.tableExports = nil
	}
	decoded.hasTableExportMetadata = true
	if inferred := compiledStructuralRequiredFeatures(&decoded); inferred&^CoreFeatures(decoded.requiredFeatures) != 0 {
		return fmt.Errorf("compiled metadata invalid: structural metadata requires unrecorded features %s", inferred&^CoreFeatures(decoded.requiredFeatures))
	}
	if err := decoded.validate(); err != nil {
		return err
	}
	if CoreFeatures(decoded.requiredFeatures).IsEnabled(CoreFeatureSIMD) && !hostSupportsSIMD() {
		return fmt.Errorf("wago: compiled module requires SIMD CPU features unavailable on this host")
	}
	*c = decoded
	installCompiledFinalizer(c)
	return nil
}

// IsCompiled reports whether b is a precompiled wago module (vs raw wasm).
func IsCompiled(b []byte) bool { return len(b) >= 5 && string(b[:4]) == wagoMagic }

// Load returns a *Compiled from either a precompiled ".wago" blob or raw wasm
// (which it compiles).
func Load(b []byte) (*Compiled, error) {
	if IsCompiled(b) {
		c := &Compiled{}
		return c, c.UnmarshalBinary(b)
	}
	return Compile(nil, b)
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper
// call. The returned slice is backed by an instance-owned buffer and stays valid
// only until the next call on this Instance; copy it if you need to retain it.
// Invoke calls an exported function. Arguments and results are raw uint64 slots
// interpreted per the function's signature (encode/decode scalar slots with
// I32/I64/F32/F64 and AsI32/AsI64/AsF32/AsF64). A v128 occupies two adjacent
// little-endian uint64 slots in the argument and result slices. Public funcref
// slots use opaque store-owned tokens: zero is null, and nonzero tokens are valid
// only in the Runtime store (or standalone private store) that issued them.
func (in *Instance) Invoke(export string, args ...uint64) ([]uint64, error) {
	return in.invoke(export, args, nil)
}

// InvokeContext is like Invoke but honors context cancellation. If ctx is
// cancelled or its deadline expires while the guest is executing, the call is
// interrupted at the next native safepoint and returns ctx.Err() (e.g.
// context.DeadlineExceeded) instead of blocking on a runaway guest.
//
// Native cancellation is available on amd64/arm64; on other architectures ctx
// is only checked before the call begins. A nil or already-cancelled ctx is
// handled up front; a Background context (Done() == nil) keeps Invoke's
// zero-goroutine fast path.
//
// This is the raw-uint64 sibling of Call, for callers that invoke by slot
// values rather than typed Values (e.g. AssemblyScript rule dispatch).
func (in *Instance) InvokeContext(ctx context.Context, export string, args ...uint64) ([]uint64, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	var cancel <-chan struct{}
	if nativeCancellationSupported() && ctx != nil {
		cancel = ctx.Done()
	}

	out, err := in.invoke(export, args, cancel)

	return out, contextInterruptError(ctx, err)
}

func (in *Instance) invoke(export string, args []uint64, cancel <-chan struct{}) ([]uint64, error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("invoke %q: %w", export, err)
	}
	defer in.endInvocation()
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
			return nil, err
		}
	}
	li := ic.li
	if li < 0 {
		importIdx := -li - 1
		if importIdx < 0 || importIdx >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, importIdx)
		}
		ex, ok := in.imports[in.c.Imports[importIdx]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("export %q is an imported function without an InstanceExport owner", export)
		}
		// Native cross-instance calls copy the caller's trap cell into the callee.
		// This Go-level re-export path must preserve the same interruption owner.
		return ex.inst.invokeLocalContext(ex.localIdx, args, cancel, in.trap)
	}
	if len(args) != ic.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if ic.resultSlots > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, ic.resultSlots, len(in.results)/8)
	}
	if ic.hasFuncRefParams {
		if err := in.marshalPublicReferenceArgs(export, args, in.c.Funcs[li].Params); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, in.c.Funcs[li].Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	}
	entry := in.base + uintptr(in.c.Entry[li])
	stopCancel := noOpCancellationWatch
	if cancel != nil {
		stopCancel = in.startCancellationWatch(cancel, in.trap)
	}
	if in.syncMode {
		if err := in.callNativeSync(entry); err != nil {
			stopCancel()
			return nil, err
		}
	} else {
		if err := in.callNativeAsync(entry, false); err != nil {
			stopCancel()
			return nil, err
		}
		if err := in.replayHostLog(); err != nil {
			stopCancel()
			return nil, err
		}
	}
	stopCancel()
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	for i, wide := range ic.resultWide {
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("%s result slot %d exceeds instance result buffer", export, i)
		}
		if wide { // i64/f64 or one half of a v128
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else { // i32 / f32 (4-byte)
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	if ic.hasFuncRefResults {
		if err := in.translatePublicReferenceResults(export, out, in.c.Funcs[li].Results); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// invokeLocal calls this instance's local function `li` directly (bypassing the
// export-name cache). Used to call through a re-exported import into the instance
// that satisfies it. activeTrap is the outer caller's invocation cell, matching
// the native dynamic-call ABI so closing a re-exporting caller interrupts code
// executing in the producer. It shares the instance's call buffers, so the
// returned slice is valid only until the next call on this Instance.
func (in *Instance) invokeLocal(li int, args []uint64) ([]uint64, error) {
	return in.invokeLocalContext(li, args, nil, in.trap)
}

func (in *Instance) invokeLocalContext(li int, args []uint64, cancel <-chan struct{}, activeTrap []byte) ([]uint64, error) {
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("invoke function %d: %w", li, err)
	}
	defer in.endInvocation()
	if li < 0 || li >= len(in.c.Funcs) || li >= len(in.c.Entry) {
		return nil, fmt.Errorf("invalid function index %d", li)
	}
	sig := in.c.Funcs[li]
	paramSlots, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, fmt.Errorf("function parameter slots: %w", err)
	}
	resultSlots, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, fmt.Errorf("function result slots: %w", err)
	}
	if len(args) != paramSlots {
		return nil, fmt.Errorf("function expects %d arg slot(s), got %d", paramSlots, len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("requires %d arg slot(s), instance buffer has %d", len(args), len(in.serArgs)/8)
	}
	if resultSlots > len(in.results)/8 {
		return nil, fmt.Errorf("requires %d result slot(s), instance buffer has %d", resultSlots, len(in.results)/8)
	}
	if hasReferenceValType(sig.Params) {
		if err := in.marshalPublicReferenceArgs("function", args, sig.Params); err != nil {
			return nil, err
		}
	} else {
		marshalPublicScalarArgs(in.serArgs, args, sig.Params)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if len(activeTrap) < 4 {
		activeTrap = in.trap
	}
	stopCancel := noOpCancellationWatch
	if cancel != nil {
		stopCancel = in.startCancellationWatch(cancel, activeTrap)
	}
	if in.syncMode {
		if err := in.callNativeSyncWithTrap(entry, activeTrap); err != nil {
			stopCancel()
			return nil, err
		}
	} else {
		if err := in.callNativeAsyncWithTrap(entry, false, activeTrap); err != nil {
			stopCancel()
			return nil, err
		}
		if err := in.replayHostLog(); err != nil {
			stopCancel()
			return nil, err
		}
	}
	stopCancel()
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:resultSlots]
	resSlot := 0
	for _, rt := range sig.Results {
		if rt == ValV128 {
			for half := 0; half < 2; half++ {
				off := resSlot * 8
				if off+8 > len(in.results) {
					return nil, fmt.Errorf("result slot %d exceeds instance result buffer", resSlot)
				}
				out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
				resSlot++
			}
			continue
		}
		off := resSlot * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("result slot %d exceeds instance result buffer", resSlot)
		}
		if isWideValType(rt) {
			out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[resSlot] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
		resSlot++
	}
	if hasReferenceValType(sig.Results) {
		if err := in.translatePublicReferenceResults("function", out, sig.Results); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// nativeCancellationSupported reports whether the railshot backend for this
// GOARCH emits cooperative cancellation polls (function-entry and loop-header
// trap-cell checks). Both the amd64 and arm64 backends do, so a context-aware
// Call can arm the watcher on either.
func nativeCancellationSupported() bool {
	return goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64"
}

// startCancellationWatch arms the native safepoints for a high-level
// context-aware Call. Background contexts keep the zero-goroutine fast path.
func noOpCancellationWatch() {}

func (in *Instance) startCancellationWatch(cancel <-chan struct{}, activeTrap []byte) func() {
	if !nativeCancellationSupported() || cancel == nil || len(activeTrap) < 4 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	trap := (*uint32)(unsafe.Pointer(&activeTrap[0]))
	go func() {
		defer close(stopped)
		select {
		case <-done:
			return
		case <-cancel:
		}
		// Keep publishing until native code observes the request. This closes the
		// small arm-before-entry race where Engine.Call establishes a zero trap cell.
		for {
			atomic.StoreUint32(trap, uint32(wruntime.TrapInterrupted))
			select {
			case <-done:
				return
			default:
				goruntime.Gosched()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
		atomic.CompareAndSwapUint32(trap, uint32(wruntime.TrapInterrupted), 0)
	}
}

// replayHostLog runs the void host imports the last native call logged. Each
// logged entry carries the single i32 argument the codegen captured; it is passed
// to the stack-form HostFunc as params[0], with no results.
func (in *Instance) replayHostLog() (err error) {
	if len(in.hostLog) == 0 {
		return nil
	}
	// A replayed host call may panic(HostExit{...}) to end
	// execution — recover it as an *ExitError, exactly like the synchronous path.
	defer func() {
		if r := recover(); r != nil {
			if ex, ok := r.(HostExit); ok {
				err = &ExitError{Code: ex.Code}
				return
			}
			if missing, ok := r.(missingHostFunc); ok {
				err = fmt.Errorf("missing host function for import index %d", missing.importIdx)
				return
			}
			panic(r)
		}
	}()
	n := binary.LittleEndian.Uint32(in.hostLog)
	var params [1]uint64
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				params[0] = uint64(uint32(arg))
				if in.rt == nil || !in.rt.scopedHostCalls() {
					fn(staticHostModule{in: in}, params[:], nil)
					continue
				}
				caller := in.beginHostCallScope()
				func() {
					defer caller.scope.end(caller.generation)
					fn(caller, params[:], nil)
				}()
			}
		}
	}
	return nil
}

// fillInvokeCache resolves export and memoizes it so subsequent Invokes skip the
// exports map probe. Local functions store their local index; an imported
// InstanceExport stores -1-importIndex and forwards through its original owner.
func (in *Instance) fillInvokeCache(export string) (*invokeCache, error) {
	gfi, ok := in.c.Exports[export]
	if !ok {
		return nil, fmt.Errorf("no exported function %q", export)
	}
	if gfi < 0 {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	if gfi < in.c.NumImports {
		if gfi >= len(in.c.Imports) {
			return nil, fmt.Errorf("export %q imported function index %d has no binding", export, gfi)
		}
		ex, ok := in.imports[in.c.Imports[gfi]].(*InstanceExport)
		if !ok || ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("export %q is an imported function without an InstanceExport owner", export)
		}
		slot := &in.ic[int(in.icNext)%len(in.ic)]
		in.icNext++
		*slot = invokeCache{export: export, valid: true, li: -1 - gfi, resultWide: slot.resultWide[:0]}
		return slot, nil
	}
	li := gfi - in.c.NumImports
	if li < 0 || li >= len(in.c.Funcs) {
		return nil, fmt.Errorf("export %q function index %d out of range", export, gfi)
	}
	sig := in.c.Funcs[li]
	paramSlots, err := valTypesSlots(sig.Params)
	if err != nil {
		return nil, fmt.Errorf("%s parameter slots: %w", export, err)
	}
	resultSlots, err := valTypesSlots(sig.Results)
	if err != nil {
		return nil, fmt.Errorf("%s result slots: %w", export, err)
	}
	slot := &in.ic[int(in.icNext)%len(in.ic)]
	in.icNext++
	rw := slot.resultWide[:0]
	if cap(rw) < resultSlots {
		rw = make([]bool, 0, resultSlots)
	}
	for _, r := range sig.Results {
		if r == ValV128 {
			rw = append(rw, true, true)
		} else {
			rw = append(rw, isWideValType(r))
		}
	}
	*slot = invokeCache{
		export:            export,
		valid:             true,
		li:                li,
		paramSlots:        paramSlots,
		resultSlots:       resultSlots,
		hasFuncRefParams:  hasReferenceValType(sig.Params),
		hasFuncRefResults: hasReferenceValType(sig.Results),
		resultWide:        rw,
	}
	return slot, nil
}

func marshalPublicScalarArgs(dst []byte, values []uint64, types []ValType) {
	slot := 0
	for _, typ := range types {
		if typ == ValV128 {
			binary.LittleEndian.PutUint64(dst[slot*8:], values[slot])
			binary.LittleEndian.PutUint64(dst[(slot+1)*8:], values[slot+1])
			slot += 2
			continue
		}
		bits := values[slot]
		if !isWideValType(typ) {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(dst[slot*8:], bits)
		slot++
	}
}

func hasValType(types []ValType, want ValType) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func hasReferenceValType(types []ValType) bool {
	return hasValType(types, ValFuncRef) || hasValType(types, ValExternRef)
}

func (in *Instance) marshalPublicReferenceArgs(subject string, values []uint64, types []ValType) error {
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			binary.LittleEndian.PutUint64(in.serArgs[slot*8:], values[slot])
			binary.LittleEndian.PutUint64(in.serArgs[(slot+1)*8:], values[slot+1])
			slot += 2
			continue
		}
		bits := values[slot]
		switch typ {
		case ValFuncRef:
			if bits != 0 {
				if in.refStore == nil {
					return fmt.Errorf("%s: invalid funcref token for argument %d", subject, i)
				}
				descriptor, ok := in.refStore.resolve(bits)
				if !ok {
					return fmt.Errorf("%s: invalid funcref token for argument %d", subject, i)
				}
				bits = descriptor
			}
		case ValExternRef:
			if bits != 0 && (in.refStore == nil || !in.validExternrefToken(bits)) {
				return fmt.Errorf("%s: invalid externref token for argument %d", subject, i)
			}
		}
		if !isWideValType(typ) {
			bits = uint64(uint32(bits))
		}
		binary.LittleEndian.PutUint64(in.serArgs[slot*8:], bits)
		slot++
	}
	return nil
}

func (in *Instance) translatePublicReferenceResults(subject string, values []uint64, types []ValType) error {
	slot := 0
	for i, typ := range types {
		if typ == ValFuncRef && values[slot] != 0 {
			store, err := in.funcrefStoreForEgress()
			if err != nil {
				clear(values)
				return fmt.Errorf("%s: invalid funcref result %d: %w", subject, i, err)
			}
			token, err := store.issue(in, values[slot])
			if err != nil {
				clear(values)
				return fmt.Errorf("%s: invalid funcref result %d: %w", subject, i, err)
			}
			values[slot] = token
		}
		if typ == ValExternRef && values[slot] != 0 && !in.validExternrefToken(values[slot]) {
			clear(values)
			return fmt.Errorf("%s: invalid externref result %d", subject, i)
		}
		if typ == ValV128 {
			slot += 2
		} else {
			slot++
		}
	}
	return nil
}

func (in *Instance) findInvokeCache(export string) *invokeCache {
	for i := range in.ic {
		if in.ic[i].valid && sameExportName(in.ic[i].export, export) {
			return &in.ic[i]
		}
	}
	return nil
}

func sameExportName(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return len(a) == 0 || unsafe.StringData(a) == unsafe.StringData(b) || a == b
}

// CodeBase returns the base address of the instance's mapped native code and the
// per-local-function entry offsets, for external profilers (e.g. writing a
// /tmp/perf-<pid>.map JIT symbol map). Debug/introspection use only.
func (in *Instance) CodeBase() (base uintptr, entries []int) {
	// Copy the entry table so callers cannot mutate the compiled module's
	// (potentially shared) state through the returned slice.
	return in.base, append([]int(nil), in.c.Entry...)
}
