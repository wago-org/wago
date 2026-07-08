//go:build linux && amd64

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func passiveDataModule() []byte {
	// (func $init (param i32 i32 i32) local.get 0; local.get 1; local.get 2; memory.init 0 0)
	initBody := []byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x08, 0x00, 0x00, 0x0b}
	// (func $drop data.drop 0)
	dropBody := []byte{0xfc, 0x09, 0x00, 0x0b}

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, append(wasmtest.ULEB(2), 0x00, 0x01)),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}), // one min-1-page memory
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("drop", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(initBody), wasmtest.Code(dropBody))),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x01}, append(wasmtest.ULEB(5), []byte("hello")...)...))),
	)
}

func TestPassiveDataMemoryInitAndDrop(t *testing.T) {
	c, err := Compile(passiveDataModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.boundsMode != BoundsChecksSignalsBased {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		c, err = Load(blob)
		if err != nil {
			t.Fatalf("Load compiled blob: %v", err)
		}
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.Invoke("init", I32(10), I32(1), I32(3)); err != nil {
		t.Fatalf("memory.init: %v", err)
	}
	if got := string(in.Memory().Bytes()[10:13]); got != "ell" {
		t.Fatalf("memory.init copied %q, want ell", got)
	}
	if _, err := in.Invoke("init", I32(20), I32(0), I32(5)); err != nil {
		t.Fatalf("second memory.init before drop: %v", err)
	}
	if got := string(in.Memory().Bytes()[20:25]); got != "hello" {
		t.Fatalf("second memory.init copied %q, want hello", got)
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatalf("data.drop: %v", err)
	}
	if _, err := in.Invoke("init", I32(30), I32(0), I32(1)); err == nil {
		t.Fatal("memory.init after data.drop succeeded; want trap")
	}
	if _, err := in.Invoke("init", I32(30), I32(0), I32(0)); err != nil {
		t.Fatalf("zero-length memory.init after data.drop: %v", err)
	}
}

func TestPassiveDataMemoryInitBoundsTrap(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  int32
		src  int32
		n    int32
	}{
		{name: "source", dst: 0, src: 4, n: 2},
		{name: "destination", dst: 65535, src: 0, n: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(MustCompile(passiveDataModule()), nil)
			if err != nil {
				t.Fatalf("Instantiate: %v", err)
			}
			defer in.Close()
			_, err = in.Invoke("init", I32(tc.dst), I32(tc.src), I32(tc.n))
			if err == nil {
				t.Fatal("memory.init succeeded; want trap")
			}
			if !strings.Contains(err.Error(), "trap") {
				t.Fatalf("memory.init error = %v, want trap", err)
			}
		})
	}
}
