//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcInlineTrustedFixture() []byte {
	types := [][]byte{
		{0x4f, 0, 0x5e, 0x7f, 1},
		{0x4f, 0, 0x5f, 1, 0x6d, 1},
		{0x4f, 0, 0x5f, 1, 0x7f, 1},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x6d, 1, 0x41, 7, 0x41, 1, 0xfb, 6, 0, 0xfb, 0, 1, 0x0b}
	bodies := [][]byte{wasmtest.Code([]byte{0x23, 0, 0xfb, 22, 1, 0xfb, 2, 1, 0, 0xfb, 22, 0, 0x1a, 0x41, 42, 0x0b})}
	for _, value := range [][]byte{{0xd0, 0x6d}, {0x41, 1, 0xfb, 28}, {0xfb, 1, 2}} {
		body := append([]byte{0x23, 0, 0xfb, 22, 1}, value...)
		bodies = append(bodies, wasmtest.Code(append(body, 0xfb, 5, 1, 0, 0x41, 0, 0x0b)))
	}
	funcs, exports := make([][]byte, len(bodies)), make([][]byte, len(bodies))
	for i, name := range []string{"run", "null", "i31", "wrong"} {
		funcs[i] = wasmtest.ULEB(3)
		exports[i] = wasmtest.ExportEntry(name, byte(wasm.ExternFunc), uint32(i))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(6, wasmtest.Vec(global)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}
func TestGCInlineTrustedMutableField(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1), gcInlineTrustedFixture())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}} {
		for _, name := range []string{"valid", "null", "i31", "wrong"} {
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 42 {
				in.Close()
				t.Fatalf("initial run=%v %v", got, err)
			}
			if name != "valid" {
				if _, err = in.Invoke(name); err != nil {
					in.Close()
					t.Fatal(err)
				}
				_, err = in.Invoke("run")
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
					in.Close()
					t.Fatalf("%s got%v want cast failure", name, err)
				}
			}
			if err = in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
