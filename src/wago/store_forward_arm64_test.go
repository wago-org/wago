//go:build (darwin || linux) && arm64

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestArm64LinearStoreLoadForwardingSemantics(t *testing.T) {
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}
	results := []wasm.ValType{wasm.I64}
	exact := []byte{
		0x20, 0x00, // local.get address
		0x20, 0x02, // local.get value
		0x37, 0x03, 0x00, // i64.store align=8 offset=0
		0x20, 0x00, // local.get same address
		0x29, 0x03, 0x00, // i64.load align=8 offset=0
		0x0b,
	}
	changedAddress := []byte{
		0x20, 0x00, // local.get first address
		0x20, 0x02, // local.get value
		0x37, 0x03, 0x00, // i64.store
		0x20, 0x01, // local.get second address
		0x21, 0x00, // local.set first address (must invalidate forwarding)
		0x20, 0x00,
		0x29, 0x03, 0x00, // load second address
		0x0b,
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("exact", 0, 0),
			wasmtest.ExportEntry("changedAddress", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(exact), wasmtest.Code(changedAddress))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	const stored = uint64(0x1122334455667788)
	got, err := in.Invoke("exact", I32(0), I32(16), stored)
	if err != nil {
		t.Fatalf("exact: %v", err)
	}
	if len(got) != 1 || got[0] != stored {
		t.Fatalf("exact result = %x, want %x", got, stored)
	}

	const untouched = uint64(0x8877665544332211)
	binary.LittleEndian.PutUint64(in.Memory().Bytes()[16:24], untouched)
	got, err = in.Invoke("changedAddress", I32(0), I32(16), stored)
	if err != nil {
		t.Fatalf("changedAddress: %v", err)
	}
	if len(got) != 1 || got[0] != untouched {
		t.Fatalf("changed-address result = %x, want untouched %x", got, untouched)
	}
}
