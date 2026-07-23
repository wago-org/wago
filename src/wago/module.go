package wago

import "sort"

// Module is the runtime-aware wrapper over a *Compiled: it carries the compiled
// code plus the extension-derived view of the module (its imports, the
// capabilities it requires, and lightweight metadata). rt.Compile returns one;
// rt.Instantiate consumes one.
type Module struct {
	rt      *Runtime
	c       *Compiled
	imports []ImportSpec
	reqCaps []Capability
}

// ImportKind classifies what a module imports.
type ImportKind uint8

const (
	ImportFunc ImportKind = iota
	ImportGlobal
	ImportMemory
	ImportTable
)

func (k ImportKind) String() string {
	switch k {
	case ImportGlobal:
		return "global"
	case ImportMemory:
		return "memory"
	case ImportTable:
		return "table"
	default:
		return "func"
	}
}

// ImportSpec describes one import a module declares, enriched with its exact
// structural type and, for function imports, capability/docs metadata from the
// extension providing it. Index is the kind-specific Wasm index. Type/Mutable
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
	Type          ValType
	Mutable       bool
	Min           int
	Max           int
	HasMax        bool
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
	ImportModule string
	ImportName   string
	Exports      []string
}

// GlobalMetadata describes one global in Wasm global-index order.
type GlobalMetadata struct {
	Index        int
	Type         ValType
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
	Min          int
	Max          int
	HasMax       bool
	ImportModule string
	ImportName   string
	Exports      []string
}

// ModuleMetadata is a deterministic, inspectable structural summary of a module.
type ModuleMetadata struct {
	ExportedFuncs        []string
	ExportedGlobals      []string
	ExportedTables       []string
	FuncImportCount      int
	RequiredCapabilities []Capability
	Functions            []FunctionMetadata
	Globals              []GlobalMetadata
	Tables               []TableMetadata
}

// buildModule wraps a freshly compiled module, resolving each import against the
// runtime's registered extensions to attach signatures, capabilities, and
// provided-state.
func (rt *Runtime) buildModule(c *Compiled) *Module {
	m := &Module{rt: rt, c: c}
	rt.mu.Lock()
	defer rt.mu.Unlock()

	capSeen := map[Capability]bool{}
	for i, key := range c.Imports { // function imports, in "module.name" form
		mod, name := splitImportKey(key)
		spec := ImportSpec{Module: mod, Name: name, Kind: ImportFunc, Index: i}
		if i < len(c.importFuncSigs) {
			spec.Params = append([]ValType(nil), c.importFuncSigs[i].Params...)
			spec.Results = append([]ValType(nil), c.importFuncSigs[i].Results...)
		}
		if _, ok := rt.imports[key]; ok {
			spec.Provided = true
		}
		if meta := rt.importMeta[key]; meta != nil {
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
		m.imports = append(m.imports, ImportSpec{
			Module: gi.Module, Name: gi.Name, Kind: ImportGlobal, Index: i,
			Type: gi.Type, Mutable: gi.Mutable, Provided: rt.imports[key] != nil,
		})
	}
	if key, ok := c.MemoryImport(); ok {
		mod, name := splitImportKey(key)
		m.imports = append(m.imports, ImportSpec{Module: mod, Name: name, Kind: ImportMemory, Provided: rt.imports[key] != nil})
	}
	for i := 0; i < c.tableImportCount(); i++ {
		def, _ := c.tableImportAt(i)
		mod, name := splitImportKey(def.Key)
		m.imports = append(m.imports, ImportSpec{
			Module: mod, Name: name, Kind: ImportTable, Index: i,
			Type: def.Type, Min: def.Min, Max: def.Max, HasMax: def.HasMax,
			Provided: rt.imports[def.Key] != nil,
		})
	}
	return m
}

// Compiled returns the underlying low-level compiled module.
func (m *Module) Compiled() *Compiled { return m.c }

// Exports returns the module's exported function names, sorted.
func (m *Module) Exports() []string { return m.c.ExportedFunctions() }

// Imports returns the module's declared imports with extension-derived metadata.
func (m *Module) Imports() []ImportSpec { return append([]ImportSpec(nil), m.imports...) }

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
	functionExports := exportsByIndex(c.Exports, c.NumImports+len(c.Funcs))
	functions := make([]FunctionMetadata, c.NumImports+len(c.Funcs))
	for i := range functions {
		functions[i].Index = i
		functions[i].Exports = functionExports[i]
		if i < c.NumImports {
			if i < len(c.importFuncSigs) {
				functions[i].Params = append([]ValType(nil), c.importFuncSigs[i].Params...)
				functions[i].Results = append([]ValType(nil), c.importFuncSigs[i].Results...)
			}
			if i < len(c.Imports) {
				functions[i].ImportModule, functions[i].ImportName = splitImportKey(c.Imports[i])
			}
			continue
		}
		sig := c.Funcs[i-c.NumImports]
		functions[i].Params = append([]ValType(nil), sig.Params...)
		functions[i].Results = append([]ValType(nil), sig.Results...)
	}

	globalExports := exportsByIndex(c.GlobalExports, len(c.Globals))
	globals := make([]GlobalMetadata, len(c.Globals))
	for i, def := range c.Globals {
		globals[i] = GlobalMetadata{Index: i, Type: def.Type, Mutable: def.Mutable, Exports: globalExports[i]}
		if i < len(c.GlobalImports) {
			globals[i].ImportModule = c.GlobalImports[i].Module
			globals[i].ImportName = c.GlobalImports[i].Name
		}
	}

	tableExports := exportsByIndex(c.tableExports, c.tableCount())
	tables := make([]TableMetadata, c.tableCount())
	for i := range tables {
		tables[i] = TableMetadata{Index: i, Type: c.tableElementType(i), Exports: tableExports[i]}
		if imp, ok := c.tableImportAt(i); ok {
			tables[i].ImportModule, tables[i].ImportName = splitImportKey(imp.Key)
			tables[i].Min, tables[i].Max, tables[i].HasMax = imp.Min, imp.Max, imp.HasMax
			continue
		}
		def := c.tableDef(i)
		tables[i].Min, tables[i].HasMax = def.Size, def.HasMax
		if def.HasMax {
			tables[i].Max = def.Max
		}
	}

	exportedTables := make([]string, 0, len(c.tableExports))
	for name, index := range c.tableExports {
		if index != memoryExportSentinel {
			exportedTables = append(exportedTables, name)
		}
	}
	sort.Strings(exportedTables)
	return ModuleMetadata{
		ExportedFuncs:        c.ExportedFunctions(),
		ExportedGlobals:      c.ExportedGlobals(),
		ExportedTables:       exportedTables,
		FuncImportCount:      len(c.Imports),
		RequiredCapabilities: m.RequiredCapabilities(),
		Functions:            functions,
		Globals:              globals,
		Tables:               tables,
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

// Close releases module-level resources. The underlying compiled code is
// reference-counted and reclaimed once its instances close, so this is currently
// a no-op reserved for future extension-owned module state.
func (m *Module) Close() error { return nil }

// splitImportKey splits a "module.name" key at the first dot.
func splitImportKey(key string) (module, name string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
