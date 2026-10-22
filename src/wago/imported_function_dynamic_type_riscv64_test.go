//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ImportedFunctionDynamicTypeCalls(t *testing.T) {
	types := []byte{2, 0x50, 0, 0x60, 1, 0x7f, 1, 0x7f, 0x4f, 1, 0, 0x60, 1, 0x7f, 1, 0x7f}
	provider := wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	for _, artifact := range []bool{false, true} {
		for _, kind := range []string{"indirect", "reference", "tail-indirect", "tail-reference"} {
			for _, actualSubtype := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/artifact%t/subtype%t", kind, artifact, actualSubtype), func(t *testing.T) {
					imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0)
					body := []byte{0x20, 0}
					if kind == "reference" {
						body = append(body, 0xd2, 0, 0xfb, 22, 1, 0x14, 1)
					} else if kind == "tail-reference" {
						body = append(body, 0xd2, 0, 0xfb, 22, 1, 0x15, 1)
					} else if kind == "tail-indirect" {
						body = append(body, 0x41, 0, 0x13, 1, 0)
					} else {
						body = append(body, 0x41, 0, 0x11, 1, 0)
					}
					body = append(body, 0x0b)
					consumer := wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					rt := NewRuntime()
					defer rt.Close()
					input := provider
					if !actualSubtype {
						input = rv64ReplaceTableStageSection(t, input, 3, wasmtest.Section(3, []byte{1, 0}))
					}
					p := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, fmt.Sprintf("dynamic-import-provider-%t", actualSubtype), input, artifact), nil)
					defer p.Close()
					target, e := p.ExportedFunc("f")
					if e != nil {
						t.Fatal(e)
					}
					in := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "dynamic-import-"+kind, consumer, artifact), NewImports().Function("env", "f", target))
					defer in.Close()
					descriptor := in.funcRefDescs[40:80]
					if _, ok := rv64PublishedDescriptor(in, uint64(offHeapSlicePtr(descriptor))); !ok {
						t.Fatal("actual-type import wrapper lacks canonical physical proof")
					}
					if actualSubtype {
						key := binary.LittleEndian.Uint64(descriptor[8:])
						binary.LittleEndian.PutUint64(descriptor[8:], in.c.funcTypeKey(0))
						if _, ok := rv64PublishedDescriptor(in, uint64(offHeapSlicePtr(descriptor))); ok {
							t.Fatal("producer type replaced by imported declaration")
						}
						binary.LittleEndian.PutUint64(descriptor[8:], key)
					}
					actual, actualTypes, ok := instanceFuncrefExactType(in, uint64(offHeapSlicePtr(descriptor)))
					want, e := p.c.functionRefExactType(0)
					if !ok || e != nil || !valueTypeEquivalent(actual, actualTypes, want, p.c.Types) {
						t.Fatal("canonical token type lost producer declaration")
					}
					p.Close()
					for i := 0; i < 3; i++ {
						got, e := in.Invoke("run", 99)
						if !actualSubtype {
							if e == nil {
								t.Fatal("actual parent accepted as child", got)
							}
							continue
						}
						if e != nil || len(got) != 1 || got[0] != 99 {
							t.Fatalf("dynamic imported child call=%v,%v", got, e)
						}
					}
				})
			}
		}
	}
}
