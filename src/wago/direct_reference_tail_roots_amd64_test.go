//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"testing"
)

func TestDirectReferenceABITailChainRoots(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/direct-reference-tail-roots.wasm")
	if err != nil {
		t.Fatal(err)
	}
	data = paddedReferenceTailRoots(t, data)
	for _, enabled := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("enabled=%t/workers=%d", enabled, workers), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(workers).WithOptimization("inline", false).WithOptimization("direct-reference-abi", enabled)
				c, err := Compile(cfg, data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if a := c.GCNativeRootAdmission(); !a.Required || !a.Exact {
					t.Fatalf("missing exact roots: %+v", a)
				}
				for _, profile := range directReferenceABIGCProfiles() {
					in, err := Instantiate(c, InstantiateOptions{GC: profile.gc})
					if err != nil {
						t.Fatal(err)
					}
					func() {
						defer in.Close()
						for _, name := range []string{"run", "run-indirect", "run-reference", "run-pair", "run-numeric", "run-numeric-direct", "run-numeric-indirect"} {
							for range 3 {
								if _, err := in.Invoke("touch-table"); err != nil {
									t.Fatal(err)
								}
								before := in.gc.Stats().FullCollections
								got, err := in.Invoke(name)
								expected := uint64(40)
								if name == "run-pair" {
									expected = 63
								}
								if err != nil || len(got) != 1 || got[0] != expected {
									t.Fatalf("%s/%s=%v,%v want[%d]", profile.name, name, got, err, expected)
								}
								if profile.gc.CollectEveryAlloc && in.gc.Stats().FullCollections-before < 3 {
									t.Fatalf("%s did not collect at each allocation", name)
								}
								if err = in.CollectGC(); err != nil {
									t.Fatal(err)
								}
							}
						}
					}()
				}
			})
		}
	}
}

// More than16KiB of bytecode and13 functions qualify four hint workers.
// Pad the target after its empty local declaration, preserving all operations.
func paddedReferenceTailRoots(t *testing.T, data []byte) []byte {
	t.Helper()
	r := wasm.ReaderFrom(data)
	header, err := r.Bytes(8)
	if err != nil {
		t.Fatal(err)
	}
	out := append([]byte(nil), header...)
	for r.BytesLeft() != 0 {
		id, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		size, err := r.U32()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := r.Bytes(int(size))
		if err != nil {
			t.Fatal(err)
		}
		if id != 10 {
			out = append(out, wasmtest.Section(id, payload)...)
			continue
		}
		body := wasm.ReaderFrom(payload)
		count, err := body.U32()
		if err != nil {
			t.Fatal(err)
		}
		var functions [][]byte
		for i := uint32(0); i < count; i++ {
			n, err := body.U32()
			if err != nil {
				t.Fatal(err)
			}
			code, err := body.Bytes(int(n))
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				if len(code) == 0 || code[0] != 0 {
					t.Fatal("target must have no declared locals")
				}
				padded := make([]byte, 1, 1+(16<<10)+len(code))
				for range 16 << 10 {
					padded = append(padded, 0x01)
				}
				code = append(padded, code[1:]...)
			}
			functions = append(functions, append(wasmtest.ULEB(uint32(len(code))), code...))
		}
		out = append(out, wasmtest.Section(10, wasmtest.Vec(functions...))...)
	}
	return out
}

func TestDirectReferenceABIForeignTailRoots(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/direct-reference-foreign-tail.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("direct-reference-abi", enabled)
			providerCode, err := Compile(cfg, gcCrossInstanceProviderModule())
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			consumerCode, err := Compile(cfg, data)
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := gcTailStressConfig(GCProfileThroughput)
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			export, err := provider.ExportedFunc("retain")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.retain", export)})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc != consumer.gc || instanceNativeGCDomainID(provider) == 0 || instanceNativeGCDomainID(provider) != instanceNativeGCDomainID(consumer) || provider.jm.LinMemBase() == consumer.jm.LinMemBase() {
				t.Fatal("fixture must share GC with distinct instance bases")
			}
			before := consumer.gc.Stats().FullCollections
			got, err := consumer.Invoke("run", 23)
			if err != nil || len(got) != 1 || got[0] != 40 {
				t.Fatalf("run=%v,%v want[40]", got, err)
			}
			if consumer.gc.Stats().FullCollections-before < 3 {
				t.Fatal("fixture did not force collection")
			}
		})
	}
}
