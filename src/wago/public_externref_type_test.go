package wago

import (
	"strings"
	"testing"
)

func TestPublicExternrefBoundaryChecksExactType(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	token := ValueExternRef(issueExternref(t, rt, "public boundary")).Bits()
	in := &Instance{c: &Compiled{}, refStore: rt.refStore, serArgs: make([]byte, 8)}
	for _, tc := range []struct {
		name     string
		heap     AbstractHeapType
		nullable bool
		bits     uint64
		valid    bool
	}{
		{"bottom-null", AbstractHeapNoExtern, true, 0, true},
		{"bottom-token", AbstractHeapNoExtern, true, token, false},
		{"nonnull-null", AbstractHeapExtern, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exact := []ValueTypeDescriptor{{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: tc.nullable, Heap: HeapTypeDescriptor{Abstract: tc.heap}}}}
			for _, result := range []bool{false, true} {
				values := []uint64{tc.bits}
				var err error
				if result {
					err = in.translatePublicReferenceResults("boundary", values, []ValType{ValExternRef}, exact)
				} else {
					err = in.marshalPublicReferenceArgs("boundary", values, []ValType{ValExternRef}, exact, nil)
				}
				if tc.valid && err != nil || !tc.valid && (err == nil || !strings.Contains(err.Error(), "exact structural type")) {
					t.Fatalf("result=%t: %v", result, err)
				}
				if result && err != nil && values[0] != 0 {
					t.Fatal("rejected result was not cleared")
				}
			}
		})
	}
}
