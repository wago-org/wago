//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func TestRV64ManagedFunctionExceptionExecutionAndArtifacts(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, artifact := range []bool{false} {
			for _, kind := range []string{"provider", "local", "operand"} {
				t.Run(fmt.Sprintf("profile%d/artifact%t/%s", profile, artifact, kind), func(t *testing.T) {
					cfg := rv64I31Config()
					features := cfg.frontendFeatures()
					features.ExceptionHandling = true
					features.ExceptionReferences = true
					c, err := compileWithFrontendFeatures(cfg, rv64OwnedFunctionExceptionFixture(kind), features)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()

					rt := NewRuntime()
					defer rt.Close()
					in, err := instantiateCore(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 512, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = in.Invoke("save"); err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 5; i++ {
						if err = in.CollectGC(); err != nil {
							t.Fatal(err)
						}
						got, err := in.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatalf("run %v %v", got, err)
						}
						if kind != "provider" {
							got, err = in.Invoke("held")
							if err != nil || len(got) != 1 || got[0] != 42 {
								t.Fatalf("held %v %v", got, err)
							}
						}
					}
					state := in.managedFunctionCollector(in.gcInvocationDomain())
					if len(state.envs) != 1 {
						t.Fatalf("Envs=%d", len(state.envs))
					}
					if err = in.Close(); err != nil {
						t.Fatal(err)
					}
					if in.hasPhysicalResources() || len(state.envs) != 0 {
						t.Fatalf("closed self cycle survived: physical%t Envs%d pending%d", in.hasPhysicalResources(), len(state.envs), state.graph.pendingReclaims.Load())
					}
				})
			}
		}
	}
}

func TestRV64ManagedFunctionExceptionForeignExecutionAndArtifacts(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, artifact := range []bool{false} {
			t.Run(fmt.Sprintf("profile%d/artifact%t", profile, artifact), func(t *testing.T) {
				cfg := rv64I31Config()
				features := cfg.frontendFeatures()
				features.ExceptionHandling = true
				features.ExceptionReferences = true
				compile := func(kind string) *Compiled {
					c, err := compileWithFrontendFeatures(cfg, rv64OwnedFunctionExceptionFixture(kind), features)
					if err != nil {
						t.Fatal(err)
					}
					original := c
					t.Cleanup(func() { original.Close() })

					return c
				}
				rt := NewRuntime()
				defer rt.Close()
				options := InstantiateOptions{store: rt.refStore, GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 512, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}}
				producer, err := instantiateCore(compile("provider"), options)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				target, err := producer.ExportedFunc("throw")
				if err != nil {
					t.Fatal(err)
				}
				tag, err := producer.ExportedTag("tag")
				if err != nil {
					t.Fatal(err)
				}
				options.Imports = NewImports().Function("env", "throw", target).Tag("env", "tag", tag)
				consumer, err := instantiateCore(compile("consumer"), options)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if _, err = producer.Invoke("save"); err != nil {
					t.Fatal(err)
				}
				if _, err = consumer.Invoke("save"); err != nil {
					t.Fatal(err)
				}
				if err = producer.Close(); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 5; i++ {
					if err = consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					got, err := consumer.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatalf("retained foreign call %v %v", got, err)
					}
				}
				if !producer.hasPhysicalResources() {
					t.Fatal("closed foreign function producer lost")
				}
				if err = consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.hasPhysicalResources() || consumer.hasPhysicalResources() {
					t.Fatalf("closed foreign cycle retained producer%t consumer%t", producer.hasPhysicalResources(), consumer.hasPhysicalResources())
				}
			})
		}
	}
}

func TestRV64ManagedFunctionExceptionCrossStoreNumericOwner(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			cfg := rv64I31Config()
			features := cfg.frontendFeatures()
			features.ExceptionHandling, features.ExceptionReferences = true, true
			compile := func(source []byte) *Compiled {
				original, err := compileWithFrontendFeatures(cfg, source, features)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { original.Close() })

				return original

			}
			producerCode := compile(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b})))))
			consumerCode := compile(rv64OwnedFunctionExceptionFixture("import"))
			rtA, rtB := NewRuntime(), NewRuntime()
			defer rtA.Close()
			defer rtB.Close()
			producer, err := instantiateCore(producerCode, InstantiateOptions{store: rtA.refStore})
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			target, err := producer.ExportedFunc("target")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{store: rtB.refStore, Imports: NewImports().Function("env", "target", target), GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 512, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if _, err = consumer.Invoke("save"); err != nil {
				t.Fatal(err)
			}
			producer.Close()
			for range 3 {
				if err = consumer.CollectGC(); err != nil {
					t.Fatal(err)
				}
				got, err := consumer.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatalf("foreign numeric call=%v %v", got, err)
				}
			}
			consumer.Close()
			if producer.hasPhysicalResources() || consumer.hasPhysicalResources() {
				t.Fatal("cross-store numeric capture retained a closed owner")
			}
			managedCode := compile(rv64OwnedFunctionExceptionFixture("provider"))
			managed, err := instantiateCore(managedCode, InstantiateOptions{store: rtA.refStore})
			if err != nil {
				t.Fatal(err)
			}
			defer managed.Close()
			managedTarget, err := managed.ExportedFunc("target")
			if err != nil {
				t.Fatal(err)
			}
			blocked, err := instantiateCore(consumerCode, InstantiateOptions{store: rtB.refStore, Imports: NewImports().Function("env", "target", managedTarget)})
			if blocked != nil || err == nil || !strings.Contains(err.Error(), "same Runtime") {
				if blocked != nil {
					blocked.Close()
				}
				t.Fatalf("foreign managed owner bypassed store qualification: %v", err)
			}
		})
	}
}

func TestRV64ManagedFunctionCrossStoreMutableRelay(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("private%t", private), func(t *testing.T) {
			cfg := rv64I31Config()
			features := cfg.frontendFeatures()
			features.ExceptionHandling, features.ExceptionReferences = true, true
			managed, err := compileWithFrontendFeatures(cfg, rv64OwnedFunctionExceptionFixture("provider"), features)
			if err != nil {
				t.Fatal(err)
			}
			defer managed.Close()
			tableImport := append(wasmtest.Name("env"), wasmtest.Name("target")...)
			tableImport = append(tableImport, 1, 0x70, 1, 1, 1)
			relaySource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(tableImport)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x11, 0, 0, 0x0b}))))
			relay, err := compileWithFrontendFeatures(cfg, relaySource, features)
			if err != nil {
				t.Fatal(err)
			}
			defer relay.Close()
			storeA, storeB := newReferenceStore(private), newReferenceStore(false)
			defer storeA.closeRuntime()
			defer storeB.closeRuntime()
			table, err := NewTable(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			options := InstantiateOptions{Imports: NewImports().Table("env", "target", table), store: storeA}
			first, err := instantiateCore(relay, options)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			options.store = storeB
			second, err := instantiateCore(relay, options)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			blocked, err := instantiateCore(managed, InstantiateOptions{store: storeA})
			if blocked != nil || err == nil || !strings.Contains(err.Error(), "mutable funcref table") {
				if blocked != nil {
					blocked.Close()
				}
				t.Fatalf("managed owner after foreign relay=%v", err)
			}
			second.Close()
			owner, err := instantiateCore(managed, InstantiateOptions{store: storeA})
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			blocked, err = instantiateCore(relay, options)
			if blocked != nil || err == nil || !strings.Contains(err.Error(), "reference store") {
				if blocked != nil {
					blocked.Close()
				}
				t.Fatalf("foreign relay after managed owner=%v", err)
			}
		})
	}
}
