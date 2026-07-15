//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func encodedFuncType(params, results [][]byte) []byte {
	out := []byte{0x60}
	out = append(out, wasmtest.ULEB(uint32(len(params)))...)
	for _, typ := range params {
		out = append(out, typ...)
	}
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	for _, typ := range results {
		out = append(out, typ...)
	}
	return out
}

func typedCallBoundaryModule() []byte {
	i32 := []byte{0x7f}
	ref0 := encodedNullableIndexedRef(0)
	nonNullRef0 := append([]byte{0x64}, wasmtest.ULEB(0)...)
	ref1 := encodedNullableIndexedRef(1)
	types := [][]byte{
		encodedFuncType(nil, nil),
		encodedFuncType([][]byte{i32}, [][]byte{i32}),
		encodedFuncType([][]byte{ref0}, [][]byte{ref0}),
		encodedFuncType([][]byte{nonNullRef0}, [][]byte{nonNullRef0}),
		encodedFuncType(nil, [][]byte{nonNullRef0}),
		encodedFuncType(nil, [][]byte{ref1}),
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2),
			wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(5),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("id", 0, 2),
			wasmtest.ExportEntry("nonNull", 0, 3),
			wasmtest.ExportEntry("getF", 0, 4),
			wasmtest.ExportEntry("getG", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x01, 0x0b}),
		)),
	)
}

func TestTypedFunctionReferencePublicCallBoundary(t *testing.T) {
	compiled := stagedTypedStorageCompile(t, typedCallBoundaryModule())
	store := newReferenceStore(false)
	in, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatalf("instantiate staged typed call module: %v", err)
	}
	defer in.Close()

	f, err := in.Call(context.Background(), "getF")
	if err != nil || len(f) != 1 || f[0].FuncRef().IsNull() {
		t.Fatalf("getF = %v, %v", f, err)
	}
	g, err := in.Call(context.Background(), "getG")
	if err != nil || len(g) != 1 || g[0].FuncRef().IsNull() {
		t.Fatalf("getG = %v, %v", g, err)
	}

	got, err := in.Call(context.Background(), "id", f[0])
	if err != nil || len(got) != 1 || got[0].Bits() != f[0].Bits() {
		t.Fatalf("id(f) = %v, %v; want stable token %#x", got, err, f[0].Bits())
	}
	if _, err := in.Call(context.Background(), "id", g[0]); err == nil || !strings.Contains(err.Error(), "exact structural type") {
		t.Fatalf("id(g) mismatch error = %v", err)
	}
	if _, err := in.Invoke("id", g[0].Bits()); err == nil || !strings.Contains(err.Error(), "exact structural type") {
		t.Fatalf("Invoke id(g) mismatch error = %v", err)
	}

	null, err := in.Call(context.Background(), "id", ValueFuncRef(FuncRef{}))
	if err != nil || len(null) != 1 || !null[0].FuncRef().IsNull() {
		t.Fatalf("nullable id(null) = %v, %v", null, err)
	}
	if _, err := in.Call(context.Background(), "nonNull", ValueFuncRef(FuncRef{})); err == nil || !strings.Contains(err.Error(), "non-null argument") {
		t.Fatalf("nonNull(null) error = %v", err)
	}
}
