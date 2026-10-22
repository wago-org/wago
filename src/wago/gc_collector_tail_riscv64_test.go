//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64CollectorTailSource(kind string, vector bool, graph ...string) []byte {
	signature := []byte{0x60, 1, 0x63, 0, 1, 0x63, 0}
	get := []byte{0x20, 0}
	if vector {
		signature = []byte{0x60, 2, 0x63, 0, 0x7b, 2, 0x63, 0, 0x7b}
		get = append(get, 0x20, 1)
	}
	target := []byte{0x41, 1, 0xfb, 0, 0, 0x1a}
	target = append(target, get...)
	target = append(target, 0x0b)
	tail := append([]byte(nil), get...)
	switch kind {
	case "reference":
		tail = append(tail, 0xd2, 0, 0x15, 1)
	case "indirect", "indirect-mismatch":
		tail = append(tail, 0x41, 0, 0x13, 1, 0)
	default:
		tail = append(tail, 0x12, 0)
	}
	tail = append(tail, 0x0b)
	run := []byte{0x41, 42, 0xfb, 0, 0}
	if vector {
		run = append(run, 0xfd, 12)
		run = append(run, make([]byte, 16)...)
	}
	run = append(run, 0x10, 1)
	if vector {
		run = append(run, 0x1a)
	}
	run = append(run, 0xfb, 2, 0, 0, 0x0b)
	types := [][]byte{{0x5f, 1, 0x7f, 0}, signature, {0x60, 0, 1, 0x7f}}
	if len(graph) != 0 && graph[0] == "subtype" {
		types[1] = append([]byte{0x50, 0}, signature...)
		types = append(types, append([]byte{0x4f, 1, 1}, signature...))
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	functions, bodies := [][]byte{{1}, {1}, {2}}, [][]byte{wasmtest.Code(target), wasmtest.Code(tail), wasmtest.Code(run)}
	if len(graph) != 0 && graph[0] == "subtype" {
		functions[0] = []byte{3}
	}
	if kind == "import" {
		im := append(wasmtest.Name("env"), wasmtest.Name("echo")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(im, 0, 1))))
		functions, bodies = functions[1:], bodies[1:]
	}
	if kind == "indirect-mismatch" {
		types := [][]byte{{0x5f, 1, 0x7f, 0}, signature, {0x60, 0, 1, 0x7f}, {0x5f, 1, 0x7e, 0}}
		other := []byte{0x60, 1, 0x63, 3, 1, 0x63, 3}
		if vector {
			other = []byte{0x60, 2, 0x63, 3, 0x7b, 2, 0x63, 3, 0x7b}
		}
		types = append(types, other)
		sections[0] = wasmtest.Section(1, wasmtest.Vec(types...))
		functions = append(functions, []byte{4})
		bodies = append(bodies, wasmtest.Code(append(append([]byte{}, get...), 0x0b)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(functions...)))
	if kind == "indirect" || kind == "indirect-mismatch" {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 2)}
	if kind == "local" {
		exports = append(exports, wasmtest.ExportEntry("echo", 0, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if kind == "reference" {
		sections = append(sections, wasmtest.Section(9, []byte{1, 3, 0, 1, 0}))
	}
	if kind == "indirect" || kind == "indirect-mismatch" {
		target := byte(0)
		if kind == "indirect-mismatch" {
			target = 3
		}
		sections = append(sections, wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, target}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return wasmtest.Module(sections...)
}

func TestRV64CollectorBearingTailCalls(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"local", "reference", "indirect", "import", "indirect-mismatch"} {
			for _, artifact := range []bool{false, true} {
				for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
					t.Run(fmt.Sprintf("%s/vector%t/artifact%t/profile%d", kind, vector, artifact, profile), func(t *testing.T) {
						cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
						cfg.railshotState.tailProducts = true
						source := rv64CollectorTailSource(kind, vector)
						m, err := wasm.DecodeModule(source)
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true}, 1, wasm.ValidationLimits{}); err != nil {
							t.Fatal(err)
						}
						c, err := cfg.Compile(source)
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
						rt := NewRuntime(WithRuntimeConfig(cfg))
						defer rt.Close()
						options := InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 4096, ForceMajorEveryMinor: true, VerifyAfterCollect: true}, store: rt.refStore, railshotTailPolicy: railshotInstantiateTailPolicy{referenceFunctions: true}}
						var provider *Instance
						if kind == "import" {
							pc, err := cfg.Compile(rv64CollectorTailSource("local", vector))
							if err != nil {
								t.Fatal(err)
							}
							defer pc.Close()
							if artifact {
								blob, e := pc.MarshalBinary()
								if e != nil {
									t.Fatal(e)
								}
								pc, e = LoadTrustedArtifact(blob)
								if e != nil {
									t.Fatal(e)
								}
								defer pc.Close()
							}
							provider, err = instantiateCore(pc, options)
							if err != nil {
								t.Fatal(err)
							}
							defer provider.Close()
							target, err := provider.ExportedFunc("echo")
							if err != nil {
								t.Fatal(err)
							}
							options.Imports = NewImports().Function("env", "echo", target)
						}
						in, err := instantiateCore(c, options)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if provider != nil {
							if in.gc == nil || in.gc != provider.gc {
								t.Fatal("native tail did not select the producer collector")
							}
							if err := provider.Close(); err != nil {
								t.Fatal(err)
							}
						}
						for round := 0; round < 4; round++ {
							got, err := in.InvokeContext(context.Background(), "run")
							if kind == "indirect-mismatch" {
								if err == nil {
									t.Fatalf("unrelated heap signature returned %v", got)
								}
								continue
							}
							if err != nil || len(got) != 1 || got[0] != 42 {
								t.Fatalf("run=%v %v", got, err)
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64CollectorTailWarmAllocations(t *testing.T) {
	for _, kind := range []string{"local", "reference", "indirect", "import"} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/vector%t", kind, vector), func(t *testing.T) {
				cfg := rv64RootedScalarConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
				cfg.railshotState.tailProducts = true
				c, err := cfg.Compile(rv64CollectorTailSource(kind, vector))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				opts := InstantiateOptions{store: rt.refStore, GC: GCConfig{DisableCollection: true}, railshotTailPolicy: railshotInstantiateTailPolicy{referenceFunctions: true}}
				if kind == "import" {
					pc, e := cfg.Compile(rv64CollectorTailSource("local", vector))
					if e != nil {
						t.Fatal(e)
					}
					defer pc.Close()
					producer, e := instantiateCore(pc, opts)
					if e != nil {
						t.Fatal(e)
					}
					defer producer.Close()
					target, e := producer.ExportedFunc("echo")
					if e != nil {
						t.Fatal(e)
					}
					opts.Imports = NewImports().Function("env", "echo", target)
				}
				in, err := instantiateCore(c, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				invoke := func() {
					got, e := session.Invoke()
					if e != nil || len(got) != 1 || got[0] != 42 {
						panic(fmt.Sprintf("run=%v,%v", got, e))
					}
				}
				invoke()
				allocations := testing.AllocsPerRun(4, invoke)
				t.Logf("public entry Go allocations=%g native code=%dB", allocations, len(c.code))
				if kind != "reference" && kind != "indirect" {
					if allocations != 0 {
						t.Fatalf("warm public entry allocations=%g; want 0", allocations)
					}
					return
				}
				// Dynamic public entry owns and releases a new bounded generation.
				// Repeated activation under its retained physical lease is the warm
				// transport boundary; do not count construction as per-hop work.
				session.Close()
				gate := in.lockInvocation(0)
				defer gate.unlockInvocation()
				entry, err := in.beginNativeEntry()
				if err != nil {
					t.Fatal(err)
				}
				defer in.unlockNativeEntry(entry)
				generations := entry.tables.targets.tailActive
				if len(generations) != 1 {
					t.Fatal("missing retained collector tail generation")
				}
				warm := testing.AllocsPerRun(100, func() {
					if err := rv64ActivateNativeTailGenerations(generations); err != nil {
						panic(err)
					}
				})
				t.Logf("retained generation activation Go allocations=%g", warm)
				if warm != 0 {
					t.Fatalf("warm generation activation allocations=%g; want 0", warm)
				}
			})
		}
	}
}

func TestRV64CollectorTailDeclaredSubtype(t *testing.T) {
	for _, kind := range []string{"reference", "indirect"} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/vector%t", kind, vector), func(t *testing.T) {
				cfg := rv64RootedScalarConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
				cfg.railshotState.tailProducts = true
				c, e := cfg.Compile(rv64CollectorTailSource(kind, vector, "subtype"))
				if e != nil {
					t.Fatal(e)
				}
				defer c.Close()
				blob, e := c.MarshalBinary()
				if e != nil {
					t.Fatal(e)
				}
				loaded, e := LoadTrustedArtifact(blob)
				if e != nil {
					t.Fatal(e)
				}
				defer loaded.Close()
				for _, code := range []*Compiled{c, loaded} {
					in, e := instantiateCore(code, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}, railshotTailPolicy: railshotInstantiateTailPolicy{referenceFunctions: true}})
					if e != nil {
						t.Fatal(e)
					}
					defer in.Close()
					got, e := in.Invoke("run")
					if e != nil || len(got) != 1 || got[0] != 42 {
						t.Fatalf("run=%v,%v", got, e)
					}
				}
			})
		}
	}
}

func TestRV64CollectorTailRequiresCurrentGCABI(t *testing.T) {
	compiled, err := NewRuntimeConfig().Compile(rv64CollectorTailSource("import", false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if len(view.importFuncSigs) != 1 || !view.importFuncSigs[0].unsafeCrossTail || !railshotNativeTailImport(view, 0) {
		t.Fatal("fixture must exercise a collector-bearing tail import")
	}
	if !rv64CollectorTailMetadata(view) {
		t.Fatal("current collector ABI did not admit tail metadata")
	}
	if _, valid := rv64RootedImportDigest(view); !valid {
		t.Fatal("current collector ABI did not admit the rooted tail import")
	}
	seen := make(map[uint32]bool)
	for _, version := range []uint32{0, 1, gc.NativeABIVersion - 1, gc.NativeABIVersion + 1} {
		if version == gc.NativeABIVersion || seen[version] {
			continue
		}
		seen[version] = true
		t.Run(fmt.Sprintf("ABI%d", version), func(t *testing.T) {
			candidateCompiled, err := NewRuntimeConfig().Compile(rv64CollectorTailSource("import", false))
			if err != nil {
				t.Fatal(err)
			}
			defer candidateCompiled.Close()
			candidate := candidateCompiled.executionView()
			candidate.codeCache.setNativeGCABIVersion(version)
			if rv64CollectorTailMetadata(candidate) {
				t.Fatal("unsupported collector ABI admitted tail metadata")
			}
			if _, valid := rv64RootedImportDigest(candidate); valid {
				t.Fatal("unsupported collector ABI admitted rooted tail import")
			}
			if rv64FreshRootedGC(candidate) {
				t.Fatal("unsupported collector ABI retained a rooted execution receipt")
			}
		})
	}
}
