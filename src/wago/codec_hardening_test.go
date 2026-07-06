package wago

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestMarshalRejectsLinkDeferredModule(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	if !c.needsLink {
		t.Fatal("returning import should defer codegen")
	}
	_, err := c.MarshalBinary()
	if err == nil || !strings.Contains(err.Error(), "link-deferred") {
		t.Fatalf("want link-deferred marshal error, got %v", err)
	}
}

func TestMarshalRejectsSyncHostLinkedModule(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(voidI32ImportCallerModule())
	in, err := Instantiate(c, Imports{"env.log": SyncHostFunc(func(HostModule, []uint64, []uint64) {})})
	if err != nil {
		t.Fatalf("instantiate sync host module: %v", err)
	}
	defer in.Close()
	if !in.c.syncHostImports {
		t.Fatal("linked module should use sync host imports")
	}
	_, err = in.c.MarshalBinary()
	if err == nil || !strings.Contains(err.Error(), "synchronous-host") {
		t.Fatalf("want synchronous-host marshal error, got %v", err)
	}
}

func TestMarshalGlobalScalarAndV128RoundTrip(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
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

func TestUnmarshalTruncatedV128GlobalPayload(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	vec := V128{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf}
	c := &Compiled{Globals: []GlobalDef{{Type: ValV128, V128: vec}}}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	i := bytes.Index(blob, vec[:])
	if i < 0 {
		t.Fatalf("encoded v128 payload % x not found", vec)
	}
	truncated := append([]byte(nil), blob[:i+8]...)
	var dec Compiled
	if err := dec.UnmarshalBinary(truncated); err == nil || (!strings.Contains(err.Error(), "truncated") && !strings.Contains(err.Error(), "unexpected EOF")) {
		t.Fatalf("want truncated v128 global error, got %v", err)
	}
}

func TestUnmarshalRejectsSIMDBlobWhenHostUnsupported(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	old := simdHostFeaturesSupported
	simdHostFeaturesSupported = func() bool { return true }
	c, err := Compile(codecSIMDModule())
	if err != nil {
		simdHostFeaturesSupported = old
		t.Fatalf("Compile SIMD module: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		simdHostFeaturesSupported = old
		t.Fatalf("MarshalBinary: %v", err)
	}
	simdHostFeaturesSupported = func() bool { return false }
	defer func() { simdHostFeaturesSupported = old }()

	var dec Compiled
	if err := dec.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "requires SIMD") {
		t.Fatalf("want SIMD CPU feature rejection, got %v", err)
	}
}

func TestUnmarshalRejectsV128BlockTypeBlobWhenHostUnsupported(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	old := simdHostFeaturesSupported
	simdHostFeaturesSupported = func() bool { return true }
	c, err := Compile(codecSIMDBlockTypeModule())
	if err != nil {
		simdHostFeaturesSupported = old
		t.Fatalf("Compile SIMD block type module: %v", err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		simdHostFeaturesSupported = old
		t.Fatalf("MarshalBinary: %v", err)
	}
	simdHostFeaturesSupported = func() bool { return false }
	defer func() { simdHostFeaturesSupported = old }()

	var dec Compiled
	if err := dec.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "requires SIMD") {
		t.Fatalf("want SIMD CPU feature rejection for v128 block type, got %v", err)
	}
}

func codecSIMDModule() []byte {
	body := []byte{0x00, 0xfd, 0x0c}
	body = append(body, make([]byte, 16)...)
	body = append(body, 0x1a, 0x0b) // v128.const; drop; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func codecSIMDBlockTypeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x7b, // block (result v128)
			0x00, // unreachable
			0x0b, // end block
			0x1a, // drop v128 result
			0x0b, // end function
		}))),
	)
}
