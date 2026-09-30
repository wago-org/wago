package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGlobalNullabilityDirectAndExported(t *testing.T) {
	for _, heap := range []byte{0x70, 0x6f} {
		for _, nullable := range []bool{true, false} {
			t.Run(fmt.Sprintf("heap=%x/nullable=%t", heap, nullable), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
				if !nullable {
					requireCompleteCore3Backend(t)
					cfg = cfg.WithCoreFeatures(CoreFeaturesV3)
				}
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				typ := []byte{heap}
				if !nullable {
					typ = []byte{0x64, heap}
				}
				init := []byte{0xd0, heap, 0x0b}
				globalIndex := uint32(0)
				imports := NewImports()
				sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
				if heap == 0x70 {
					init = []byte{0xd2, 0, 0x0b}
				} else if !nullable {
					seed, err := rt.NewExternRefGlobal(issueExternref(t, rt, "non-null initializer"), false)
					if err != nil {
						t.Fatal(err)
					}
					defer seed.Close()
					// Give the internal host fixture its exact type before importing it.
					seed.owner.valueType = ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
					seed.owner.hasValueType = true
					imports.Global("env", "seed", seed)
					entry := append(wasmtest.Name("env"), wasmtest.Name("seed")...)
					entry = append(entry, 3, 0x64, heap, 0)
					sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
					init = []byte{0x23, 0, 0x0b}
					globalIndex = 1
				}
				global := append(append(typ, 1), init...)
				sections = append(sections,
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(6, wasmtest.Vec(global)),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, globalIndex), wasmtest.ExportEntry("f", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec([]byte{4, 0, 0x41, 42, 0x0b})),
				)
				c, err := Compile(cfg, wasmtest.Module(sections...))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				variants := []*Compiled{c}
				if nullable {
					loaded := publicArtifactRoundTrip(t, c)
					defer loaded.Close()
					variants = append(variants, loaded)
				}
				for _, compiled := range variants {
					module, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer module.Close()
					in, err := rt.Instantiate(context.Background(), module, WithImports(imports))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					null := ValueExternRef(NullExternRef())
					if heap == 0x70 {
						null = ValueFuncRef(NullFuncRef())
					}
					for _, exported := range []bool{false, true} {
						if exported {
							if _, err := in.ExportedGlobalObject("g"); err != nil {
								t.Fatal(err)
							}
						}
						if (in.globalCells[globalIndex].owner != nil) != exported {
							t.Fatalf("exported=%t: unexpected global ownership path", exported)
						}
						before, err := in.GlobalValue("g")
						if err != nil {
							t.Fatal(err)
						}
						err = in.SetGlobalValue("g", null)
						if (err == nil) != nullable {
							t.Fatalf("exported=%t: null write error=%v, nullable=%t", exported, err, nullable)
						}
						after, err := in.GlobalValue("g")
						if err != nil {
							t.Fatal(err)
						}
						if nullable {
							if after.Bits() != 0 {
								t.Fatalf("nullable global contains %#x", after.Bits())
							}
						} else if before.Bits() == 0 || after.Bits() != before.Bits() {
							t.Fatalf("rejected write changed %#x to %#x", before.Bits(), after.Bits())
						}
					}
				}
			})
		}
	}
}
