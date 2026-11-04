//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func immediateWarmCompile(t *testing.T, immediate, artifact bool) *Compiled {
	t.Helper()

	body := []byte{0x41, 1, 0x0b}
	if immediate {
		body = []byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0x14, 0x6c, 0x0b}
	}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	return c

}

func TestRV64ImmediateReferenceWarmAllocations(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("immediate%t/artifact%t", immediate, artifact), func(t *testing.T) {
				c := immediateWarmCompile(t, immediate, artifact)
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.gc != nil || c.nativeGCABIRequirement() != 0 {
					t.Fatal("collector-free module acquired a collector/ABI")
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				alloc := testing.AllocsPerRun(100, func() {
					got, err := session.Invoke0()
					if err != nil || len(got) != 1 || got[0] != 1 {
						panic(fmt.Sprintf("immediate warm result=%v,%v", got, err))
					}
				})
				t.Logf("native code=%dB; native GC ABI=0; collector=nil; warm Go allocations=%g", len(c.code), alloc)
				if alloc != 0 {
					t.Fatalf("warm Go allocations=%g, want0", alloc)
				}
			})
		}
	}
}
