package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestMarshalRejectsLinkDeferredModule(t *testing.T) {
	c := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	if !c.needsLink {
		t.Fatal("returning import should defer codegen")
	}
	_, err := c.MarshalBinary()
	if err == nil || !strings.Contains(err.Error(), "link-deferred") {
		t.Fatalf("want link-deferred marshal error, got %v", err)
	}
}

func TestMarshalGlobalScalarAndV128RoundTrip(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	vec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	vconst := []byte{0xfd, 0x0c}
	vconst = append(vconst, vec[:]...)
	vconst = append(vconst, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x2a, 0x0b}),
			wasmtest.GlobalEntry(wasm.V128, false, vconst),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g32", 3, 0),
			wasmtest.ExportEntry("gv", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	c := MustCompile(mod)
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var dec Compiled
	if err := dec.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	in, err := Instantiate(&dec, nil)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if got, err := in.Global("g32"); err != nil || AsI32(got) != 42 {
		t.Fatalf("g32 = %d, %v; want 42", AsI32(got), err)
	}
	if got, err := in.GlobalV128("gv"); err != nil || got != vec {
		t.Fatalf("gv = % x, %v; want % x", got, err, vec)
	}

	scalar := *c
	scalar.Globals = []GlobalDef{{Type: ValI32, Bits: I32(1)}, {Type: ValI64, Bits: I64(2)}}
	scalar.GlobalExports = map[string]int{}
	compact, err := scalar.MarshalBinary()
	if err != nil {
		t.Fatalf("scalar MarshalBinary: %v", err)
	}
	withVec := scalar
	withVec.Globals = append(append([]GlobalDef(nil), scalar.Globals...), GlobalDef{Type: ValV128, V128: vec})
	larger, err := withVec.MarshalBinary()
	if err != nil {
		t.Fatalf("v128 MarshalBinary: %v", err)
	}
	if delta := len(larger) - len(compact); delta < 17 { // type/mut/bits/init fields plus the 16 vector bytes
		t.Fatalf("adding a v128 global grew encoding by %d bytes, want at least vector payload", delta)
	}
}
