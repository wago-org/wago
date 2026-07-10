package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"sort"
	"unsafe"

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

// CompileWithConfig is the named compatibility form of Compile(cfg, wasmBytes):
// the config's feature set gates which modules are accepted and its bounds-check
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

func compileWithConfig(cfg *RuntimeConfig, wasmBytes []byte) (*Compiled, error) {
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
	// A module with a returning import can only run through the legacy async path
	// once that import is bound to another instance's function; defer its codegen
	// to the link-time recompile in Instantiate. Architectures that always use
	// the sync-host dispatcher can compile host-default code up front and still
	// recompile later if Instantiate binds an import cross-instance.
	elide := cfg.boundsChecks == BoundsChecksSignalsBased
	importedFuncs := m.ImportedFuncCount()
	syncHostInitial := forceSyncHostImports && importedFuncs > 0
	needsLink := moduleNeedsLink(m) && !syncHostInitial
	var code []byte
	var entry, internalEntry []int
	if !needsLink {
		cm, err := railshotCompileModuleWith(m, railshotCompileOptions{ElideBoundsChecks: elide, NoBoundsFacts: cfg.noDeferBounds, SyncHostCalls: syncHostInitial})
		if err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		code, entry, internalEntry = cm.Code, cm.Entry, cm.InternalEntry
	}

	c := &Compiled{Code: code, Entry: entry, InternalEntry: internalEntry, NumImports: importedFuncs, Exports: map[string]int{}, Names: m.NameSec, GlobalExports: map[string]int{}, boundsMode: cfg.boundsChecks, GCTypeDescs: gcDescs, needsLink: needsLink, boundsElide: elide, noDeferBounds: cfg.noDeferBounds, requiresSIMD: frontend.ModuleRequiresSIMD(m), syncHostImports: syncHostInitial}
	if importedFuncs > 0 {
		c.importFuncSigs = make([]FuncSig, importedFuncs)
		for i := 0; i < importedFuncs; i++ {
			if ft, ok := m.FuncSignature(uint32(i)); ok {
				c.importFuncSigs[i] = FuncSig{valTypesFromWasm(ft.Params), valTypesFromWasm(ft.Results)}
			}
		}
	}
	// Retain the raw module for the link-time recompile whenever an import could be
	// bound cross-instance (any function import), or codegen was deferred.
	if needsLink || importedFuncs > 0 {
		c.wasmBytes = append([]byte(nil), wasmBytes...)
	}
	// Any module with function imports may need a host-only sync recompile at
	// Instantiate (deferred returning/v128 imports, or non-legacy host bindings),
	// and that generated code is independent of the concrete host function values.
	if importedFuncs > 0 && !syncHostInitial {
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
			min := im.Type.Table.Limits.Min
			if min > uint64(maxInt()) {
				return nil, fmt.Errorf("table import %q.%q minimum %d overflows int", im.Module, im.Name, min)
			}
			c.tableImportMin = int(min)
			if im.Type.Table.Limits.Max != nil {
				max := *im.Type.Table.Limits.Max
				if max > uint64(maxInt()) {
					return nil, fmt.Errorf("table import %q.%q maximum %d overflows int", im.Module, im.Name, max)
				}
				c.tableImportMax = int(max)
				c.tableImportHasMax = true
			}
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

	hasTable, tableSize, tableMax, err := frontend.SupportedTableRuntimeShape(m)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	c.HasTable = hasTable
	c.TableSize = tableSize
	c.TableMax = tableMax
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
		if e.Mode.Kind == wasm.ElemDeclarative {
			continue
		}
		funcs, err := elementPayloads(e)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		init := ElemInit{Funcs: funcs}
		if e.Mode.Kind == wasm.ElemPassive {
			// table.init/elem.drop immediates address the module's original element
			// index space, not a dense list of only passive segments. Preserve holes for
			// active/declarative segments so runtime descriptor index N is element N.
			if len(c.passiveElems) <= i {
				c.passiveElems = append(c.passiveElems, make([]ElemInit, i+1-len(c.passiveElems))...)
			}
			c.passiveElems[i] = init
			continue
		}
		if e.Mode.Kind == wasm.ElemActive {
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
	maxPassiveData := -1
	for i := range m.Data {
		if m.Data[i].Mode.Kind == wasm.DataPassive {
			maxPassiveData = i
		}
	}
	if maxPassiveData >= 0 {
		// Keep data indexes stable: memory.init/data.drop immediates index the
		// module's data segment list, not the compacted passive-only list.
		c.PassiveData = make([]PassiveDataInit, maxPassiveData+1)
	}
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

// moduleNeedsLink reports whether the module has a returning function import,
// which the host log-and-replay model cannot satisfy: it must be bound to
// another instance's function at Instantiate, so codegen is deferred to the
// link-time recompile.
func moduleNeedsLink(m *wasm.Module) bool {
	imported := m.ImportedFuncCount()
	for i := 0; i < imported; i++ {
		if ft, ok := m.FuncSignature(uint32(i)); ok && (len(ft.Results) != 0 || funcTypeUsesV128(ft)) {
			return true
		}
	}
	return false
}

func elementPayloads(e *wasm.Elem) ([]uint32, error) {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		out := make([]uint32, len(e.Kind.Funcs))
		for i, fidx := range e.Kind.Funcs {
			out[i] = uint32(fidx)
		}
		return out, nil
	case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
		out := make([]uint32, len(e.Kind.Exprs))
		for i, ex := range e.Kind.Exprs {
			payload, err := elementExprPayload(ex)
			if err != nil {
				return nil, fmt.Errorf("expression %d: %w", i, err)
			}
			out[i] = payload
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported element kind %d", e.Kind.Kind)
	}
}

func elementExprPayload(e wasm.Expr) (uint32, error) {
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

// linkModule resolves the module's function imports against the provided imports
// and, when any resolve to another instance's function (cross-instance linking),
// recompiles the module with those bindings so the calls lower to a native
// context-swap. It returns c unchanged when no linking is needed (host-only
// modules keep their prebuilt fast-path code). The returned *Compiled is
// instance-specific: its code bakes the callee instances' addresses.
func (c *Compiled) linkModule(imports Imports) (*Compiled, error) {
	bindings := make([]railshotImportBinding, len(c.Imports))
	anyCross := false
	forceSyncHost := false
	for i, key := range c.Imports {
		ex, ok := imports[key].(*InstanceExport)
		if !ok {
			if _, isHost := imports[key].(HostFunc); isHost {
				if i >= len(c.importFuncSigs) {
					return nil, fmt.Errorf("import %q: missing signature", key)
				}
				// A void import taking an optional single i32 arg can be served by the
				// async log-and-replay path (which captures one i32, no results). Any
				// other signature must run through the synchronous host dispatcher.
				if !c.syncHostImports && (forceSyncHostImports || !asyncReplayable(c.importFuncSigs[i])) {
					forceSyncHost = true
				}
			} else if imports[key] != nil {
				// A non-HostFunc host binding must run through the synchronous host
				// dispatcher (bindHostImport rejects it there if it is not a HostFunc).
				if !c.syncHostImports {
					forceSyncHost = true
				}
			}
			continue
		}
		if ex == nil || ex.inst == nil {
			return nil, fmt.Errorf("cross-instance import %q is nil", key)
		}
		if ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
			return nil, fmt.Errorf("cross-instance import %q references an unavailable function", key)
		}
		bindings[i] = railshotImportBinding{
			CrossInstance: true,
			CalleeLinMem:  uint64(ex.inst.jm.LinMemBase()),
			CalleeEntry:   uint64(ex.inst.base + uintptr(ex.inst.c.Entry[ex.localIdx])),
		}
		anyCross = true
	}
	if !c.needsLink && !anyCross && !forceSyncHost {
		return c, nil // host-only legacy void imports: use the prebuilt async code
	}
	// Host-only link (deferred codegen, no cross-instance binding): the recompiled
	// code does not depend on which host functions are supplied, so produce it once
	// and reuse it — every later Instantiate then skips re-running the backend and
	// shares the one executable mapping. (bindings here are all zero-value.)
	if !anyCross {
		if hl := c.hostLink; hl != nil {
			hl.once.Do(func() { hl.c, hl.err = c.recompileLinked(nil, bindings, forceSyncHost) })
			return hl.c, hl.err
		}
	}
	return c.recompileLinked(imports, bindings, forceSyncHost)
}

// recompileLinked re-runs codegen with the given import bindings and returns a
// fresh linked Compiled. bindings is all zero-value for a host-only link and
// carries per-instance callee addresses for cross-instance imports.
func (c *Compiled) recompileLinked(imports Imports, bindings []railshotImportBinding, forceSyncHost bool) (*Compiled, error) {
	if len(c.wasmBytes) == 0 {
		return nil, fmt.Errorf("cross-instance linking requires the retained module source")
	}
	m, err := wasm.DecodeModule(c.wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("link: decode: %w", err)
	}
	imported := m.ImportedFuncCount()
	importSigs := make([]FuncSig, imported)
	syncHost := forceSyncHost
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
		} else if len(ft.Results) != 0 || funcTypeUsesV128(ft) {
			// A returning import, or any host import carrying v128 slots, uses the
			// synchronous re-entry protocol (callHostSync). The older async log path
			// can only replay legacy void HostFunc calls with a single i32 argument.
			syncHost = true
		}
	}
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{ElideBoundsChecks: c.boundsElide, NoBoundsFacts: c.noDeferBounds, ImportBindings: bindings, SyncHostCalls: syncHost})
	if err != nil {
		return nil, fmt.Errorf("link: %w", err)
	}
	linked := *c
	linked.Code = cm.Code
	linked.Entry = cm.Entry
	linked.InternalEntry = cm.InternalEntry
	linked.needsLink = false
	linked.requiresSIMD = c.requiresSIMD || frontend.ModuleRequiresSIMD(m)
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
	if len(c.importFuncSigs) != c.NumImports {
		return fmt.Errorf("compiled metadata invalid: importFuncSigs length %d != NumImports %d", len(c.importFuncSigs), c.NumImports)
	}
	if c.NumImports > maxInt()-len(c.Funcs) {
		return fmt.Errorf("compiled metadata invalid: function count overflows int")
	}
	if compiledMetadataUsesSIMD(c) && !c.requiresSIMD {
		return fmt.Errorf("compiled metadata invalid: SIMD value types without requiresSIMD")
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
	} else {
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
	if len(c.Elems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d element segment(s) without table", len(c.Elems))
	}
	if len(c.passiveElems) > 0 && !c.HasTable {
		return fmt.Errorf("compiled metadata invalid: %d passive element segment(s) without table", len(c.passiveElems))
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
	validateElementFuncs := func(kind string, seg int, funcs []uint32) error {
		for k, fidx := range funcs {
			if fidx == nullFuncRefIndex {
				continue
			}
			if int(fidx) >= totalFuncs {
				return fmt.Errorf("compiled metadata invalid: %s element %d function %d index %d out of range", kind, seg, k, fidx)
			}
		}
		return nil
	}
	for seg, el := range c.Elems {
		if el.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("element", seg, el.Offset.Global); err != nil {
				return err
			}
		}
		if err := validateElementFuncs("active", seg, el.Funcs); err != nil {
			return err
		}
	}
	for seg, el := range c.passiveElems {
		if err := validateElementFuncs("passive", seg, el.Funcs); err != nil {
			return err
		}
	}
	for seg, d := range c.Data {
		if d.Offset.HasGlobal {
			if err := c.validateDeferredOffsetGlobal("data", seg, d.Offset.Global); err != nil {
				return err
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

func (c *Compiled) validateArenaFootprint() error {
	maxParams, maxResults, err := c.maxCallSlots()
	if err != nil {
		return fmt.Errorf("compiled metadata invalid: %w", err)
	}
	funcRefCount := 0
	if c.HasTable {
		funcRefCount = len(c.FuncTypeID) + 1
	}
	need, err := wruntime.InstantiateArenaNeed(wruntime.InstantiateFootprint{
		FuncImportCount:  len(c.Imports),
		FuncRefCount:     funcRefCount,
		GlobalCount:      len(c.Globals),
		HasTable:         c.HasTable,
		TableSize:        c.TableSize,
		TableCapacity:    c.TableMax,
		ElemCount:        len(c.Elems),
		PassiveElemCount: len(c.passiveElems),
		PassiveDataCount: len(c.PassiveData),
		MaxParamSlots:    maxParams,
		MaxResultSlots:   maxResults,
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

// Bumped to 16 for the merge of the wasm-2 table-ops codec (passive element
// descriptors, table metadata) with the memory.init codec (passive data
// descriptors); the combined blob format is distinct from either branch's.
const wagoVersion = 16

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
	if c.needsLink || (len(c.Entry) == 0 && len(c.Funcs) > 0) {
		return nil, errors.New("wago: link-deferred compiled modules cannot be serialized; instantiate or recompile from wasm at load time")
	}
	if c.syncHostImports {
		return nil, errors.New("wago: synchronous-host compiled modules cannot be serialized; recompile from wasm at load time")
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
	if c.requiresSIMD && !hostSupportsSIMD() {
		return fmt.Errorf("wago: compiled module requires SIMD CPU features unavailable on this host")
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
	return Compile(nil, b)
}

// Invoke marshals slot-based arguments/results around one native WasmWrapper
// call. The returned slice is backed by an instance-owned buffer and stays valid
// only until the next call on this Instance; copy it if you need to retain it.
// Invoke calls an exported function. Arguments and results are raw uint64 slots
// interpreted per the function's signature (encode/decode scalar slots with
// I32/I64/F32/F64 and AsI32/AsI64/AsF32/AsF64). A v128 occupies two adjacent
// little-endian uint64 slots in the argument and result slices.
func (in *Instance) Invoke(export string, args ...uint64) ([]uint64, error) {
	ic := in.findInvokeCache(export)
	if ic == nil {
		var err error
		ic, err = in.fillInvokeCache(export)
		if err != nil {
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
	li := ic.li
	if len(args) != ic.paramSlots {
		return nil, fmt.Errorf("%s expects %d arg slot(s), got %d", export, ic.paramSlots, len(args))
	}
	switch len(args) {
	case 0:
	case 1:
		binary.LittleEndian.PutUint64(in.serArgs, args[0])
	case 2:
		binary.LittleEndian.PutUint64(in.serArgs, args[0])
		binary.LittleEndian.PutUint64(in.serArgs[8:], args[1])
	default:
		for i, a := range args {
			binary.LittleEndian.PutUint64(in.serArgs[i*8:], a)
		}
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
		var err error
		if directPreparedCallEnabled && preparedCallEnabled && in.ownsMem {
			err = in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results)
		} else {
			err = callNative(in.c, in.eng, in.jm, !in.ownsMem, entry, in.serArgs, in.trap, in.results)
		}
		if err != nil {
			return nil, err
		}
		if len(in.hostLog) != 0 {
			if err := in.replayHostLog(); err != nil {
				return nil, err
			}
		}
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	if ic.resultSlots == 1 {
		if ic.resultWide[0] {
			out[0] = binary.LittleEndian.Uint64(in.results)
		} else {
			out[0] = uint64(binary.LittleEndian.Uint32(in.results))
		}
		return out, nil
	}
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
		var err error
		if directPreparedCallEnabled && preparedCallEnabled && in.ownsMem {
			err = in.eng.CallPrepared(entry, in.serArgs, in.jm.LinMemBase(), in.trap, in.results)
		} else {
			err = callNative(in.c, in.eng, in.jm, !in.ownsMem, entry, in.serArgs, in.trap, in.results)
		}
		if err != nil {
			return nil, err
		}
		if err := in.replayHostLog(); err != nil {
			return nil, err
		}
	}
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
		if rt == ValI64 || rt == ValF64 {
			out[resSlot] = binary.LittleEndian.Uint64(in.results[off:])
		} else {
			out[resSlot] = uint64(binary.LittleEndian.Uint32(in.results[off:]))
		}
		resSlot++
	}
	return out, nil
}

// replayHostLog runs the void host imports the last native call logged. Each
// logged entry carries the single i32 argument the codegen captured; it is passed
// to the stack-form HostFunc as params[0], with no results.
func (in *Instance) replayHostLog() (err error) {
	if len(in.hostLog) == 0 {
		return nil
	}
	// A replayed host call may panic(HostExit{...}) (e.g. WASI proc_exit) to end
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
	mod := instanceHostModule{in: in}
	var params [1]uint64
	for i := uint32(0); i < n; i++ {
		off := 8 + i*8
		imp := binary.LittleEndian.Uint32(in.hostLog[off:])
		arg := int32(binary.LittleEndian.Uint32(in.hostLog[off+4:]))
		if int(imp) < len(in.c.Imports) {
			if fn := in.hosts[in.c.Imports[imp]]; fn != nil {
				params[0] = uint64(uint32(arg))
				fn(mod, params[:], nil)
			}
		}
	}
	return nil
}

// fillInvokeCache resolves export to its local function index and memoizes it so
// subsequent Invokes on the same export skip the exports map probe.
func (in *Instance) fillInvokeCache(export string) (*invokeCache, error) {
	li, err := in.c.localIndex(export)
	if err != nil {
		return nil, err
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
	if paramSlots > len(in.serArgs)/8 || resultSlots > len(in.results)/8 {
		return nil, fmt.Errorf("%s signature exceeds instance call buffers", export)
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
			rw = append(rw, r == ValI64 || r == ValF64)
		}
	}
	*slot = invokeCache{export: export, valid: true, li: li, paramSlots: paramSlots, resultSlots: resultSlots, resultWide: rw}
	return slot, nil
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
	// Repeated Invoke calls overwhelmingly reuse the same string/literal. Pointer
	// identity avoids runtime.memequal; dynamically rebuilt equal names retain the
	// ordinary content comparison fallback. Empty export names are valid Wasm.
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
