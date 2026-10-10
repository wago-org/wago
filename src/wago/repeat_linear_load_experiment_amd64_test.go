//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// TestAdjacentRepeatLoadExec checks the opt-in backend and the default path
// with a duplicate pair, an intervening store, a local mutation, and an OOB
// trap. Run it in separate processes with the experiment flag off and on.
func TestAdjacentRepeatLoadExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	load := []byte{0x20, 0x00, 0x28, 0x02, 0x00}
	pair := append(append(append([]byte{}, load...), load...), 0x6a, 0x0b)
	storeBetween := append([]byte{}, load...)
	storeBetween = append(storeBetween, 0x20, 0x00, 0x41, 0x09, 0x36, 0x02, 0x00)
	storeBetween = append(storeBetween, load...)
	storeBetween = append(storeBetween, 0x6a, 0x0b)
	localSet := append([]byte{}, load...)
	localSet = append(localSet, 0x41, 0x04, 0x21, 0x00)
	localSet = append(localSet, load...)
	localSet = append(localSet, 0x6a, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("pair", 0, 0),
			wasmtest.ExportEntry("storeBetween", 0, 1),
			wasmtest.ExportEntry("localSet", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(pair), wasmtest.Code(storeBetween), wasmtest.Code(localSet))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x04, 0x07, 0x00, 0x00, 0x00})),
	)
	c := MustCompile(module)
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	check := func(name string, arg uint64, want uint64) {
		t.Helper()
		got, err := in.Invoke(name, arg)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("%s(%d)=%v, %v; want %d", name, arg, got, err, want)
		}
	}
	check("pair", 0, 14)
	check("storeBetween", 0, 16)
	check("pair", 0, 18)
	check("localSet", 0, 9)
	if _, err := in.Invoke("pair", 65535); err == nil {
		t.Fatal("OOB duplicate load did not trap")
	}
}
