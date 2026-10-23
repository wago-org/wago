package wago

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMappedCompilerStagingLifecycle(t *testing.T) {
	body := []byte{0x20, 0}
	for i := 0; i < 512; i++ {
		body = append(body, 0x41, 1, 0x6a)
	}
	body = append(body, 0x0b)
	funcs := make([][]byte, 64)
	codes := make([][]byte, 64)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(0)
		codes[i] = wasmtest.Code(body)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	for _, activate := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1), bytes.Clone(module))
		if err != nil {
			t.Fatal(err)
		}
		if c.codeCache.mem == nil || c.codeCache.base == 0 || c.codeCache.sealed {
			t.Fatal("large compile did not retain an unsealed mapped image")
		}
		base := c.codeCache.base
		snapshot := c.executionView()
		if uintptr(unsafe.Pointer(unsafe.SliceData(c.code))) != base || uintptr(unsafe.Pointer(unsafe.SliceData(snapshot.code))) != base {
			t.Fatal("published views do not share mapped staging")
		}
		if activate {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !c.codeCache.sealed || c.codeCache.base != base {
				t.Fatal("activation did not seal the existing mapping")
			}
			got, err := in.Invoke("run", 7)
			if err != nil || len(got) != 1 || got[0] != 519 {
				t.Fatalf("run=%v/%v", got, err)
			}
			if err = in.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err = c.Close(); err != nil {
			t.Fatal(err)
		}
		if c.codeCache.mem != nil || c.code != nil || snapshot.code != nil {
			t.Fatal("Close retained mapped compiler storage")
		}
	}
}
