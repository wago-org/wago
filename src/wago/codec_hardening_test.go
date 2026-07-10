package wago

import (
	"bytes"
	"encoding/binary"
	"reflect"
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
	// A void f64 import cannot use the async replay path, so binding it forces the
	// synchronous host dispatcher.
	c := MustCompile(voidF64ImportCallerModule())
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}})
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

func TestCompiledCodecRoundTripsReferenceSignatures(t *testing.T) {
	input := &Compiled{
		Code:       []byte{0xc3},
		Entry:      []int{0},
		Funcs:      []FuncSig{{Params: []ValType{ValFuncRef, ValExternRef}, Results: []ValType{ValExternRef, ValFuncRef}}},
		FuncTypeID: []uint32{0},
		Exports:    map[string]int{"refs": 0},
	}
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if blob[4] != wagoVersion || wagoVersion != 18 {
		t.Fatalf("compiled codec version = %d, want explicit reference-metadata version 18", blob[4])
	}
	oldVersion := append([]byte(nil), blob...)
	oldVersion[4] = 17
	var old Compiled
	if err := old.UnmarshalBinary(oldVersion); err == nil || !strings.Contains(err.Error(), "version 17 unsupported") {
		t.Fatalf("version-17 reference blob error = %v, want explicit incompatibility rejection", err)
	}
	var got Compiled
	if err := got.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	params, results, err := got.Signature("refs")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if want := []ValType{ValFuncRef, ValExternRef}; !reflect.DeepEqual(params, want) {
		t.Fatalf("params = %v, want %v", params, want)
	}
	if want := []ValType{ValExternRef, ValFuncRef}; !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %v, want %v", results, want)
	}
}

func TestCompiledCodecRejectsReferenceGlobalMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    *Compiled
	}{
		{name: "null funcref", c: &Compiled{Globals: []GlobalDef{{Type: ValFuncRef}}}},
		{name: "live externref token", c: &Compiled{Globals: []GlobalDef{{Type: ValExternRef, Bits: 0x1234}}}},
		{
			name: "imported externref",
			c: &Compiled{
				GlobalImports: []GlobalImportDef{{Module: "env", Name: "ref", Type: ValExternRef}},
				Globals:       []GlobalDef{{Type: ValExternRef}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.c.MarshalBinary()
			if err == nil || !strings.Contains(err.Error(), "reference global metadata") {
				t.Fatalf("MarshalBinary error = %v, want reference global metadata rejection", err)
			}
		})
	}
}

func TestCompiledCodecLoadRejectsForgedLiveReferenceGlobal(t *testing.T) {
	const marker = uint64(0x8877665544332211)
	blob, err := marshalCompiled(&Compiled{Globals: []GlobalDef{{Type: ValI64, Bits: marker}}})
	if err != nil {
		t.Fatalf("marshal scalar fixture: %v", err)
	}
	var encodedMarker [8]byte
	binary.LittleEndian.PutUint64(encodedMarker[:], marker)
	i := bytes.Index(blob, encodedMarker[:])
	if i < 2 {
		t.Fatalf("encoded marker not found in compiled blob")
	}
	blob[i-2] = 0x6f // change the scalar global type to externref, retaining live token bits.

	var got Compiled
	if err := got.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "reference global metadata") {
		t.Fatalf("UnmarshalBinary error = %v, want forged reference global rejection", err)
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
	in, err := Instantiate(&dec, InstantiateOptions{})
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
	c, err := Compile(nil, codecSIMDModule())
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
	c, err := Compile(nil, codecSIMDBlockTypeModule())
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
