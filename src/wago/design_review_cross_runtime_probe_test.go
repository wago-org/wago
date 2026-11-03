//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Design-review probe: a non-GC producer that merely declares externref storage
// exports a scalar function to a consumer outside its Runtime.
func TestDesignReviewExternStorageProducerCrossRuntimeImport(t *testing.T) {
	storage := map[string][]byte{
		"none":   nil,
		"global": wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		"table":  wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})),
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("f")...), 0, 0)
	consumer := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	for _, kind := range []string{"none", "global", "table"} {
		for _, consumerRuntime := range []string{"standalone", "other-runtime"} {
			t.Run(kind+"/"+consumerRuntime, func(t *testing.T) {
				sections := [][]byte{wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(3, wasmtest.Vec([]byte{0}))}
				if storage[kind] != nil && kind == "table" {
					sections = append(sections, storage[kind])
				}
				if storage[kind] != nil && kind == "global" {
					sections = append(sections, storage[kind])
				}
				sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))))
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
				pc, err := Compile(cfg, wasmtest.Module(sections...))
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				r1 := NewRuntime()
				p, err := instantiateCore(pc, InstantiateOptions{store: r1.refStore})
				if err != nil {
					t.Fatal(err)
				}
				defer p.Close()
				f, err := p.ExportedFunc("f")
				if err != nil {
					t.Fatal(err)
				}
				cc, err := Compile(cfg, consumer)
				if err != nil {
					t.Fatal(err)
				}
				defer cc.Close()
				var store *referenceStore
				if consumerRuntime == "other-runtime" {
					store = NewRuntime().refStore
				}
				t.Logf("producer dynamic=%t", p.executionFlags.Load()&executionFlagDynamicGCDomain != 0)
				c, err := instantiateCore(cc, InstantiateOptions{store: store, Imports: testImports("p.f", f)})
				if err != nil {
					t.Fatalf("consumer instantiation: %v", err)
				}
				defer c.Close()
				out, err := c.Invoke("run")
				if err != nil || len(out) != 1 || out[0] != 7 {
					t.Fatalf("run = %v, %v", out, err)
				}
			})
		}
	}
}
