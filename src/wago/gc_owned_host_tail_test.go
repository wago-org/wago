//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func collectorOwnedHostTailSource(kind string, vector bool) []byte {
	signature := []byte{0x60, 1, 0x6e, 1, 0x6e}
	args := []byte{0x20, 0}
	run := []byte{0x41, 42, 0xfb, 0, 0}
	if vector {
		run = append(append([]byte{0xfd, 12}, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}...), run...)
	}
	types := [][]byte{{0x5f, 1, 0x7f, 0}, signature, {0x60, 0, 1, 0x7f}}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 1)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{2, 1, 2})}
	tail := append([]byte(nil), args...)
	if vector {
		tail = append(append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x1a), tail...)
	}
	switch kind {
	case "ordinary":
		tail = append(tail, 0x10, 0)
	case "reference":
		tail = append(tail, 0xd2, 0, 0x15, 1)
	case "indirect":
		tail = append(tail, 0x41, 0, 0x13, 1, 0)
	default:
		tail = append(tail, 0x12, 0)
	}
	tail = append(tail, 0x0b)
	if kind == "indirect" {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("echo", 0, 1))))
	if kind == "reference" {
		sections = append(sections, wasmtest.Section(9, []byte{1, 3, 0, 1, 0}))
	}
	if kind == "indirect" {
		sections = append(sections, wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}))
	}
	run = append(run, 0x41, 7, 0xfb, 0, 0, 0x1a, 0x10, 1)
	run = append(run, 0xfb, 22, 0, 0xfb, 2, 0, 0)
	if vector {
		run = append(run, 0x21, 0, 0xfd, 29, 0, 0x42)
		run = append(run, wasmtest.SLEB64(0x0807060504030201)...)
		run = append(run, 0x52, 0x04, 0x40, 0, 0x0b, 0x20, 0)
	}
	run = append(run, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tail), func() []byte {
		if !vector {
			return wasmtest.Code(run)
		}
		body := append([]byte{1, 1, 0x7f}, run...)
		return append(wasmtest.ULEB(uint32(len(body))), body...)
	}())))
	return wasmtest.Module(sections...)
}

func TestRV64CollectorBearingOwnedHostTailCalls(t *testing.T) {
	for _, kind := range []string{"direct", "reference", "indirect"} {
		for _, vector := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
					t.Run(fmt.Sprintf("%s/vector%t/artifact%t/profile%d", kind, vector, artifact, profile), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						sig := FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}}
						calls := 0
						var active *Instance
						host, err := rt.NewGCHostFuncRef(slotHostFunc(func(_ HostModule, args, results []uint64) {
							calls++
							if args[0] == 0 || args[0]>>32 == 0 {
								panic("collector host received raw reference")
							}
							if active != nil {
								if err := active.CollectGC(); err != nil {
									panic(err)
								}
							}
							copy(results, args)
						}), sig)
						if err != nil {
							t.Fatal(err)
						}
						defer host.Close()
						c := rv64CollectorHostCompile(t, fmt.Sprintf("owned-host-%s-vector%t", kind, vector), collectorOwnedHostTailSource(kind, vector), artifact)
						mod, err := rt.Module(c)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
						in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env", "target", host)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 4096, ForceMajorEveryMinor: true, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						active = in
						for i := 0; i < 3; i++ {
							got, e := in.Invoke("run")
							if e != nil || len(got) != 1 || got[0] != 42 {
								t.Fatalf("owned collector host tail=%v,%v", got, e)
							}
							if e := in.CollectGC(); e != nil {
								t.Fatal(e)
							}
						}
						if calls != 3 {
							t.Fatalf("callback count=%d", calls)
						}
					})
				}
			}
		}
	}
}

func TestRV64CollectorHostTailRetainedClosedProducer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					host, err := rt.NewGCHostFuncRef(slotHostFunc(func(_ HostModule, args, results []uint64) { copy(results, args) }), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
					if err != nil {
						t.Fatal(err)
					}
					defer host.Close()
					makeModule := func() *Module {
						c := rv64CollectorHostCompile(t, fmt.Sprintf("owned-host-direct-vector%t", vector), collectorOwnedHostTailSource("direct", vector), artifact)
						m, e := rt.Module(c)
						if e != nil {
							t.Fatal(e)
						}
						t.Cleanup(func() { m.Close() })
						return m
					}
					opts := WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 4096, ForceMajorEveryMinor: true, VerifyAfterCollect: true})
					producer, err := rt.Instantiate(context.Background(), makeModule(), WithImports(NewImports().Function("env", "target", host)), opts)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					target, err := producer.ExportedFunc("echo")
					if err != nil {
						t.Fatal(err)
					}
					consumer, err := rt.Instantiate(context.Background(), makeModule(), WithImports(NewImports().Function("env", "target", target)), opts)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					producer.Close()
					if producer.resourcesClosed {
						t.Fatal("retained producer physically closed")
					}
					for i := 0; i < 3; i++ {
						got, e := consumer.Invoke("run")
						if e != nil || len(got) != 1 || got[0] != 42 {
							t.Fatalf("closed-producer collector host tail=%v,%v", got, e)
						}
					}
					consumer.Close()
					if !producer.resourcesClosed {
						t.Fatal("producer did not release after consumer close")
					}
					if e := host.Close(); e != nil {
						t.Fatal(e)
					}
				})
			}
		}
	}
}

func TestRV64CollectorHostTailWarmAllocations(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			baseline := float64(-1)
			for _, kind := range []string{"ordinary", "direct", "reference", "indirect"} {
				t.Run(fmt.Sprintf("%s/vector%t/artifact%t", kind, vector, artifact), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					host, err := rt.NewGCHostFuncRef(slotHostFunc(func(_ HostModule, args, results []uint64) { copy(results, args) }), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
					if err != nil {
						t.Fatal(err)
					}
					defer host.Close()
					c, e := NewRuntimeConfig().Compile(collectorOwnedHostTailSource(kind, vector))
					if e != nil {
						t.Fatal(e)
					}
					defer c.Close()
					if artifact {
						blob, e := c.MarshalBinary()
						if e != nil {
							t.Fatal(e)
						}
						c, e = LoadTrustedArtifact(blob)
						if e != nil {
							t.Fatal(e)
						}
						defer c.Close()
					}
					mod, e := rt.Module(c)
					if e != nil {
						t.Fatal(e)
					}
					defer mod.Close()
					in, e := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env", "target", host)), WithGC(GCConfig{DisableCollection: true}))
					if e != nil {
						t.Fatal(e)
					}
					defer in.Close()
					fn, e := in.WasmFunc("run")
					if e != nil {
						t.Fatal(e)
					}
					session, e := fn.OpenSession()
					if e != nil {
						t.Fatal(e)
					}
					defer session.Close()
					alloc := testing.AllocsPerRun(20, func() {
						got, e := session.Invoke()
						if e != nil || len(got) != 1 || got[0] != 42 {
							panic(fmt.Sprintf("warm host GC tail=%v,%v", got, e))
						}
					})
					t.Logf("code=%dB public-session Go allocations=%g", len(c.code), alloc)
					if kind == "ordinary" {
						baseline = alloc
					} else if kind == "direct" && alloc > baseline {
						t.Fatalf("host tail allocations=%g ordinary control=%g", alloc, baseline)
					}
					if kind == "reference" || kind == "indirect" {
						session.Close()
						gate := in.lockInvocation(0)
						defer gate.unlockInvocation()
						entry, e := in.beginNativeEntry()
						if e != nil {
							t.Fatal(e)
						}
						defer in.unlockNativeEntry(entry)
						generations := entry.tables.targets.tailActive
						if len(generations) != 1 {
							t.Fatal("missing host tail generation")
						}
						warm := testing.AllocsPerRun(20, func() {
							if e := rv64ActivateNativeTailGenerations(generations); e != nil {
								panic(e)
							}
						})
						t.Logf("retained generation activation Go allocations=%g", warm)
						if warm != 0 {
							t.Fatalf("warm host tail activation allocations=%g", warm)
						}
					}
				})
			}
		}
	}
}

// Load mode is also used by the compiler-free build. Source and saved native
// images use the actual default configuration, with no private feature flags.
func rv64CollectorHostCompile(t *testing.T, name string, source []byte, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_TAIL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE")
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source case")
		}
		blob, e := os.ReadFile(filepath.Join(dir, name+".wago"))
		if e != nil {
			t.Fatal(e)
		}
		c, e := LoadTrustedArtifact(blob)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	c, e := NewRuntimeConfig().Compile(source)
	if e != nil {
		t.Fatal(e)
	}
	original := c
	t.Cleanup(func() { original.Close() })
	if artifact || mode == "prepare" {
		blob, e := c.MarshalBinary()
		if e != nil {
			t.Fatal(e)
		}
		if mode == "prepare" {
			if e := os.MkdirAll(dir, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, name+".wago"), blob, 0600); e != nil {
				t.Fatal(e)
			}
		}
		if artifact {
			c, e = LoadTrustedArtifact(blob)
			if e != nil {
				t.Fatal(e)
			}
			saved := c
			t.Cleanup(func() { saved.Close() })
		}
	}
	return c
}
