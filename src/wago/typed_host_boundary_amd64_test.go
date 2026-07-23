//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

func typedHostBoundaryModule() []byte {
	i32 := []byte{0x7f}
	ref0 := encodedNullableIndexedRef(0)
	nonNullRef0 := append([]byte{0x64}, wasmtest.ULEB(0)...)
	nonNullRef1 := append([]byte{0x64}, wasmtest.ULEB(1)...)
	types := [][]byte{
		encodedFuncType(nil, nil),
		encodedFuncType([][]byte{i32}, [][]byte{i32}),
		encodedFuncType([][]byte{ref0}, [][]byte{ref0}),
		encodedFuncType(nil, [][]byte{nonNullRef0}),
		encodedFuncType(nil, [][]byte{nonNullRef1}),
	}
	importHost := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	importHost = append(importHost, 0x00)
	importHost = append(importHost, wasmtest.ULEB(2)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importHost)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2),
			wasmtest.ULEB(3), wasmtest.ULEB(4),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callHost", 0, 3),
			wasmtest.ExportEntry("getF", 0, 4),
			wasmtest.ExportEntry("getG", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(1, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x01, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x02, 0x0b}),
		)),
	)
}

func TestTypedFunctionReferenceHostBoundary(t *testing.T) {
	compiled := stagedTypedStorageCompile(t, typedHostBoundaryModule())
	store := newReferenceStore(false)
	var seen uint64
	var override uint64
	host := HostFunc(func(_ HostModule, params, results []uint64) {
		seen = params[0]
		results[0] = params[0]
		if override != 0 {
			results[0] = override
		}
	})
	in, err := instantiateCore(compiled, InstantiateOptions{Imports: Imports{"env.host": host}, store: store})
	if err != nil {
		t.Fatalf("instantiate staged typed host module: %v", err)
	}
	defer in.Close()

	f, err := in.Call(context.Background(), "getF")
	if err != nil || len(f) != 1 {
		t.Fatalf("getF = %v, %v", f, err)
	}
	g, err := in.Call(context.Background(), "getG")
	if err != nil || len(g) != 1 {
		t.Fatalf("getG = %v, %v", g, err)
	}

	got, err := in.Call(context.Background(), "callHost", f[0])
	if err != nil || len(got) != 1 || got[0].Bits() != f[0].Bits() || seen != f[0].Bits() {
		t.Fatalf("callHost(f) = %v, %v seen=%#x; want token %#x", got, err, seen, f[0].Bits())
	}

	override = g[0].Bits()
	if _, err := in.Call(context.Background(), "callHost", f[0]); err == nil || !strings.Contains(err.Error(), "result 0 does not match its exact structural type") {
		t.Fatalf("mismatched typed host result error = %v", err)
	}

	override = 0
	null, err := in.Call(context.Background(), "callHost", ValueFuncRef(FuncRef{}))
	if err != nil || len(null) != 1 || !null[0].FuncRef().IsNull() {
		t.Fatalf("callHost(null) = %v, %v", null, err)
	}
}
