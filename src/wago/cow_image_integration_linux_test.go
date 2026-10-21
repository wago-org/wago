//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func cowIntegratedModule() []byte {
	segment := func(offset int32, payload string) []byte {
		d := []byte{0, 0x41}
		d = append(d, wasmtest.SLEB32(offset)...)
		d = append(d, 0x0b)
		d = append(d, wasmtest.ULEB(uint32(len(payload)))...)
		return append(d, payload...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x40, 0, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec(
			segment(0, "ABC"), segment(1, "xy"), segment(65534, "QR"),
		)),
	)
}

// This exercises the actual Wago instance path, including ordered overlapping
// active segments, private writes, growth, close, and fresh instantiation.
func TestCOWImageIntegratedInstances(t *testing.T) {
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), cowIntegratedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if c.codeCache.memoryImage == nil {
		t.Fatal("eligible compiled module did not build a shared image")
	}
	for _, in := range []*Instance{a, b} {
		got := in.Memory().UnsafeBytes()
		if !bytes.Equal(got[:3], []byte("Axy")) || !bytes.Equal(got[65534:65536], []byte("QR")) {
			t.Fatalf("active data initialization = %q / %q", got[:3], got[65534:65536])
		}
	}
	a.Memory().UnsafeBytes()[0] = 'z'
	if b.Memory().UnsafeBytes()[0] != 'A' {
		t.Fatal("private memory write reached sibling")
	}
	result, err := a.Invoke("grow")
	if err != nil || len(result) != 1 || AsI32(result[0]) != 1 {
		t.Fatalf("grow = %v, %v", result, err)
	}
	if got := a.Memory().UnsafeBytes(); len(got) != 2*65536 || got[65536] != 0 {
		t.Fatal("grown page was not zero-filled")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Memory().UnsafeBytes(); got[0] != 'A' || got[65536-1] != 'R' {
		t.Fatal("fresh image inherited an earlier instance write")
	}
}
