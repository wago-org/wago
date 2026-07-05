package wago

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

// ImportSpec describes one import a module declares, enriched (for function
// imports) with the declared signature, required capability, and docs of the
// extension providing it. Provided reports whether the runtime currently has a
// binding for it.
type ImportSpec struct {
	Module        string
	Name          string
	Kind          ImportKind
	Params        []ValType
	Results       []ValType
	Capability    Capability
	HasCapability bool
	Docs          string
	Provided      bool
}

// Key returns the "module.name" import key.
func (s ImportSpec) Key() string { return s.Module + "." + s.Name }

// ModuleMetadata is a compact, inspectable summary of a module.
type ModuleMetadata struct {
	ExportedFuncs        []string
	ExportedGlobals      []string
	FuncImportCount      int
	RequiredCapabilities []Capability
}

// buildModule wraps a freshly compiled module, resolving each import against the
// runtime's registered extensions to attach signatures, capabilities, and
// provided-state.
func (rt *Runtime) buildModule(c *Compiled) *Module {
	m := &Module{rt: rt, c: c}
	rt.mu.Lock()
	defer rt.mu.Unlock()

	capSeen := map[Capability]bool{}
	for _, key := range c.Imports { // function imports, in "module.name" form
		mod, name := splitImportKey(key)
		spec := ImportSpec{Module: mod, Name: name, Kind: ImportFunc}
		if _, ok := rt.imports[key]; ok {
			spec.Provided = true
		}
		if meta := rt.importMeta[key]; meta != nil {
			spec.Params = append([]ValType(nil), meta.params...)
			spec.Results = append([]ValType(nil), meta.results...)
			spec.Capability, spec.HasCapability = meta.cap, meta.hasCap
			spec.Docs = meta.docs
			if meta.hasCap && !capSeen[meta.cap] {
				capSeen[meta.cap] = true
				m.reqCaps = append(m.reqCaps, meta.cap)
			}
		}
		m.imports = append(m.imports, spec)
	}
	for _, gi := range c.GlobalImports {
		m.imports = append(m.imports, ImportSpec{
			Module: gi.Module, Name: gi.Name, Kind: ImportGlobal,
			Provided: rt.imports[gi.Module+"."+gi.Name] != nil,
		})
	}
	if key, ok := c.MemoryImport(); ok {
		mod, name := splitImportKey(key)
		m.imports = append(m.imports, ImportSpec{Module: mod, Name: name, Kind: ImportMemory})
	}
	if key, ok := c.TableImport(); ok {
		mod, name := splitImportKey(key)
		m.imports = append(m.imports, ImportSpec{Module: mod, Name: name, Kind: ImportTable})
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

// Metadata returns a compact summary for inspection/CLI use.
func (m *Module) Metadata() ModuleMetadata {
	return ModuleMetadata{
		ExportedFuncs:        m.c.ExportedFunctions(),
		ExportedGlobals:      m.c.ExportedGlobals(),
		FuncImportCount:      len(m.c.Imports),
		RequiredCapabilities: m.RequiredCapabilities(),
	}
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
