package wago

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Module is the runtime-aware wrapper over a *Compiled: it carries the compiled
// code plus the plugin-derived view of the module (its imports, the
// capabilities it requires, and lightweight metadata). rt.Compile returns one;
// rt.Instantiate consumes one.
type Module struct {
	rt           *Runtime
	c            *Compiled
	compiledView *Compiled
	imports      []ImportSpec
	reqCaps      []Capability
	// importIdentities is populated only when a declared component contains a
	// dot and the flat binding namespace can therefore be ambiguous.
	importIdentities map[string]importBindingKey

	identity             atomic.Pointer[moduleIdentityToken]
	ownsCompiled         bool
	independentInstances bool
	lifeMu               sync.Mutex
	closed               atomic.Bool
	uses                 uint32
	usesDone             chan struct{}
	closeState           atomic.Pointer[moduleCloseState]
	closeCallbacks       atomic.Uint32
}

type moduleCloseState struct {
	done   chan struct{}
	result error
}

// ImportKind classifies what a module imports.
type ImportKind uint8

const (
	ImportFunc ImportKind = iota
	ImportGlobal
	ImportMemory
	ImportTable
	ImportTag
)

func (k ImportKind) String() string {
	switch k {
	case ImportGlobal:
		return "global"
	case ImportMemory:
		return "memory"
	case ImportTable:
		return "table"
	case ImportTag:
		return "tag"
	default:
		return "func"
	}
}

// ImportSpec describes one import a module declares, enriched with its exact
// structural type and, for function imports, capability/docs metadata from the
// plugin providing it. Index is the kind-specific Wasm index. Type/Mutable
// describe globals; Type/Min/Max/HasMax describe tables. Duplicate table/global
// declarations are preserved in declaration order. Provided reports whether the
// runtime currently has a binding for the key.
type ImportSpec struct {
	Module        string
	Name          string
	Kind          ImportKind
	Index         int
	Params        []ValType
	Results       []ValType
	ParamTypes    []ValueTypeDescriptor
	ResultTypes   []ValueTypeDescriptor
	Type          ValType
	ValueType     ValueTypeDescriptor
	HasValueType  bool
	Mutable       bool
	Min           uint64
	Max           uint64
	MemoryMin     uint64
	MemoryMax     uint64
	HasMax        bool
	Addr64        bool
	Shared        bool
	Capability    Capability
	HasCapability bool
	Docs          string
	Provided      bool
}

// Key returns the "module.name" import key.
func (s ImportSpec) Key() string { return s.Module + "." + s.Name }

// FunctionMetadata describes one function in Wasm function-index order.
type FunctionMetadata struct {
	Index        int
	Params       []ValType
	Results      []ValType
	ParamTypes   []ValueTypeDescriptor
	ResultTypes  []ValueTypeDescriptor
	ImportModule string
	ImportName   string
	Exports      []string
}

// GlobalMetadata describes one global in Wasm global-index order.
type GlobalMetadata struct {
	Index        int
	Type         ValType
	ValueType    ValueTypeDescriptor
	HasValueType bool
	Mutable      bool
	ImportModule string
	ImportName   string
	Exports      []string
}

// TableMetadata describes one table in Wasm table-index order. Min is the
// declared minimum. Max is the exact declared maximum when HasMax is true and
// zero otherwise; implementation growth reserves are intentionally not exposed
// as Wasm limits.
type TableMetadata struct {
	Index        int
	Type         ValType
	ValueType    ValueTypeDescriptor
	HasValueType bool
	Min          uint64
	Max          uint64
	HasMax       bool
	Addr64       bool
	ImportModule string
	ImportName   string
	Exports      []string
}

// MemoryMetadata describes one memory in Wasm memory-index order. Min and Max
// are declared page counts, not implementation reservation sizes.
type MemoryMetadata struct {
	Index        int
	Min          uint64
	Max          uint64
	HasMax       bool
	Addr64       bool
	Shared       bool
	ImportModule string
	ImportName   string
	Exports      []string
}

// TagMetadata describes one exception tag in Wasm tag-index order.
type TagMetadata struct {
	Index        int
	TypeIndex    uint32
	Params       []ValType
	ImportModule string
	ImportName   string
	Exports      []string
}

// ModuleMetadata is a deterministic, inspectable structural summary of a module.
type ModuleMetadata struct {
	ExportedFuncs        []string
	Types                []DefinedTypeDescriptor
	ExportedGlobals      []string
	ExportedTables       []string
	ExportedMemories     []string
	FuncImportCount      int
	RequiredFeatures     CoreFeatures
	RequiredCapabilities []Capability
	Functions            []FunctionMetadata
	Globals              []GlobalMetadata
	Tables               []TableMetadata
	Memories             []MemoryMetadata
	Tags                 []TagMetadata
}

type moduleBindings struct {
	rt                   *Runtime
	imports              Imports
	importMeta           map[string]*registeredImport
	independentInstances bool
	moduleIdentity       bool
}

// snapshotModuleBindingsLocked captures one immutable import-policy generation.
// The caller must hold rt.mu.
func (rt *Runtime) snapshotModuleBindingsLocked(hooks *hookRegistry) moduleBindings {
	cfg := rt.cfg.clone()
	bindings := moduleBindings{
		rt:                   rt,
		imports:              make(Imports, len(rt.imports)),
		importMeta:           make(map[string]*registeredImport, len(rt.importMeta)),
		independentInstances: cfg.IndependentInstanceExecution(),
		moduleIdentity:       hooks.needsModuleIdentity(),
	}
	for key, value := range rt.imports {
		bindings.imports[key] = value
	}
	for key, value := range rt.importMeta {
		bindings.importMeta[key] = cloneRegisteredImport(value)
	}
	return bindings
}

// buildModule wraps a compiled module with the runtime's current binding
// generation. Callers that already hold rt.mu should snapshot directly instead.
func (rt *Runtime) buildModule(c *Compiled) *Module {
	rt.mu.Lock()
	hooks := rt.loadHooks()
	bindings := rt.snapshotModuleBindingsLocked(hooks)
	rt.mu.Unlock()
	return buildModule(c, bindings)
}

func buildModule(c *Compiled, bindings moduleBindings) *Module {
	m := &Module{rt: bindings.rt, c: c.freezeExecution(), compiledView: c, independentInstances: bindings.independentInstances}
	c = m.c
	if bindings.moduleIdentity {
		m.identity.Store(&moduleIdentityToken{})
	}

	funcModuleEnds, tableModuleEnds, memoryModuleEnds, tagModuleEnds, _ := c.importModuleEndSections()
	capSeen := map[Capability]bool{}
	for i, key := range c.Imports { // function imports, in "module.name" form
		mod, name := splitImportKeyAt(key, importModuleEndAt(funcModuleEnds, i))
		spec := ImportSpec{Module: mod, Name: name, Kind: ImportFunc, Index: i}
		if i < len(c.importFuncSigs) {
			spec.Params = append([]ValType(nil), c.importFuncSigs[i].Params...)
			spec.Results = append([]ValType(nil), c.importFuncSigs[i].Results...)
			spec.ParamTypes, spec.ResultTypes, _ = exactFuncSignature(c.importFuncSigs[i], c.Types)
		}
		meta := bindings.importMeta[key]
		if _, ok := bindings.imports[key]; ok && registeredImportMatches(meta, mod, name) {
			spec.Provided = true
		}
		if meta != nil && registeredImportMatches(meta, mod, name) {
			spec.Capability, spec.HasCapability = meta.cap, meta.hasCap
			spec.Docs = meta.docs
			if meta.hasCap && !capSeen[meta.cap] {
				capSeen[meta.cap] = true
				m.reqCaps = append(m.reqCaps, meta.cap)
			}
		}
		m.imports = append(m.imports, spec)
	}
	for i, gi := range c.GlobalImports {
		key := gi.Module + "." + gi.Name
		exact, exactErr := exactValueType(gi.Type, gi.HasValueType, gi.ValueTypeIndex, c.ValueTypes, c.Types)
		m.imports = append(m.imports, ImportSpec{
			Module: gi.Module, Name: gi.Name, Kind: ImportGlobal, Index: i,
			Type: gi.Type, ValueType: exact, HasValueType: exactErr == nil, Mutable: gi.Mutable, Provided: bindings.imports[key] != nil,
		})
	}
	for i := 0; i < c.memoryImportCount(); i++ {
		def, _ := c.memoryImportAt(i)
		mod, name := splitImportKeyAt(def.ImportKey, importModuleEndAt(memoryModuleEnds, i))
		m.imports = append(m.imports, ImportSpec{
			Module: mod, Name: name, Kind: ImportMemory, Index: i,
			MemoryMin: def.Min, MemoryMax: def.Max, HasMax: def.HasMax, Addr64: def.Addr64, Shared: def.Shared,
			Provided: bindings.imports[def.ImportKey] != nil,
		})
	}
	for i := 0; i < c.tableImportCount(); i++ {
		def, _ := c.tableImportAt(i)
		mod, name := splitImportKeyAt(def.Key, importModuleEndAt(tableModuleEnds, i))
		exact, exactErr := exactValueType(def.Type, def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types)
		m.imports = append(m.imports, ImportSpec{
			Module: mod, Name: name, Kind: ImportTable, Index: i,
			Type: def.Type, ValueType: exact, HasValueType: exactErr == nil, Min: def.Min, Max: def.Max, HasMax: def.HasMax, Addr64: def.Addr64,
			Provided: bindings.imports[def.Key] != nil,
		})
	}
	if c.memoryDir != nil {
		for i := 0; i < c.tagImportCount(); i++ {
			def := c.memoryDir.ehTags[i]
			mod, name := splitImportKeyAt(def.ImportKey, importModuleEndAt(tagModuleEnds, i))
			sig := c.Types[def.TypeIndex]
			params, _ := valTypesFromDescriptors(sig.Params, c.Types)
			m.imports = append(m.imports, ImportSpec{Module: mod, Name: name, Kind: ImportTag, Index: i, Params: params, ParamTypes: append([]ValueTypeDescriptor(nil), sig.Params...), Provided: bindings.imports[def.ImportKey] != nil})
		}
	}
	return m
}

// indexDeclaredImportIdentities rejects distinct structured import names that
// the low-level flat Imports namespace cannot represent independently. It keeps
// the resulting index only when dotted components make alternate splits
// possible, so per-instance exact overrides can be checked in linear time.
func indexDeclaredImportIdentities(specs []ImportSpec) (map[string]importBindingKey, error) {
	ambiguous := false
	for _, spec := range specs {
		if strings.Contains(spec.Module, ".") || strings.Contains(spec.Name, ".") {
			ambiguous = true
			break
		}
	}
	if !ambiguous {
		return nil, nil
	}

	flatIdentities := make(map[string]importBindingKey, len(specs))
	for _, spec := range specs {
		identity := importBindingKey{module: spec.Module, name: spec.Name}
		key := spec.Key()
		if previous, ok := flatIdentities[key]; ok && previous != identity {
			return nil, importIdentityCollisionError(previous, identity)
		}
		flatIdentities[key] = identity
	}
	return flatIdentities, nil
}

func importIdentityCollisionError(previous, identity importBindingKey) error {
	return fmt.Errorf("wago: imports %q.%q and %q.%q share flattened key %q and cannot be bound safely", previous.module, previous.name, identity.module, identity.name, identity.module+"."+identity.name)
}

// registeredImportMatches prevents the legacy flat binding namespace from
// crossing an exact Wasm module/name boundary. A nil record identifies an
// explicitly supplied legacy binding, which has no structured identity to
// verify and retains the public Imports API's historical behavior.
func registeredImportMatches(meta *registeredImport, module, name string) bool {
	return meta == nil || meta.module == module && meta.name == name
}

func (h *hookRegistry) needsModuleIdentity() bool {
	if h == nil {
		return false
	}
	return len(h.afterCompile) != 0 || len(h.onModuleClose) != 0 ||
		len(h.beforeInstantiate) != 0 || len(h.afterCreate) != 0 ||
		len(h.afterInstantiate) != 0 || len(h.onInstantiateError) != 0 ||
		len(h.beforeClose) != 0 || len(h.afterClose) != 0
}

func (m *Module) moduleIdentity() ModuleIdentity {
	if m == nil {
		return ModuleIdentity{}
	}
	return ModuleIdentity{value: m.identity.Load()}
}

// Compiled returns the underlying low-level compiled module.
func (m *Module) Compiled() *Compiled {
	if m.compiledView != nil {
		return m.compiledView
	}
	return m.c
}

// Exports returns the module's exported function names, sorted.
func (m *Module) Exports() []string { return m.c.ExportedFunctions() }

// Imports returns caller-owned snapshots of the module's declared imports with
// plugin-derived metadata. Mutating the result does not affect the Module.
func (m *Module) Imports() []ImportSpec { return cloneImportSpecs(m.imports) }

// RequiredCapabilities returns the capabilities the module's function imports
// require, deduplicated in first-seen order.
func (m *Module) RequiredCapabilities() []Capability {
	return append([]Capability(nil), m.reqCaps...)
}

// Metadata returns a deterministic structural summary for inspection/CLI use.
func (m *Module) Metadata() ModuleMetadata {
	if m == nil || m.c == nil {
		return ModuleMetadata{}
	}
	c := m.c
	funcModuleEnds, tableModuleEnds, memoryModuleEnds, tagModuleEnds, _ := c.importModuleEndSections()
	functionExports := exportsByIndex(c.Exports, c.NumImports+len(c.Funcs))
	functions := make([]FunctionMetadata, c.NumImports+len(c.Funcs))
	for i := range functions {
		functions[i].Index = i
		functions[i].Exports = functionExports[i]
		if i < c.NumImports {
			if i < len(c.importFuncSigs) {
				functions[i].Params = append([]ValType(nil), c.importFuncSigs[i].Params...)
				functions[i].Results = append([]ValType(nil), c.importFuncSigs[i].Results...)
				functions[i].ParamTypes, functions[i].ResultTypes, _ = exactFuncSignature(c.importFuncSigs[i], c.Types)
			}
			if i < len(c.Imports) {
				functions[i].ImportModule, functions[i].ImportName = splitImportKeyAt(c.Imports[i], importModuleEndAt(funcModuleEnds, i))
			}
			continue
		}
		sig := c.Funcs[i-c.NumImports]
		functions[i].Params = append([]ValType(nil), sig.Params...)
		functions[i].Results = append([]ValType(nil), sig.Results...)
		functions[i].ParamTypes, functions[i].ResultTypes, _ = exactFuncSignature(sig, c.Types)
	}

	globalExports := exportsByIndex(c.GlobalExports, len(c.Globals))
	globals := make([]GlobalMetadata, len(c.Globals))
	for i, def := range c.Globals {
		exact, exactErr := exactValueType(def.Type, def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types)
		globals[i] = GlobalMetadata{Index: i, Type: def.Type, ValueType: exact, HasValueType: exactErr == nil, Mutable: def.Mutable, Exports: globalExports[i]}
		if i < len(c.GlobalImports) {
			globals[i].ImportModule = c.GlobalImports[i].Module
			globals[i].ImportName = c.GlobalImports[i].Name
		}
	}

	tableExports := exportsByIndex(c.tableExports, c.tableCount())
	tables := make([]TableMetadata, c.tableCount())
	for i := range tables {
		def := c.tableDef(i)
		exact, exactErr := exactValueType(c.tableElementType(i), def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types)
		tables[i] = TableMetadata{Index: i, Type: c.tableElementType(i), ValueType: exact, HasValueType: exactErr == nil, Addr64: def.Addr64, Exports: tableExports[i]}
		if imp, ok := c.tableImportAt(i); ok {
			tables[i].ImportModule, tables[i].ImportName = splitImportKeyAt(imp.Key, importModuleEndAt(tableModuleEnds, i))
			tables[i].Min, tables[i].Max, tables[i].HasMax = imp.Min, imp.Max, imp.HasMax
			continue
		}
		def = c.tableDef(i)
		tables[i].Min, tables[i].HasMax = uint64(def.Size), def.HasMax
		if def.HasMax {
			tables[i].Max = def.Max
		}
	}

	memoryExports := exportsByIndex(c.memoryExportMap(), c.memoryCount())
	memories := make([]MemoryMetadata, c.memoryCount())
	for i := range memories {
		def := c.memoryDef(i)
		memories[i] = MemoryMetadata{Index: i, Min: def.Min, Max: def.Max, HasMax: def.HasMax, Addr64: def.Addr64, Shared: def.Shared, Exports: memoryExports[i]}
		if def.ImportKey != "" {
			memories[i].ImportModule, memories[i].ImportName = splitImportKeyAt(def.ImportKey, importModuleEndAt(memoryModuleEnds, i))
		}
	}

	var tags []TagMetadata
	if c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		tagExports := exportsByIndex(c.memoryDir.ehTagExports, len(c.memoryDir.ehTags))
		tags = make([]TagMetadata, len(c.memoryDir.ehTags))
		for i, tag := range c.memoryDir.ehTags {
			tags[i] = TagMetadata{Index: i, TypeIndex: tag.TypeIndex, Exports: tagExports[i]}
			if tag.ImportKey != "" {
				tags[i].ImportModule, tags[i].ImportName = splitImportKeyAt(tag.ImportKey, importModuleEndAt(tagModuleEnds, i))
			}
			if int(tag.TypeIndex) < len(c.Types) && c.Types[tag.TypeIndex].Kind == CompositeTypeFunction {
				tags[i].Params, _ = valTypesFromDescriptors(c.Types[tag.TypeIndex].Params, c.Types)
			}
		}
	}

	return ModuleMetadata{
		ExportedFuncs:        c.ExportedFunctions(),
		Types:                cloneDefinedTypeDescriptors(c.Types),
		ExportedGlobals:      c.ExportedGlobals(),
		ExportedTables:       sortedKeys(c.tableExports),
		ExportedMemories:     sortedKeys(c.memoryExportMap()),
		FuncImportCount:      len(c.Imports),
		RequiredFeatures:     compiledStructuralRequiredFeatures(c),
		RequiredCapabilities: m.RequiredCapabilities(),
		Functions:            functions,
		Globals:              globals,
		Tables:               tables,
		Memories:             memories,
		Tags:                 tags,
	}
}

func exportsByIndex(exports map[string]int, count int) [][]string {
	if count == 0 {
		return nil
	}
	out := make([][]string, count)
	for _, name := range sortedKeys(exports) {
		index := exports[name]
		if index >= 0 && index < count {
			out[index] = append(out[index], name)
		}
	}
	return out
}

// Close ends this runtime-bound module's lifecycle and notifies module-close
// observers exactly once. Modules returned by Runtime.Compile or AdoptModule own
// their Compiled value; Runtime.Module wrappers borrow caller-owned code. Existing
// instances retain executable mappings until they close. Close calls made while
// module-close observers are active return without self-waiting; callers after
// observer completion receive the published final result.
func (m *Module) Close() (err error) {
	if m == nil {
		return nil
	}
	state, owner := m.beginClose()
	if !owner {
		if m.closeCallbacks.Load() != 0 {
			return nil
		}
		<-state.done
		return state.result
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if panicErr, ok := recovered.(error); ok {
				err = joinPrimary(err, fmt.Errorf("wago: module close panicked: %w", panicErr))
			} else {
				err = joinPrimary(err, fmt.Errorf("wago: module close panicked: %v", recovered))
			}
		}
		state.result = err
		close(state.done)
	}()
	return m.closeOwned()
}

func (m *Module) beginClose() (*moduleCloseState, bool) {
	if active := m.closeState.Load(); active != nil {
		return active, false
	}
	candidate := &moduleCloseState{done: make(chan struct{})}
	if m.closeState.CompareAndSwap(nil, candidate) {
		return candidate, true
	}
	return m.closeState.Load(), false
}

func (m *Module) closeOwned() error {
	m.lifeMu.Lock()
	m.closed.Store(true)
	var usesDone <-chan struct{}
	if m.uses != 0 {
		m.usesDone = make(chan struct{})
		usesDone = m.usesDone
	}
	m.lifeMu.Unlock()
	if usesDone != nil {
		<-usesDone
	}

	var errs []error
	if hooks, end := m.rt.beginModuleCloseCallbacks(); end != nil {
		defer end()
		if len(hooks.onModuleClose) != 0 {
			event := ModuleCloseEvent{Module: moduleView(m)}
			m.closeCallbacks.Add(1)
			for i := len(hooks.onModuleClose) - 1; i >= 0; i-- {
				observer := hooks.onModuleClose[i]
				if err := callHookSafely("ModuleCloseObserver", func() { observer(event) }); err != nil {
					errs = append(errs, err)
				}
			}
			m.closeCallbacks.Add(^uint32(0))
		}
	}
	m.identity.Store(nil)
	if m.ownsCompiled && m.c != nil {
		errs = append(errs, m.c.Close())
	}
	return errors.Join(errs...)
}

// beginUse admits one operation that reads the compiled module. Admission is
// atomic with Close publishing the module's closed state.
func (m *Module) beginUse() bool {
	if m == nil {
		return false
	}
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if m.closed.Load() {
		return false
	}
	m.uses++
	return true
}

func (m *Module) endUse() {
	m.lifeMu.Lock()
	if m.uses == 0 {
		m.lifeMu.Unlock()
		panic("wago: module use lease underflow")
	}
	m.uses--
	if m.uses == 0 && m.usesDone != nil {
		close(m.usesDone)
		m.usesDone = nil
	}
	m.lifeMu.Unlock()
}

// splitImportKey splits a "module.name" key at the first dot.
func splitImportKey(key string) (module, name string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// exactImportModuleEnd encodes one plus the module-name byte length so zero can
// retain the legacy first-dot interpretation for hand-built Compiled values.
func exactImportModuleEnd(module string) uint64 { return uint64(len(module)) + 1 }

func importModuleEndAt(ends []uint64, index int) uint64 {
	if index < 0 || index >= len(ends) {
		return 0
	}
	return ends[index]
}

func splitImportKeyAt(key string, moduleEnd uint64) (module, name string) {
	if validateImportModuleEnd(key, moduleEnd) == nil && moduleEnd != 0 {
		i := int(moduleEnd - 1)
		return key[:i], key[i+1:]
	}
	return splitImportKey(key)
}

func validateImportModuleEnd(key string, moduleEnd uint64) error {
	if moduleEnd == 0 { // Legacy hand-built Compiled metadata.
		return nil
	}
	separator := moduleEnd - 1
	if separator >= uint64(len(key)) || key[separator] != '.' {
		return fmt.Errorf("module-name end %d does not identify the import-key separator", moduleEnd)
	}
	return nil
}

// importModuleEndSections returns exact-name sidecars in their persisted order.
// A false result identifies a legacy hand-built Compiled value or malformed
// private metadata; callers retain the historical first-dot fallback in either
// case, while validation rejects malformed non-empty sidecars.
func (c *Compiled) importModuleEndSections() (functions, tables, memories, tags []uint64, exact bool) {
	if c == nil {
		return nil, nil, nil, nil, false
	}
	var ends []uint64
	if memo := c.loadValidateMemo(); memo != nil {
		ends = memo.importModuleEnds
	}
	functionCount := len(c.Imports)
	tableCount := c.tableImportCount()
	memoryCount := c.memoryImportCount()
	tagCount := c.tagImportCount()
	total := functionCount + tableCount + memoryCount + tagCount
	if total == 0 {
		return nil, nil, nil, nil, len(ends) == 0
	}
	if len(ends) != total {
		return nil, nil, nil, nil, false
	}
	tableStart := functionCount
	memoryStart := tableStart + tableCount
	tagStart := memoryStart + memoryCount
	return ends[:tableStart], ends[tableStart:memoryStart], ends[memoryStart:tagStart], ends[tagStart:], true
}

func (c *Compiled) appendImportModuleEnd(moduleEnd uint64) {
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	c.validateMemo.importModuleEnds = append(c.validateMemo.importModuleEnds, moduleEnd)
}

func (c *Compiled) validateImportModuleEnds() error {
	memo := c.loadValidateMemo()
	if memo == nil || len(memo.importModuleEnds) == 0 {
		return nil
	}
	functionEnds, tableEnds, memoryEnds, tagEnds, exact := c.importModuleEndSections()
	if !exact {
		want := len(c.Imports) + c.tableImportCount() + c.memoryImportCount() + c.tagImportCount()
		return fmt.Errorf("compiled metadata invalid: import module-name ends length %d != non-global import count %d", len(memo.importModuleEnds), want)
	}
	for i, key := range c.Imports {
		if err := validateImportModuleEnd(key, functionEnds[i]); err != nil {
			return fmt.Errorf("compiled metadata invalid: function import %d: %w", i, err)
		}
	}
	for i := range tableEnds {
		def, _ := c.tableImportAt(i)
		if err := validateImportModuleEnd(def.Key, tableEnds[i]); err != nil {
			return fmt.Errorf("compiled metadata invalid: table import %d: %w", i, err)
		}
	}
	for i := range memoryEnds {
		def, _ := c.memoryImportAt(i)
		if err := validateImportModuleEnd(def.ImportKey, memoryEnds[i]); err != nil {
			return fmt.Errorf("compiled metadata invalid: memory import %d: %w", i, err)
		}
	}
	if c.memoryDir != nil {
		for i := range tagEnds {
			if err := validateImportModuleEnd(c.memoryDir.ehTags[i].ImportKey, tagEnds[i]); err != nil {
				return fmt.Errorf("compiled metadata invalid: tag import %d: %w", i, err)
			}
		}
	}
	return nil
}
