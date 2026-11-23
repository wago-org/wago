//go:build linux && amd64 && tinygo

package runtime

import "testing"

//go:noinline
func tinygoLeafIdentity(value int32) int32 { return value }

//go:noinline
func tinygoLeafAdd(a, b int32) int32 { return a + b }

//go:noinline
func tinygoLeafCapture(value *int32) func(int32) int32 {
	return func(input int32) int32 { return input + *value }
}

func TestTinyGoScalarLeafAdmission(t *testing.T) {
	if scalarLeafBody(tinygoLeafIdentity, 1) == nil {
		t.Fatal("identity callback rejected")
	}
	if scalarLeafBody(tinygoLeafAdd, 2) == nil {
		t.Fatal("two-argument add callback rejected")
	}
	value := int32(19)
	for _, fn := range []any{tinygoLeafCapture(&value), (func(int32) int32)(nil), func(v int64) int64 { return v }} {
		if scalarLeafCanRegister(fn, 1) || scalarLeafBody(fn, 1) != nil {
			t.Fatalf("unproved callback %T admitted", fn)
		}
	}
	if scalarLeafCanRegister(tinygoLeafAdd, 1) || tinygoScalarLeafText(0) != nil {
		t.Fatal("wrong argument count or out-of-image address admitted")
	}
}
