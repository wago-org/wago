//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_guardpage && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestRV64ManagedFunctionAggregateExecutionAndArtifacts(t *testing.T) {
	for _, array := range []bool{false, true} {
		mutations := []string{"", "set"}
		if array {
			mutations = append(mutations, "fill", "init")
		}
		for _, mutation := range mutations {
			for _, local := range []bool{false, true} {
				for _, vector := range []bool{false, true} {
					t.Run(fmt.Sprintf("array%t/%s/local%t/vector%t", array, mutation, local, vector), func(t *testing.T) {
						cfg := rv64I31Config()
						c, err := CompileWithConfig(cfg, rv64FunctionStorageFixture(array, local, vector, mutation))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, artifact := range []bool{false, true} {
							run := c
							if artifact {
								blob, err := c.MarshalBinary()
								if err != nil {
									t.Fatal(err)
								}
								run, err = LoadTrustedArtifact(blob)
								if err != nil {
									t.Fatal(err)
								}
								defer run.Close()
							}
							in, err := instantiateCore(run, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 512, VerifyAfterCollect: true}})
							if err != nil {
								t.Fatal(err)
							}
							for i := 0; i < 5; i++ {
								got, err := in.Invoke("run")
								if err != nil || len(got) != 1 || got[0] != 42 {
									t.Fatalf("artifact%t got%v err%v", artifact, got, err)
								}
							}
							if err := in.Close(); err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64ManagedFunctionAggregateForeignExecutionAndArtifacts(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, array := range []bool{false, true} {
			mutations := []string{"", "set"}
			if array {
				mutations = append(mutations, "fill", "init")
			}
			for _, mutation := range mutations {
				for _, local := range []bool{false, true} {
					for _, vector := range []bool{false, true} {
						t.Run(fmt.Sprintf("profile%d/array%t/%s/local%t/vector%t", profile, array, mutation, local, vector), func(t *testing.T) {
							cfg := rv64I31Config()
							c, err := CompileWithConfig(cfg, rv64FunctionStorageInputFixture(array, local, vector, mutation, true))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							producerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x5f, 1, 0x63, 0, 1}, []byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x63, 0})), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("probe")...), 0, 0))), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{3})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 1), wasmtest.ExportEntry("function", 0, 2))), wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 1, 0x1a, 0x10, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 1, 0x0b}))))
							pc, err := CompileWithConfig(cfg, producerSource)
							if err != nil {
								t.Fatal(err)
							}
							defer pc.Close()
							for _, artifact := range []bool{false, true} {
								run, producerRun := c, pc
								if artifact {
									blob, err := c.MarshalBinary()
									if err != nil {
										t.Fatal(err)
									}
									run, err = LoadTrustedArtifact(blob)
									if err != nil {
										t.Fatal(err)
									}
									defer run.Close()
									producerBlob, err := pc.MarshalBinary()
									if err != nil {
										t.Fatal(err)
									}
									producerRun, err = LoadTrustedArtifact(producerBlob)
									if err != nil {
										t.Fatal(err)
									}
									defer producerRun.Close()
								}
								rt := NewRuntime()
								defer rt.Close()
								opts := InstantiateOptions{store: rt.refStore, GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 512, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}}
								var consumer *Instance
								observed := 0
								producerOptions := opts
								producerOptions.Imports = NewImports().Function("env", "probe", slotHostFunc(func(_ HostModule, _ []uint64, out []uint64) {
									state, ok := managedFunctionCollectors.Load(consumer.gc)
									if !ok || len(state.(*managedFunctionCollector).envs) != 1 {
										panic("raw function lost its foreign Env during the call")
									}
									observed++
									out[0] = 42
								}))
								producer, err := instantiateCore(producerRun, producerOptions)
								if err != nil {
									t.Fatal(err)
								}
								desc, ok := producer.localFuncrefDescriptor(0)
								if !ok {
									t.Fatal("no descriptor")
								}
								token, err := rt.refStore.issue(producer, desc)
								if err != nil {
									t.Fatal(err)
								}
								consumer, err = instantiateCore(run, opts)
								if err != nil {
									t.Fatal(err)
								}
								if err = producer.Close(); err != nil {
									t.Fatal(err)
								}
								for i := 0; i < 5; i++ {
									got, err := consumer.Invoke("run", token)
									if err != nil || len(got) != 1 || got[0] != 42 {
										t.Fatalf("artifact%t foreign result %v %v", artifact, got, err)
									}
								}
								if mutation != "init" && observed != 5 {
									t.Fatalf("foreign function probes=%d", observed)
								}
								if err = consumer.Close(); err != nil {
									t.Fatal(err)
								}
								if err = rt.Close(); err != nil {
									t.Fatal(err)
								}
								if err = rt.WaitClosed(context.Background()); err != nil {
									t.Fatal(err)
								}
								if producer.hasPhysicalResources() || consumer.hasPhysicalResources() {
									t.Fatal("Runtime shutdown retained foreign function owners")
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64ManagedFunctionConstructionExecutionAndArtifacts(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile%d/artifact%t", profile, artifact), func(t *testing.T) {
				source := gcConstExprFuncrefModule()
				c, err := CompileWithConfig(rv64I31Config(), source)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					c, err = LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
				}
				in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 512, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				value, ok := managedFunctionCollectors.Load(in.gc)
				if !ok {
					t.Fatal("construction created no managed function state")
				}
				state := value.(*managedFunctionCollector)
				if len(state.envs) != 1 {
					t.Fatalf("construction Envs=%d", len(state.envs))
				}
				if err = in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if _, err = in.Invoke("f"); err != nil {
					t.Fatal(err)
				}
				if err = in.Close(); err != nil {
					t.Fatal(err)
				}
				if in.hasPhysicalResources() || len(state.envs) != 0 || len(state.shadows) != 0 {
					t.Fatalf("construction cycle survived: physical%t Envs%d shadows%d", in.hasPhysicalResources(), len(state.envs), len(state.shadows))
				}
			})
		}
	}
}
