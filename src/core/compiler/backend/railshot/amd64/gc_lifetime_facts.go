//go:build amd64

package amd64

import (
	"sync"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// gcLifetimeModuleFacts is compile-scoped and shared read-only after each lazy
// entry is published. The map is populated only for types referenced by an
// accepted proof; there is no module-wide sizing or preparation pass.
type gcLifetimeModuleFacts struct {
	mu     sync.Mutex
	module *wasm.Module
	types  map[uint32]*gcLifetimeTypeFact
}

type gcLifetimeTypeFact struct {
	typ    wasm.SubType
	layout nativeGCStructAllocPlan
}

func newGCLifetimeModuleFacts() *gcLifetimeModuleFacts {
	return &gcLifetimeModuleFacts{}
}

func (c *gcLifetimeModuleFacts) cachedLayout(f *fn, typeIndex uint32) (nativeGCStructAllocPlan, bool) {
	if c == nil {
		return nativeGCStructAllocPlan{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.module != f.m {
		return nativeGCStructAllocPlan{}, false
	}
	fact := c.types[typeIndex]
	if fact == nil || !fact.matches(f, typeIndex) {
		return nativeGCStructAllocPlan{}, false
	}
	return fact.layout, true
}

func (c *gcLifetimeModuleFacts) typeFact(f *fn, typeIndex uint32) (*gcLifetimeTypeFact, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.module == nil {
		c.module = f.m
	} else if c.module != f.m {
		return nil, false
	}
	if fact := c.types[typeIndex]; fact != nil {
		return fact, fact.matches(f, typeIndex)
	}
	layout, ok := f.deriveNativeGCStructAllocLayout(typeIndex)
	if !ok {
		return nil, false
	}
	typ, ok := f.stagedStructType(typeIndex)
	source, found := f.m.TypeSubType(typeIndex)
	if !ok || !found || !lifetimeSubTypeEqual(typ, *source) {
		return nil, false
	}
	// Own every slice once. Published facts never borrow worker scratch or a
	// caller-owned optional layout slice.
	typ.Comp = lifetimeCompCopy(typ.Comp)
	typ.Supers = append([]wasm.TypeIdx(nil), typ.Supers...)
	layout.fields = append(layout.fields[:0:0], layout.fields...)
	fact := &gcLifetimeTypeFact{typ: typ, layout: layout}
	if c.types == nil {
		c.types = make(map[uint32]*gcLifetimeTypeFact)
	}
	c.types[typeIndex] = fact
	return fact, true
}

func (f *gcLifetimeTypeFact) matches(fn *fn, typeIndex uint32) bool {
	if f == nil {
		return false
	}
	typ, ok := fn.stagedStructType(typeIndex)
	if !ok || !lifetimeSubTypeEqual(typ, f.typ) {
		return false
	}
	if layout, ok := fn.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if !layout.NativeAlloc {
			return false
		}
		align := layout.Align
		if align < 8 {
			align = 8
		}
		return lifetimeLayoutEqual(nativeGCStructAllocPlan{
			fields: layout.FieldLayout, objectSize: layout.ObjectSize,
			objectAlign: align, pointerFree: layout.PointerFree,
		}, f.layout)
	}
	return nativeGCStructAllocLayoutMatches(fn.m, typeIndex, f.layout)
}

func lifetimeCompCopy(c wasm.CompType) wasm.CompType {
	c.Fields = append([]wasm.FieldType(nil), c.Fields...)
	c.Params = append([]wasm.ValType(nil), c.Params...)
	c.Results = append([]wasm.ValType(nil), c.Results...)
	return c
}

// These comparisons stay explicit so production replay does not box values
// through reflect.DeepEqual.
func lifetimeSubTypeEqual(a, b wasm.SubType) bool {
	if a.Final != b.Final || a.Metadata != b.Metadata || a.Comp.Kind != b.Comp.Kind ||
		len(a.Supers) != len(b.Supers) || len(a.Comp.Fields) != len(b.Comp.Fields) ||
		len(a.Comp.Params) != len(b.Comp.Params) || len(a.Comp.Results) != len(b.Comp.Results) {
		return false
	}
	for i := range a.Supers {
		if a.Supers[i] != b.Supers[i] {
			return false
		}
	}
	for i := range a.Comp.Fields {
		if a.Comp.Fields[i] != b.Comp.Fields[i] {
			return false
		}
	}
	for i := range a.Comp.Params {
		if a.Comp.Params[i] != b.Comp.Params[i] {
			return false
		}
	}
	for i := range a.Comp.Results {
		if a.Comp.Results[i] != b.Comp.Results[i] {
			return false
		}
	}
	return true
}

func lifetimeLayoutEqual(a, b nativeGCStructAllocPlan) bool {
	if a.objectSize != b.objectSize || a.objectAlign != b.objectAlign || a.pointerFree != b.pointerFree || len(a.fields) != len(b.fields) {
		return false
	}
	for i := range a.fields {
		if a.fields[i] != b.fields[i] {
			return false
		}
	}
	return true
}
