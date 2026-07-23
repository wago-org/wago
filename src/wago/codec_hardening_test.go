package wago

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestMarshalRoundTripsReturningImportDispatch(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	defer loaded.Close()
	if !loaded.dynamicImports {
		t.Fatal("loaded returning import lost dynamic dispatch metadata")
	}
}

func TestMarshalRoundTripsSyncHostDispatch(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	c := MustCompile(voidF64ImportCallerModule())
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	defer loaded.Close()
	called := 0
	in, err := Instantiate(&loaded, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) { called++ })}})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g"); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if called != 1 {
		t.Fatalf("host calls = %d, want 1", called)
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
	if blob[4] != wagoVersion || wagoVersion != 23 {
		t.Fatalf("compiled codec version = %d, want extended-const version 23", blob[4])
	}
	for _, version := range []byte{20, 21, 22} {
		oldVersion := append([]byte(nil), blob...)
		oldVersion[4] = version
		var old Compiled
		if err := old.UnmarshalBinary(oldVersion); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("version %d unsupported", version)) {
			t.Fatalf("version-%d reference blob error = %v, want explicit incompatibility rejection", version, err)
		}
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

func TestCompiledCodecPreservesNoTableFuncRefDescriptors(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile no-table ref.func body: %v", err)
	}
	if compiled.HasTable || !compiled.NeedsFuncRefDescs {
		t.Fatalf("descriptor metadata HasTable=%v NeedsFuncRefDescs=%v, want false/true", compiled.HasTable, compiled.NeedsFuncRefDescs)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if loaded.HasTable || !loaded.NeedsFuncRefDescs {
		t.Fatalf("loaded descriptor metadata HasTable=%v NeedsFuncRefDescs=%v, want false/true", loaded.HasTable, loaded.NeedsFuncRefDescs)
	}
	in, err := Instantiate(&loaded)
	if err != nil {
		t.Fatalf("Instantiate loaded no-table ref.func body: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("get")
	if err != nil || len(got) != 1 || got[0] == 0 {
		t.Fatalf("loaded get = %v, %v; want one non-null token", got, err)
	}
}

func TestCompiledCodecAcceptsStructuralReferenceGlobalsAndRejectsLiveBits(t *testing.T) {
	for _, c := range []*Compiled{
		{Globals: []GlobalDef{{Type: ValFuncRef}}},
		{
			GlobalImports: []GlobalImportDef{{Module: "env", Name: "ref", Type: ValExternRef}},
			Globals:       []GlobalDef{{Type: ValExternRef}},
		},
	} {
		_ = roundTripCompiled(t, c)
	}
	if _, err := (&Compiled{Globals: []GlobalDef{{Type: ValExternRef, Bits: 0x1234}}}).MarshalBinary(); err == nil || !strings.Contains(err.Error(), "non-null externref") {
		t.Fatalf("MarshalBinary live externref error = %v, want fail-closed rejection", err)
	}
}

func TestCompiledCodecRejectsMalformedExtendedConstMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    *Compiled
		want string
	}{
		{name: "global bad type", c: &Compiled{Globals: []GlobalDef{{Type: ValI64, InitExpr: []byte{0x41, 0x01, 0x0b}}}}, want: "type i32, want i64"},
		{name: "global unavailable import", c: &Compiled{Globals: []GlobalDef{{Type: ValI32, InitExpr: []byte{0x23, 0x00, 0x41, 0x01, 0x6a, 0x0b}}}}, want: "not an imported global"},
		{name: "data malformed", c: &Compiled{Data: []DataInit{{Offset: OffsetInit{Expr: []byte{0x41, 0x01}}}}}, want: "missing end"},
		{name: "element multiple forms", c: &Compiled{Elems: []ElemInit{{RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{HasGlobal: true, Expr: []byte{0x41, 0x00, 0x0b}}}}}, want: "multiple offset initializer forms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("MarshalBinary error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestCompiledCodecLoadRejectsForgedLiveReferenceGlobal(t *testing.T) {
	const marker = uint64(0x8877665544332211)
	// marshalCompiled intentionally bypasses Compiled.MarshalBinary's validation,
	// while still recording the structural reference-types feature bit. This makes
	// the load-side live-token check the only reason the blob can be rejected.
	blob, err := marshalCompiled(&Compiled{Globals: []GlobalDef{{Type: ValExternRef, Bits: marker}}})
	if err != nil {
		t.Fatalf("marshal forged externref fixture: %v", err)
	}
	var got Compiled
	if err := got.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "non-null externref") {
		t.Fatalf("UnmarshalBinary error = %v, want forged live externref rejection", err)
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
