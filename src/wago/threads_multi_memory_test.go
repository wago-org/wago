//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsPrivateSecondaryModule(primary64, secondary64, imported bool) []byte {
	return threadsPrivateSecondaryModuleWithStart(primary64, secondary64, imported, false)
}

func threadsPrivateSecondaryModuleWithStart(primary64, secondary64, imported, startTrap bool) []byte {
	p, q := wasm.I32, wasm.I32
	primaryLimits, secondaryLimits := byte(3), byte(1)
	if primary64 {
		p, primaryLimits = wasm.I64, 7
	}
	if secondary64 {
		q, secondaryLimits = wasm.I64, 5
	}
	count := wasm.I32
	if primary64 && secondary64 {
		count = wasm.I64
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{p, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{q, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{p}),
		wasmtest.FuncType(nil, []wasm.ValType{q}),
		wasmtest.FuncType([]wasm.ValType{p}, []wasm.ValType{p}),
		wasmtest.FuncType([]wasm.ValType{q}, []wasm.ValType{q}),
		wasmtest.FuncType([]wasm.ValType{p, q, count}, nil),
		wasmtest.FuncType([]wasm.ValType{q, p, count}, nil),
		wasmtest.FuncType(nil, nil),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(entry, 2, primaryLimits, 1, 3))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{3}, []byte{4}, []byte{5}, []byte{6}, []byte{7}, []byte{8}, []byte{0})))
	memories := [][]byte{{secondaryLimits, 1, 3}}
	if !imported {
		memories = append([][]byte{{primaryLimits, 1, 3}}, memories...)
	}
	sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	exports := [][]byte{wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1), wasmtest.ExportEntry("store0-explicit", 0, 9)}
	for i, name := range []string{"store0", "store1", "size0", "size1", "grow0", "grow1", "copy01", "copy10"} {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if startTrap {
		sections = append(sections, wasmtest.Section(8, []byte{8}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 2, 0, 0x20, 0, 0xfe, 0x10, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x36, 0x42, 1, 0, 0x20, 0, 0x28, 0x42, 1, 0, 0x0b}),
		wasmtest.Code([]byte{0x3f, 0, 0x0b}), wasmtest.Code([]byte{0x3f, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x40, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 1, 0, 0x0b}),
		wasmtest.Code([]byte{0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 0x42, 0, 0, 0x20, 0, 0xfe, 0x10, 0x42, 0, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsPrivateSecondaryMemoryIsolationGrowthAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, primary64 := range []bool{false, true} {
			for _, secondary64 := range []bool{false, true} {
				for _, cache := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						t.Run(fmt.Sprintf("imported=%t/primary64=%t/secondary64=%t/cache=%t/regabi=%t", imported, primary64, secondary64, cache, regABI), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).
								WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(threadsPrivateSecondaryModule(primary64, secondary64, imported))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
								imports := NewImports()
								var provider *Instance
								if imported {
									providerModule, err := cfg.Compile(threadsPrivateSecondaryModule(primary64, secondary64, false))
									if err != nil {
										t.Fatal(err)
									}
									defer providerModule.Close()
									provider, err = Instantiate(providerModule)
									if err != nil {
										t.Fatal(err)
									}
									defer provider.Close()
									memory, err := provider.ExportedMemory("memory0")
									if err != nil {
										t.Fatal(err)
									}
									imports.Memory("env", "memory", memory)
								}
								in, err := Instantiate(compiled, imports)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								check := func(name string, want uint64, args ...uint64) {
									t.Helper()
									got, err := in.Invoke(name, args...)
									if err != nil || len(got) != 1 || got[0] != want {
										t.Fatalf("%s = %v, %v; want %d", name, got, err, want)
									}
								}
								check("store0", 7, 0, 7)
								check("store0-explicit", 5, 8, 5)
								check("store1", 11, 0, 11)
								m0, _ := in.ExportedMemory("memory0")
								m1, _ := in.ExportedMemory("memory1")
								if bytes.Equal(m0.UnsafeBytes()[:4], m1.UnsafeBytes()[:4]) {
									t.Fatal("primary and secondary backing alias")
								}
								check("grow0", 1, 1)
								check("size0", 2)
								check("size1", 1)
								if imported {
									got, err := provider.Invoke("size0")
									if err != nil || got[0] != 2 {
										t.Fatalf("provider growth view = %v, %v", got, err)
									}
									got, err = provider.Invoke("store1", 0, 31)
									if err != nil || got[0] != 31 {
										t.Fatalf("provider secondary write = %v, %v", got, err)
									}
									if binary.LittleEndian.Uint32(m1.UnsafeBytes()) != 11 {
										t.Fatal("private secondary memory aliases provider")
									}
								}
								check("grow1", 1, 1)
								check("size1", 2)
								check("store0", 19, 65536, 19)
								check("store1", 23, 65536, 23)
								for _, name := range []string{"copy01", "copy10"} {
									if _, err := in.Invoke(name, 32, 65536, 4); err != nil {
										t.Fatal(err)
									}
									want, destination := uint32(23), m0
									if name == "copy10" {
										want, destination = 19, m1
									}
									if got := binary.LittleEndian.Uint32(destination.UnsafeBytes()[32:]); got != want {
										t.Fatalf("%s copied %d, want %d", name, got, want)
									}
									before0, before1 := bytes.Clone(m0.UnsafeBytes()), bytes.Clone(m1.UnsafeBytes())
									if _, err := in.Invoke(name, 131071, 0, 4); err == nil {
										t.Fatal("cross-memory copy accepted bad destination")
									}
									if !bytes.Equal(before0, m0.UnsafeBytes()) || !bytes.Equal(before1, m1.UnsafeBytes()) {
										t.Fatal("trapping cross-memory copy mutated backing")
									}
								}
								for _, probe := range []struct {
									name string
									wide bool
								}{{"store0", primary64}, {"store1", secondary64}} {
									if probe.wide {
										if _, err := in.Invoke(probe.name, 1<<32|8, 29); err == nil {
											t.Fatalf("%s truncated memory64 address", probe.name)
										}
									} else {
										check(probe.name, 29, 1<<32|8, 29)
									}
								}
								if err := in.Close(); err != nil {
									t.Fatal(err)
								}
								if (!imported && m0.jobMemory() != nil) || m1.jobMemory() != nil {
									t.Fatal("closing owner retained either backing")
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestThreadsPrivateSecondaryMappingQuotaAndRollback(t *testing.T) {
	for _, startTrap := range []bool{false, true} {
		for _, limit := range []uint32{2, 3} {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithNativeMemoryMappingLimit(limit)
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			mod, err := rt.Compile(threadsPrivateSecondaryModuleWithStart(false, false, false, startTrap))
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			if limit == 2 || startTrap {
				if err == nil {
					in.Close()
					t.Fatal("mapping quota/start failure accepted")
				}
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 0 {
					t.Fatalf("failed constructor retained mappings: %+v", stats)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 3 {
					t.Fatalf("backing/control/secondary mappings: %+v", stats)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if stats := rt.ResourceStats(); stats.NativeMemoryMappings != 0 {
					t.Fatalf("closed instance retained mappings: %+v", stats)
				}
			}
		}
	}
}
