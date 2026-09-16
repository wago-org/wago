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
	mu         sync.Mutex
	bindings   resolvedImports
	identities map[string]importBindingKey
	decls      []*registeredImport
	err        error
	sealed     bool
}

type resolvedImports map[string]any

func NewImports() *Imports {
	return &Imports{bindings: make(resolvedImports), identities: make(map[string]importBindingKey)}
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
	if im.identities[key] != (importBindingKey{module: module, name: name}) {
		return nil, false
	}
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
	identity := importBindingKey{module: module, name: name}
	if _, ok := im.identities[key]; ok {
		im.record(fmt.Errorf("wago: duplicate import %q.%q", module, name))
		return false
	}
	im.identities[key] = identity
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
	imp.inferredParams, imp.inferredResults, imp.inferred = inferredHostFuncSignature(fn)
	imp.params = append([]ValType(nil), imp.inferredParams...)
	imp.results = append([]ValType(nil), imp.inferredResults...)
	if im.add(module, name, fn) {
		im.decls = append(im.decls, imp)
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
	imp := &registeredImport{module: module, name: name, eventI32: fn, params: []ValType{ValI32}}
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

func (im *Imports) snapshot() (resolvedImports, map[string]importBindingKey, error) {
	if im == nil {
		return nil, nil, nil
	}
	im.mu.Lock()
	defer im.mu.Unlock()
	im.sealed = true
	for _, imp := range im.decls {
		if event, ok := im.bindings[imp.key()].(I32HostEvent); ok {
			if event == nil {
				im.record(fmt.Errorf("wago: import %q.%q: deferred host callback is nil", imp.module, imp.name))
			}
			continue
		}
		if imp.fn == nil || nilHostCallback(imp.fn) {
			im.record(fmt.Errorf("wago: import %q.%q: host callback is nil", imp.module, imp.name))
			continue
		}
		_, _, inferred := inferredHostFuncSignature(imp.fn)
		if !inferred && !isHostCallCallback(imp.fn) && !isHostCallback(imp.fn) {
			im.record(fmt.Errorf("wago: import %q.%q: unsupported host callback %T", imp.module, imp.name, imp.fn))
			continue
		}
		if imp.inferred && (!slices.Equal(imp.params, imp.inferredParams) || !slices.Equal(imp.results, imp.inferredResults)) {
			im.record(fmt.Errorf("wago: import %q.%q: declared signature %v -> %v does not match callback signature %v -> %v", imp.module, imp.name, imp.params, imp.results, imp.inferredParams, imp.inferredResults))
		}
	}
	if im.err != nil {
		return nil, nil, im.err
	}
	bindings := make(resolvedImports, len(im.bindings))
	for key, value := range im.bindings {
		bindings[key] = value
	}
	identities := make(map[string]importBindingKey, len(im.identities))
	for key, value := range im.identities {
		identities[key] = value
	}
	return bindings, identities, nil
}

func isHostCallCallback(value any) bool {
	switch value.(type) {
	case HostCallFunc, func(HostCall), CallerHostCallFunc, func(Caller, HostCall):
		return true
	default:
		return false
	}
}

func nilHostCallback(value any) bool {
	switch fn := value.(type) {
	case slotHostFunc:
		return fn == nil
	case callerSlotHostFunc:
		return fn == nil
	case HostCallFunc:
		return fn == nil
	case CallerHostCallFunc:
		return fn == nil
	case func(HostCall):
		return fn == nil
	case func(Caller, HostCall):
		return fn == nil
	case noArgsHostFunc:
		return fn == nil
	case func():
		return fn == nil
	case i32HostFunc:
		return fn == nil
	case func(int32):
		return fn == nil
	case i32ToI32HostFunc:
		return fn == nil
	case func(int32) int32:
		return fn == nil
	case i32I32HostFunc:
		return fn == nil
	case func(int32, int32):
		return fn == nil
	case i32I32ToI32HostFunc:
		return fn == nil
	case func(int32, int32) int32:
		return fn == nil
	case i32ToI32I32HostFunc:
		return fn == nil
	case func(int32) (int32, int32):
		return fn == nil
	case i32I32ToI32I32HostFunc:
		return fn == nil
	case func(int32, int32) (int32, int32):
		return fn == nil
	case func(int64) int64:
		return fn == nil
	case func(int64, int64) int64:
		return fn == nil
	case func(float32) float32:
		return fn == nil
	case func(float32, float32) float32:
		return fn == nil
	case func(float64) float64:
		return fn == nil
	case func(float64, float64) float64:
		return fn == nil
	case func(FuncRef) FuncRef:
		return fn == nil
	case func(ExternRef) ExternRef:
		return fn == nil
	case func(ExnRef) ExnRef:
		return fn == nil
	case func(GCRef) GCRef:
		return fn == nil
	case func(I31Ref) I31Ref:
		return fn == nil
	default:
		return false
	}
}
