package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
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

// Compile decodes, validates, and compiles a wasm module to native code using
// the default configuration.
func Compile(wasmBytes []byte) (*Compiled, error) {
	return CompileWithConfig(NewRuntimeConfig(), wasmBytes)
}

// CompileWithConfig is Compile under an explicit RuntimeConfig: the config's
// feature set gates which modules are accepted and its bounds-check mode selects
// the code-generation strategy.
func CompileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
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
	if err := wasm.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	gcDescs, err := frontend.BuildGCTypeDescs(m)
	if err != nil {
		return nil, fmt.Errorf("gc descriptors: %w", err)
	}
	if err := frontend.RejectUnsupportedWithFeatures(m, cfg.frontendFeatures()); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	// A module with a returning import can only run once that import is bound to
	// another instance's function; defer its codegen to the link-time recompile in
	// Instantiate (moduleNeedsLink). Otherwise compile now, on the fast host path.
	elide := cfg.boundsChecks == BoundsChecksSignalsBased
	needsLink := moduleNeedsLink(m)
	var code []byte
	var entry, internalEntry []int
	if !needsLink {
		cm, err := amd64.CompileModuleWith(m, amd64.CompileOptions{ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds})
		if err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		code, entry, internalEntry = cm.Code, cm.Entry, cm.InternalEntry
	}

	importedFuncs := m.ImportedFuncCount()
	c := &Compiled{Code: code, Entry: entry, InternalEntry: internalEntry, NumImports: importedFuncs, Exports: map[string]int{}, Names: m.NameSec, GlobalExports: map[string]int{}, boundsMode: cfg.boundsChecks, GCTypeDescs: gcDescs, needsLink: needsLink, boundsElide: elide, noDeferBounds: cfg.noDeferBounds}
	// Retain the raw module for the link-time recompile whenever an import could be
	// bound cross-instance (any function import), or codegen was deferred.
	if needsLink || importedFuncs > 0 {
		c.wasmBytes = append([]byte(nil), wasmBytes...)
	}
	// A deferred-codegen module memoizes its host-only link so repeated Instantiate
	// (the common WASI case) reuses the code instead of recompiling every time.
	if needsLink {
		c.hostLink = &hostLinkCache{}
	}
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
		case wasm.ExternTable:
			c.tableImport = im.Module + "." + im.Name
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
		case wasm.ExternMem:
			memoryExported = true
		}
	}

	hasTable, tableSize, err := frontend.SupportedTableRuntimeShape(m)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	c.HasTable = hasTable
	c.TableSize = tableSize
	if len(m.Memories) > 0 {
		lim := m.Memories[0].Limits
		c.HasMemory = true
		c.MemMinPages = uint32(lim.Min)
		c.MemMaxPages = 65535 // default ceiling when the module declares no max
		if lim.Max != nil {
			c.MemMaxPages = uint32(*lim.Max)
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
	// Table 0 is the only table wired through the current runtime ABI.
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			c.FuncTypeID = append(c.FuncTypeID, m.StructuralTypeID(m.Imports[i].Type.Type.Index))
		}
	}
	for li := range m.FuncTypes {
		c.FuncTypeID = append(c.FuncTypeID, m.StructuralTypeID(m.FuncTypes[li].Index))
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue // runtime has no bulk element operations yet, so inactive segments are unused
		}
		if e.Kind.Kind == wasm.ElemFuncExprs || e.Kind.Kind == wasm.ElemTypedExprs {
			return nil, fmt.Errorf("compile: active element expression segment %d unsupported", i)
		}
		if e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) == 0 {
			continue
		}
		base, err := evalConstExprWithModule(e.Mode.Offset, wasm.I32, m)
		if err != nil {
			return nil, fmt.Errorf("element %d offset: %w", i, err)
		}
		init := ElemInit{Funcs: make([]uint32, len(e.Kind.Funcs))}
		for j, fidx := range e.Kind.Funcs {
			init.Funcs[j] = uint32(fidx)
		}
		applyElemOffset(&init, base.Init())
		c.Elems = append(c.Elems, init)
	}
	for i := range m.Data {
		d := &m.Data[i]
		if d.Mode.Kind != wasm.DataActive {
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

// moduleNeedsLink reports whether the module has a returning function import,
// which the host log-and-replay model cannot satisfy: it must be bound to
// another instance's function at Instantiate, so codegen is deferred to the
// link-time recompile.
func moduleNeedsLink(m *wasm.Module) bool {
	imported := m.ImportedFuncCount()
	for i := 0; i < imported; i++ {
		if ft, ok := m.FuncSignature(uint32(i)); ok && len(ft.Results) != 0 {
			return true
		}
	}
	return false
}

// linkModule resolves the module's function imports against the provided imports
// and, when any resolve to another instance's function (cross-instance linking),
// recompiles the module with those bindings so the calls lower to a native
// context-swap. It returns c unchanged when no linking is needed (host-only
// modules keep their prebuilt fast-path code). The returned *Compiled is
// instance-specific: its code bakes the callee instances' addresses.
func (c *Compiled) linkModule(imports Imports) (*Compiled, error) {
	bindings := make([]amd64.ImportBinding, len(c.Imports))
	anyCross := false
	for i, key := range c.Imports {
		ex, ok := imports[key].(*InstanceExport)
		if !ok {
			continue
		}
		if ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("cross-instance import %q is nil", key)
		}
		if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
			return nil, fmt.Errorf("cross-instance import %q references an unavailable function", key)
		}
		bindings[i] = amd64.ImportBinding{
			CrossInstance: true,
			CalleeLinMem:  uint64(ex.inst.jm.LinMemBase()),
			CalleeEntry:   uint64(ex.inst.base + uintptr(ex.inst.c.Entry[ex.localIdx])),
		}
		anyCross = true
	}
	if !c.needsLink && !anyCross {
		return c, nil // host-only (or void host-bound imports): use the prebuilt code
	}
	// Host-only link (deferred codegen, no cross-instance binding): the recompiled
	// code does not depend on which host functions are supplied, so produce it once
	// and reuse it — every later Instantiate then skips re-running the backend and
	// shares the one executable mapping. (bindings here are all zero-value.)
	if !anyCross {
		if hl := c.hostLink; hl != nil {
			hl.once.Do(func() { hl.c, hl.err = c.recompileLinked(nil, bindings) })
			return hl.c, hl.err
		}
	}
	return c.recompileLinked(imports, bindings)
}

// recompileLinked re-runs codegen with the given import bindings and returns a
// fresh linked Compiled. bindings is all zero-value for a host-only link and
// carries per-instance callee addresses for cross-instance imports.
func (c *Compiled) recompileLinked(imports Imports, bindings []amd64.ImportBinding) (*Compiled, error) {
	if len(c.wasmBytes) == 0 {
		return nil, fmt.Errorf("cross-instance linking requires the retained module source")
	}
	m, err := wasm.DecodeModule(c.wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("link: decode: %w", err)
	}
	imported := m.ImportedFuncCount()
	importSigs := make([]FuncSig, imported)
	syncHost := false
	for i := 0; i < imported; i++ {
		ft, ok := m.FuncSignature(uint32(i))
		if !ok {
			continue
		}
		importSigs[i] = FuncSig{valTypesFromWasm(ft.Params), valTypesFromWasm(ft.Results)}
		if i < len(bindings) && bindings[i].CrossInstance {
			if ex, ok := imports[c.Imports[i]].(*InstanceExport); !ok || !sigMatches(ft, ex) {
				return nil, fmt.Errorf("cross-instance import %q signature mismatch", c.Imports[i])
			}
		} else if len(ft.Results) != 0 {
			// A returning import bound as a host function uses the synchronous
			// re-entry protocol (callHostSync). No longer an error.
			syncHost = true
		}
	}
	cm, err := amd64.CompileModuleWith(m, amd64.CompileOptions{ElideBoundsChecks: c.boundsElide, NoBoundsFacts: c.noDeferBounds, ImportBindings: bindings})
	if err != nil {
		return nil, fmt.Errorf("link: %w", err)
	}
	linked := *c
	linked.Code = cm.Code
	linked.Entry = cm.Entry
	linked.InternalEntry = cm.InternalEntry
	linked.needsLink = false
	linked.wasmBytes = nil
	linked.codeCache = nil // fresh code mapping (shared across instances of this linked module)
	linked.hostLink = nil  // the linked module is already linked; never re-links
	linked.syncHostImports = syncHost
	linked.importFuncSigs = importSigs
	return installCompiledFinalizer(&linked), nil
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

// MustCompile is like Compile but panics on error, for tests, examples, and
// package-level initialization.
func MustCompile(wasmBytes []byte) *Compiled {
	c, err := Compile(wasmBytes)
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

// TableImport returns the "module.name" key of the module's imported table, if it
// imports one; Instantiate then requires a *Table for that key (cross-instance
// shared table). The boolean is false for a module that defines its own or none.
func (c *Compiled) TableImport() (string, bool) {
	return c.tableImport, c.tableImport != ""
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
	li, err := c.localIndex(export)
	if err != nil {
		return nil, nil, err
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

func (c *Compiled) localIndex(export string) (int, error) {
	gfi, ok := c.Exports[export]
	if !ok {
		return 0, fmt.Errorf("no exported function %q", export)
	}
	li := gfi - c.NumImports
	if li < 0 || li >= len(c.Funcs) {
		return 0, fmt.Errorf("export %q is an imported function", export)
	}
	return li, nil
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
	if c.NumImports > maxInt()-len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: function count overflows int")
	}
	if c.TableSize < 0 {
		return fmt.Errorf("compiled metadata invalid: negative TableSize %d", c.TableSize)
	}
	if !c.HasTable && c.TableSize != 0 {
		return fmt.Errorf("compiled metadata invalid: TableSize %d without table", c.TableSize)
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
	for name, gfi := range c.Exports {
		if gfi < 0 || gfi >= totalFuncs {
			return fmt.Errorf("compiled metadata invalid: function export %q index %d out of range", name, gfi)
		}
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
	}
	for seg, el := range c.Elems {
		if el.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("element", seg, el.Offset.Global); err != nil {
				return err
			}
		}
		for k, fidx := range el.Funcs {
			if int(fidx) >= totalFuncs {
				return fmt.Errorf("compiled metadata invalid: element %d function %d index %d out of range", seg, k, fidx)
			}
		}
	}
	for seg, d := range c.Data {
		if d.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("data", seg, d.Offset.Global); err != nil {
				return err
			}
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

func maxInt() int { return int(^uint(0) >> 1) }

func (c *Compiled) validateArenaFootprint() error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		FuncImportCount: len(c.Imports),
		GlobalCount:     len(c.Globals),
		HasTable:        c.HasTable,
		TableSize:       c.TableSize,
		ElemCount:       len(c.Elems),
		MaxParamSlots:   maxParams,
		MaxResultSlots:  maxResults,
	})
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
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
		if len(fn.Params) > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d parameter count %d overflows call buffer", i, len(fn.Params))
		}
		if len(fn.Results) > maxInt()/8 {
			return 0, 0, fmt.Errorf("function %d result count %d overflows call buffer", i, len(fn.Results))
		}
		if len(fn.Params) > params {
			params = len(fn.Params)
		}
		if len(fn.Results) > results {
			results = len(fn.Results)
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
const wagoVersion = 9

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
	if err := unmarshalCompiled(c, data[5:]); err != nil {
		return err
	}
	if err := c.validate(); err != nil {
		return err
	}
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
	return Compile(b)
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper
// call. The returned slice is backed by an instance-owned buffer and stays valid
// only until the next call on this Instance; copy it if you need to retain it.
// Invoke calls an exported function. Arguments and results are raw uint64s
// interpreted per the function's signature (encode/decode with I32/I64/F32/F64
// and AsI32/AsI64/AsF32/AsF64). The returned slice is reused on the next call.
func (in *Instance) Invoke(export string, args ...uint64) ([]uint64, error) {
	if !in.ic.valid || in.ic.export != export {
		if err := in.fillInvokeCache(export); err != nil {
			// A re-exported import (the export names an imported function) is
			// invoked by calling through to whatever satisfies that import — for a
			// cross-instance binding, the other instance's function.
			if gfi, ok := in.c.Exports[export]; ok && gfi < in.c.NumImports {
				if ex, ok := in.imports[in.c.Imports[gfi]].(*InstanceExport); ok && ex != nil && ex.inst != nil {
					return ex.inst.invokeLocal(ex.localIdx, args)
				}
			}
			return nil, err
		}
	}
	li := in.ic.li
	sig := in.c.Funcs[li]
	if len(args) != len(sig.Params) {
		return nil, fmt.Errorf("%s expects %d arg(s), got %d", export, len(sig.Params), len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("%s requires %d arg slot(s), instance buffer has %d", export, len(args), len(in.serArgs)/8)
	}
	if len(sig.Results) > len(in.results)/8 {
		return nil, fmt.Errorf("%s requires %d result slot(s), instance buffer has %d", export, len(sig.Results), len(in.results)/8)
	}
	for i, a := range args {
		binary.LittleEndian.PutUint64(in.serArgs[i*8:], a)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0) // reset host-call log
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if in.syncMode {
		if err := in.callNativeSync(entry); err != nil {
			return nil, err
		}
	} else {
		if err := callNative(in.c, in.eng, in.jm, entry, in.serArgs, in.trap, in.results); err != nil {
			return nil, err
		}
		in.replayHostLog()
	}
	out := in.resultVals[:len(sig.Results)]
	for i := range sig.Results {
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("%s result %d exceeds instance result buffer", export, i)
		}
		if in.ic.resultWide[i] { // i64 / f64 (8-byte)
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else { // i32 / f32 (4-byte)
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	return out, nil
}

// invokeLocal calls this instance's local function `li` directly (bypassing the
// export-name cache). Used to call through a re-exported import into the instance
// that satisfies it. It shares the instance's call buffers, so the returned slice
// is valid only until the next call on this instance.
func (in *Instance) invokeLocal(li int, args []uint64) ([]uint64, error) {
	if li < 0 || li >= len(in.c.Funcs) || li >= len(in.c.Entry) {
		return nil, fmt.Errorf("invalid function index %d", li)
	}
	sig := in.c.Funcs[li]
	if len(args) != len(sig.Params) {
		return nil, fmt.Errorf("function expects %d arg(s), got %d", len(sig.Params), len(args))
	}
	if len(args) > len(in.serArgs)/8 {
		return nil, fmt.Errorf("requires %d arg slot(s), instance buffer has %d", len(args), len(in.serArgs)/8)
	}
	if len(sig.Results) > len(in.results)/8 {
		return nil, fmt.Errorf("requires %d result slot(s), instance buffer has %d", len(sig.Results), len(in.results)/8)
	}
	for i, a := range args {
		binary.LittleEndian.PutUint64(in.serArgs[i*8:], a)
	}
	if len(in.hostLog) > 0 {
		binary.LittleEndian.PutUint32(in.hostLog, 0)
	}
	entry := in.base + uintptr(in.c.Entry[li])
	if in.syncMode {
		if err := in.callNativeSync(entry); err != nil {
			return nil, err
		}
	} else {
		if err := callNative(in.c, in.eng, in.jm, entry, in.serArgs, in.trap, in.results); err != nil {
			return nil, err
		}
		in.replayHostLog()
	}
	out := in.resultVals[:len(sig.Results)]
	for i, rt := range sig.Results {
		off := i * 8
		if off+8 > len(in.results) {
			return nil, fmt.Errorf("result %d exceeds instance result buffer", i)
		}
		if rt == ValI64 || rt == ValF64 {
			out[i] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[i] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
	}
	return out, nil
}

// replayHostLog runs the void host imports the last native call logged.
func (in *Instance) replayHostLog() {
	if len(in.hostLog) == 0 {
		return
	}
	n := binary.LittleEndian.Uint32(in.hostLog)
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				fn(arg)
			}
		}
	}
}

// fillInvokeCache resolves export to its local function index and memoizes it so
// subsequent Invokes on the same export skip the exports map probe.
func (in *Instance) fillInvokeCache(export string) error {
	li, err := in.c.localIndex(export)
	if err != nil {
		in.ic.valid = false
		return err
	}
	results := in.c.Funcs[li].Results
	rw := in.ic.resultWide[:0]
	if cap(rw) < len(results) {
		rw = make([]bool, 0, len(results))
	}
	for _, r := range results {
		rw = append(rw, r == ValI64 || r == ValF64)
	}
	in.ic = invokeCache{export: export, valid: true, li: li, resultWide: rw}
	return nil
}

// CodeBase returns the base address of the instance's mapped native code and the
// per-local-function entry offsets, for external profilers (e.g. writing a
// /tmp/perf-<pid>.map JIT symbol map). Debug/introspection use only.
func (in *Instance) CodeBase() (base uintptr, entries []int) {
	// Copy the entry table so callers cannot mutate the compiled module's
	// (potentially shared) state through the returned slice.
	return in.base, append([]int(nil), in.c.Entry...)
}
