//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadedHostContextModule(imported, growth bool) []byte {
	imports := [][]byte{importEntry("env", "host", 0, 0)}
	if imported {
		imports = append(imports, append(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2), 3, 1, 3))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, wasmtest.Vec([]byte{0}))}
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 3})))
	}
	body := []byte{0x41, 0, 0x20, 0, 0x36, 2, 0, 0x20, 0, 0x10, 0, 0x10, 0, 0x41, 0, 0x28, 2, 0, 0x6a}
	if growth {
		body = append(body, 0x41, 0x80, 0x80, 4, 0x28, 2, 0, 0x6a)
	}
	body = append(body, 0x3f, 0, 0x6a, 0x0b)
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	return wasmtest.Module(sections...)
}

func TestThreadsOwnedHostControlReuseAndConcurrentGrowth(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, growth := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/growth=%t/regabi=%t", imported, growth, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					compiled, e := cfg.Compile(threadedHostContextModule(imported, growth))
					if e != nil {
						t.Fatal(e)
					}
					defer compiled.Close()
					for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
						var in, grower *Instance
						var memory *Memory
						versions := make([]uint64, 0, 2)
						host, e := rt.NewHostFuncRef(func(v int32) int32 {
							versions = append(versions, in.ensurePluginState().nativeContextVersion.Load())
							if growth && len(versions) == 1 {
								done := make(chan error, 1)
								go func() {
									got, e := grower.Invoke("grow", 1)
									if e == nil && (len(got) != 1 || got[0] != 1) {
										e = fmt.Errorf("growth result %v", got)
									}
									if e == nil {
										bytes := memory.UnsafeBytes()
										if len(bytes) != 131072 {
											e = fmt.Errorf("grown length %d", len(bytes))
										} else {
											binary.LittleEndian.PutUint32(bytes, 12)
											binary.LittleEndian.PutUint32(bytes[65536:], 17)
										}
									}
									done <- e
								}()
								select {
								case e := <-done:
									if e != nil {
										panic(HostTrap{Err: e})
									}
								case <-time.After(5 * time.Second):
									panic(HostTrap{Err: fmt.Errorf("concurrent grow stalled")})
								}
							}
							return v
						}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
						if e != nil {
							t.Fatal(e)
						}
						defer host.Close()
						imports := NewImports().Function("env", "host", host)
						if imported {
							memory, e = NewSharedMemory(1, 3)
							if e != nil {
								t.Fatal(e)
							}
							defer memory.Close()
							imports.Memory("env", "memory", memory)
						}
						mod, e := rt.Module(c)
						if e != nil {
							t.Fatal(e)
						}
						defer mod.Close()
						in, e = rt.Instantiate(context.Background(), mod, WithImports(imports))
						if e != nil {
							t.Fatal(e)
						}
						defer in.Close()
						if !in.usesPrivateThreadedExecution() {
							t.Fatal("threaded controls are not private")
						}
						if growth {
							if memory == nil {
								memory, e = in.ExportedMemory("memory")
								if e != nil {
									t.Fatal(e)
								}
							}
							gm, e := rt.Compile(threadsGrowModule(true, 1, 3))
							if e != nil {
								t.Fatal(e)
							}
							defer gm.Close()
							grower, e = rt.Instantiate(context.Background(), gm, WithImports(NewImports().Memory("env", "memory", memory)))
							if e != nil {
								t.Fatal(e)
							}
							defer grower.Close()
						}
						fn, e := in.WasmFunc("run")
						if e != nil {
							t.Fatal(e)
						}
						got, e := fn.Invoke(7)
						want := uint64(15)
						if growth {
							want = 38
						}
						if e != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("guest output %v, %v; want %d", got, e, want)
						}
						if len(versions) != 2 || versions[0] != versions[1] {
							t.Fatalf("unchanged private control rebound between callbacks: %v", versions)
						}
					}
				})
			}
		}
	}
}
