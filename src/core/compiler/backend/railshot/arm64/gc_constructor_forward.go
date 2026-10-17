//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// tryForwardGCConstructorGet recognizes an adjacent cast and scalar field read
// from a freshly allocated struct. It returns only constants and direct local
// reads, which can be read again after the allocation helper without retaining
// a register or compiler object snapshot across the call.
func (f *fn) tryForwardGCConstructorGet(r *wasm.Reader, sourceType uint32, source wasm.SubType) (storage, wasm.Reader, bool) {
	next := *r
	op, err := next.Byte()
	if err != nil || op != 0xfb {
		return storage{}, wasm.Reader{}, false
	}
	sub, err := next.U32()
	if err != nil {
		return storage{}, wasm.Reader{}, false
	}
	target, exact := int64(sourceType), false
	get := sub
	if sub == 22 || sub == 23 {
		target, exact, err = readRefHeapTypeImmediate(&next)
		if err != nil || target < 0 {
			return storage{}, wasm.Reader{}, false
		}
		if op, err = next.Byte(); err != nil || op != 0xfb {
			return storage{}, wasm.Reader{}, false
		}
		get, err = next.U32()
	}
	if err != nil || get != 2 {
		return storage{}, wasm.Reader{}, false
	}
	accessType, err := next.U32()
	if err != nil {
		return storage{}, wasm.Reader{}, false
	}
	fieldIndex, err := next.U32()
	if err != nil || int(fieldIndex) >= len(source.Comp.Fields) {
		return storage{}, wasm.Reader{}, false
	}
	field, ok := f.stagedStructField(accessType, fieldIndex)
	if !ok || field.Storage().Packed() {
		return storage{}, wasm.Reader{}, false
	}
	scalar := field.Storage().Val().Kind() == wasm.ValNum
	if !scalar {
		return storage{}, wasm.Reader{}, false
	}
	fieldN := len(source.Comp.Fields)
	if fieldN > f.depth() {
		return storage{}, wasm.Reader{}, false
	}
	value := f.s.back()
	for i := fieldN - 1; i > int(fieldIndex); i-- {
		value = f.s.prev(f.s.baseOfValentBlock(value))
	}
	if value.elemKind() != ekValue {
		return storage{}, wasm.Reader{}, false
	}
	forward := value.st
	switch forward.kind {
	case stConst:
	case stLocalRef, stLocalReg:
		forward.kind = stLocalRef
		forward.reg = regNone
	default:
		return storage{}, wasm.Reader{}, false
	}
	if exact {
		if uint32(target) != sourceType {
			return storage{}, wasm.Reader{}, false
		}
	} else {
		if !f.constructorSubtypeProof(sourceType, uint32(target)) {
			return storage{}, wasm.Reader{}, false
		}
	}
	if !f.constructorSubtypeProof(sourceType, accessType) {
		return storage{}, wasm.Reader{}, false
	}
	return forward, next, true
}

func (f *fn) constructorSubtypeProof(source, target uint32) bool {
	actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
	return f.m.ReferenceTypeSubtype(actual, required)
}
