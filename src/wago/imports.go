package wago

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Imports is a reusable collection of exact WebAssembly import declarations.
// Configure it before first use. Instantiation seals it and snapshots its
// bindings, after which concurrent reuse is safe and mutation is rejected.
type Imports struct {
	mu       sync.Mutex
	bindings resolvedImports
	decls    []*registeredImport
	err      error
	sealed   bool
}

type resolvedImports map[string]any

func NewImports() *Imports {
	return &Imports{bindings: make(resolvedImports)}
}

// importBindingMapKey is collision-free for every valid UTF-8 WebAssembly
// name, including names containing dots, colons, or NUL bytes.
func importBindingMapKey(module, name string) string {
	return strconv.Itoa(len(module)) + ":" + module + name
}

func splitImportBindingMapKey(key string) (module, name string, ok bool) {
	colon := strings.IndexByte(key, ':')
	if colon <= 0 {
		return "", "", false
	}
	size, err := strconv.Atoi(key[:colon])
	if err != nil || size < 0 || colon+1+size > len(key) {
		return "", "", false
	}
	return key[colon+1 : colon+1+size], key[colon+1+size:], true
}

// Lookup returns a declaration by its exact WebAssembly module and name.
// The returned value is the registered callback or imported object; it does not
// expose runtime-resolved binding storage.
func (im *Imports) Lookup(module, name string) (any, bool) {
	if im == nil {
		return nil, false
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	key := importBindingMapKey(module, name)
	value, ok := im.bindings[key]
	return value, ok
}

func (im *Imports) record(err error) { im.err = errors.Join(im.err, err) }

func (im *Imports) add(module, name string, value any) bool {
	if im.sealed {
		im.record(fmt.Errorf("wago: import %q.%q: collection is sealed", module, name))
		return false
	}
	key := importBindingMapKey(module, name)
	if _, ok := im.bindings[key]; ok {
		im.record(fmt.Errorf("wago: duplicate import %q.%q", module, name))
		return false
	}
	im.bindings[key] = value
	return true
}

func (im *Imports) HostFunc(module, name string, fn any) *ImportFuncBuilder {
	if im == nil {
		return &ImportFuncBuilder{}
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	imp := &registeredImport{module: module, name: name, fn: fn}
	var supported, nilCallback bool
	imp.inferredParams, imp.inferredResults, imp.inferred, supported, nilCallback = inspectHostFuncSignature(fn)
	imp.params = append([]ValType(nil), imp.inferredParams...)
	imp.results = append([]ValType(nil), imp.inferredResults...)
	if im.add(module, name, fn) {
		im.decls = append(im.decls, imp)
		if fn == nil || nilCallback {
			im.record(fmt.Errorf("wago: import %q.%q: host callback is nil", module, name))
		} else if !supported {
			im.record(fmt.Errorf("wago: import %q.%q: unsupported host callback %T", module, name, fn))
		}
		return &ImportFuncBuilder{imp: imp, imports: im}
	}
	return &ImportFuncBuilder{}
}

func (im *Imports) I32Event(module, name string, fn I32HostEvent) *ImportFuncBuilder {
	if im == nil {
		return &ImportFuncBuilder{}
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	imp := &registeredImport{module: module, name: name, fn: fn, eventI32: fn, params: []ValType{ValI32}}
	if im.add(module, name, fn) {
		im.decls = append(im.decls, imp)
		return &ImportFuncBuilder{imp: imp, imports: im}
	}
	return &ImportFuncBuilder{}
}

func (im *Imports) Function(module, name string, fn any) *Imports { return im.bind(module, name, fn) }
func (im *Imports) Global(module, name string, global any) *Imports {
	return im.bind(module, name, global)
}
func (im *Imports) Memory(module, name string, memory *Memory) *Imports {
	return im.bind(module, name, memory)
}
func (im *Imports) Table(module, name string, table *Table) *Imports {
	return im.bind(module, name, table)
}
func (im *Imports) Tag(module, name string, tag *Tag) *Imports { return im.bind(module, name, tag) }

func (im *Imports) bind(module, name string, value any) *Imports {
	if im == nil {
		return im
	}
	im.mu.Lock()
	im.add(module, name, value)
	im.mu.Unlock()
	return im
}

func (im *Imports) snapshot() (resolvedImports, error) {
	if im == nil {
		return nil, nil
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	if !im.sealed {
		im.sealed = true
		for _, imp := range im.decls {
			if event, ok := imp.fn.(I32HostEvent); ok {
				if event == nil {
					im.record(fmt.Errorf("wago: import %q.%q: deferred host callback is nil", imp.module, imp.name))
				}
				continue
			}
			if imp.inferred && (!slices.Equal(imp.params, imp.inferredParams) || !slices.Equal(imp.results, imp.inferredResults)) {
				im.record(fmt.Errorf("wago: import %q.%q: declared signature %v -> %v does not match callback signature %v -> %v", imp.module, imp.name, imp.params, imp.results, imp.inferredParams, imp.inferredResults))
			}
		}
	}
	if im.err != nil {
		return nil, im.err
	}
	return im.bindings, nil
}
