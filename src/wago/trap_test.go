//go:build linux && (amd64 || riscv64)

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestInvokeTrapError(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boom", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))), // unreachable; end
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	_, err = in.Invoke("boom")
	var te *TrapError
	if !errors.As(err, &te) || te.Code != TrapUnreachable {
		t.Fatalf("Invoke trap = %v; want *TrapError with TrapUnreachable", err)
	}
}

func TestRecursiveStackExhaustionTrapsCleanly(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))), // call self; end
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	_, err = in.Invoke("recurse")
	var te *TrapError
	if !errors.As(err, &te) || te.Code != TrapStackFenceBreached {
		t.Fatalf("recursive exhaustion trap = %v; want *TrapError with TrapStackFenceBreached", err)
	}
}

func TestExportedNamesAndMustCompile(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x00, 0x41, 0x07, 0x0b})), // global g: i32 const 7
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("zed", 0, 0),
			wasmtest.ExportEntry("abe", 0, 0),
			wasmtest.ExportEntry("g", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x0b}))),
	)
	c := MustCompile(mod)
	if got := c.ExportedFunctions(); len(got) != 2 || got[0] != "abe" || got[1] != "zed" {
		t.Fatalf("ExportedFunctions = %v; want sorted [abe zed]", got)
	}
	if got := c.ExportedGlobals(); len(got) != 1 || got[0] != "g" {
		t.Fatalf("ExportedGlobals = %v; want [g]", got)
	}
}
