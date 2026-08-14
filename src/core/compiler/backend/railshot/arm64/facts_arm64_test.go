//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"
	"unsafe"
)

func TestValueFactsFitExistingStoragePaddingArm64(t *testing.T) {
	if got, want := unsafe.Sizeof(storage{}), uintptr(64); got != want {
		t.Fatalf("storage size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(elem{}), uintptr(112); got != want {
		t.Fatalf("elem size = %d, want %d", got, want)
	}
}

func TestCompareCarriesBooleanFactArm64(t *testing.T) {
	f := fn{s: newStack()}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opLtU, mtI32)
	if got := f.s.back().st.facts; !got.has(factUpper32Zero | factBoolean) {
		t.Fatalf("compare facts = %#x, want upper-zero and boolean", got)
	}
}
