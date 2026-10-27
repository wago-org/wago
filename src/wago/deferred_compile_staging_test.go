//go:build amd64 || arm64

package wago

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDeferredCompilerStagingLifecycle(t *testing.T) {
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
		if c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed {
			t.Fatal("compile mapped deferred code before instantiation")
		}
		snapshot := c.executionView()
		if len(c.code) == 0 || unsafe.SliceData(c.code) != unsafe.SliceData(snapshot.code) {
			t.Fatal("published views do not share heap staging")
		}
		if _, err := c.MarshalBinary(); err != nil {
			t.Fatal(err)
		}
		if c.codeCache.mem != nil {
			t.Fatal("serialization mapped deferred code")
		}
		if activate {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if c.codeCache.mem == nil || c.codeCache.base == 0 || !c.codeCache.sealed {
				t.Fatal("activation did not map and seal deferred code")
			}
			if unsafe.SliceData(c.code) != unsafe.SliceData(c.codeCache.mem) || unsafe.SliceData(snapshot.code) != unsafe.SliceData(c.codeCache.mem) {
				t.Fatal("activation left a published view on heap staging")
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
			t.Fatal("Close retained compiler storage")
		}
	}
}
