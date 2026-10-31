package wasm

import (
	"testing"
	"unsafe"
)

func TestValidationUnknownTagPreservesExactTypes(t *testing.T) {
	if unsafe.Sizeof(val{}) != unsafe.Sizeof(ValType{}) {
		t.Fatal("validation operand has separate marker storage")
	}
	types := []ValType{I32, I64, F32, F64, V128, Bot, FuncRef, ExternRef,
		RefVal(Ref(true, IndexedHeap(TypeIdx{Index: ^uint32(0), Rec: true}), true)),
		{lo: RefVal(Ref(false, HeapType{lo: uint64(heapBottom)}, false)).lo, hi: ^uint64(0)},
	}
	for _, typ := range types {
		value := val{t: typ}
		if value.unknown() {
			t.Fatalf("known type %#v aliases the unknown marker", typ)
		}
		value.t.lo |= unknownValidationBit
		if !value.unknown() {
			t.Fatal("unknown marker lost")
		}
		value.t.lo &^= unknownValidationBit
		if value.t != typ {
			t.Fatal("exact type payload changed")
		}
	}
}
