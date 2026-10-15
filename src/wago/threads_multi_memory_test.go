//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"

	railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsPrivateSecondaryModule(primary64, secondary64, imported bool) []byte {
	return threadsPrivateSecondaryModuleWithStart(primary64, secondary64, imported, false)
}

func threadsPrivateSecondaryModuleWithStart(primary64, secondary64, imported, startTrap bool) []byte {
	return threadsSecondaryModule(primary64, secondary64, imported, false, startTrap)
}

func threadsSecondaryModule(primary64, secondary64, imported, secondaryImported, startTrap bool) []byte {
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
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entries := [][]byte{append(entry, 2, primaryLimits, 1, 3)}
		if secondaryImported {
			secondary := append(wasmtest.Name("env"), wasmtest.Name("secondary")...)
			entries = append(entries, append(secondary, 2, secondaryLimits, 1, 3))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{3}, []byte{4}, []byte{5}, []byte{6}, []byte{7}, []byte{8}, []byte{0}, []byte{9})))
	memories := [][]byte{{secondaryLimits, 1, 3}}
	if secondaryImported {
		memories = nil
	}
	if !imported {
		memories = append([][]byte{{primaryLimits, 1, 3}}, memories...)
	}
	if len(memories) != 0 {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	exports := [][]byte{wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1), wasmtest.ExportEntry("store0-explicit", 0, 9), wasmtest.ExportEntry("bump1", 0, 10)}
	for i, name := range []string{"store0", "store1", "size0", "size1", "grow0", "grow1", "copy01", "copy10"} {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if startTrap {
		sections = append(sections, wasmtest.Section(8, []byte{8}))
	}
	addressOp := byte(0x41)
	if secondary64 {
		addressOp = 0x42
	}
	bump := []byte{addressOp, 0, addressOp, 0, 0x28, 0x42, 1, 0, 0x41, 1, 0x6a, 0x36, 0x42, 1, 0, addressOp, 0, 0x28, 0x42, 1, 0, 0x0b}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 2, 0, 0x20, 0, 0xfe, 0x10, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x36, 0x42, 1, 0, 0x20, 0, 0x28, 0x42, 1, 0, 0x0b}),
		wasmtest.Code([]byte{0x3f, 0, 0x0b}), wasmtest.Code([]byte{0x3f, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x40, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 1, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 1, 0, 0x0b}),
		wasmtest.Code([]byte{0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 0x42, 0, 0, 0x20, 0, 0xfe, 0x10, 0x42, 0, 0, 0x0b}),
		wasmtest.Code(bump),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsPrivateSecondaryMemoryIsolationGrowthAndCodec(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
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
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
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

func TestThreadsPrivateSecondaryStartFailureReleasesNativeControl(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	if !coreruntime.ProcessNativeMemoryStats().Supported {
		t.Skip("native interrupt registry is only tracked on Linux")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithNativeMemoryMappingLimit(3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	mod, err := rt.Compile(threadsPrivateSecondaryModuleWithStart(false, false, false, true))
	if err != nil {
		t.Fatal(err)
	}
	before := coreruntime.ProcessNativeMemoryStats()
	for i := 0; i < 8; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err == nil {
			in.Close()
			t.Fatal("trapping start succeeded")
		}
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
			t.Fatalf("start failure = %v, want unreachable trap", err)
		}
		if after := coreruntime.ProcessNativeMemoryStats(); after.Active != before.Active {
			t.Fatalf("failed start retained native mappings: before=%+v after=%+v", before, after)
		}
	}
}

func TestThreadsImportedSecondaryMemoryUsesSharedLease(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, regABI := range []bool{false, true} {
		t.Run(fmt.Sprintf("regabi=%t", regABI), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
			producerModule, err := cfg.Compile(threadsPrivateSecondaryModule(false, false, false))
			if err != nil {
				t.Fatal(err)
			}
			defer producerModule.Close()
			producer, err := Instantiate(producerModule)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			primary, err := producer.ExportedMemory("memory0")
			if err != nil {
				t.Fatal(err)
			}
			secondary, err := producer.ExportedMemory("memory1")
			if err != nil {
				t.Fatal(err)
			}
			c, err := cfg.Compile(threadsSecondaryModule(false, false, true, true, false))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				binary.LittleEndian.PutUint32(secondary.UnsafeBytes(), 0)
				instances := make([]*Instance, 8)
				for i := range instances {
					in, err := Instantiate(compiled, NewImports().Memory("env", "memory", primary).Memory("env", "secondary", secondary))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					instances[i] = in
					if in.executionFlags.Load()&executionFlagForeignNativeCalls == 0 {
						t.Fatal("unshared imported secondary has no shared execution lease")
					}
				}
				var wg sync.WaitGroup
				for _, in := range instances {
					wg.Add(1)
					go func(in *Instance) {
						defer wg.Done()
						for i := 0; i < 1000; i++ {
							if _, err := in.Invoke("bump1"); err != nil {
								t.Error(err)
								return
							}
						}
					}(in)
				}
				wg.Wait()
				if got := binary.LittleEndian.Uint32(secondary.UnsafeBytes()); got != 8000 {
					t.Fatalf("shared unshared-import counter = %d, want 8000", got)
				}
				for _, in := range instances {
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestThreadsImportedSecondaryAddressWidthsGrowthAndCodec(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, primary64 := range []bool{false, true} {
		for _, secondary64 := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("primary64=%t/secondary64=%t/regabi=%t", primary64, secondary64, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
					providerModule, err := cfg.Compile(threadsPrivateSecondaryModule(primary64, secondary64, false))
					if err != nil {
						t.Fatal(err)
					}
					defer providerModule.Close()
					c, err := cfg.Compile(threadsSecondaryModule(primary64, secondary64, true, true, false))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						provider, err := Instantiate(providerModule)
						if err != nil {
							t.Fatal(err)
						}
						defer provider.Close()
						primary, err := provider.ExportedMemory("memory0")
						if err != nil {
							t.Fatal(err)
						}
						secondary, err := provider.ExportedMemory("memory1")
						if err != nil {
							t.Fatal(err)
						}
						in, err := Instantiate(compiled, NewImports().Memory("env", "memory", primary).Memory("env", "secondary", secondary))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if got, err := in.Invoke("grow1", 1); err != nil || got[0] != 1 {
							t.Fatalf("imported secondary grow = %v, %v", got, err)
						}
						if got, err := provider.Invoke("size1"); err != nil || got[0] != 2 {
							t.Fatalf("producer secondary size = %v, %v", got, err)
						}
						if got, err := in.Invoke("store1", 65536, 37); err != nil || got[0] != 37 {
							t.Fatalf("secondary write = %v, %v", got, err)
						}
						if got := binary.LittleEndian.Uint32(secondary.UnsafeBytes()[65536:]); got != 37 {
							t.Fatalf("provider backing = %d", got)
						}
						if _, err := in.Invoke("copy01", 32, 65536, 4); err != nil {
							t.Fatal(err)
						}
						if got := binary.LittleEndian.Uint32(primary.UnsafeBytes()[32:]); got != 37 {
							t.Fatalf("cross-memory import copy = %d", got)
						}
						if got, err := provider.Invoke("size0"); err != nil || got[0] != 1 {
							t.Fatalf("unrelated primary size = %v, %v", got, err)
						}
					}
				})
			}
		}
	}
}

// Secondary atomics deliberately mix the primary and selected address widths.
// Every defined numeric atomic opcode is exercised through explicit memarg 1.
func threadsSecondaryAtomicModuleWithSharing(primary64, secondary64, imported, constant, secondaryShared bool) []byte {
	address := wasm.I32
	limits0, limits1 := byte(3), byte(1)
	addressOp := byte(0x41)
	if primary64 {
		limits0 = 7
	}
	if secondary64 {
		address, limits1, addressOp = wasm.I64, 5, 0x42
	}
	if secondaryShared {
		limits1 |= 2
	}
	var types, functions, exports, bodies [][]byte
	exports = append(exports, wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))
	for sub := byte(0x10); sub <= 0x4e; sub++ {
		d, err := railshared.DecodeAtomic(wasm.NewReader([]byte{sub, 0, 8}))
		if err != nil {
			panic(err)
		}
		value := wasm.I32
		if d.ResultSize == 8 {
			value = wasm.I64
		}
		params := []wasm.ValType{address}
		operands := 0
		if d.Class != railshared.AtomicLoad {
			operands = 1
		}
		if d.Class == railshared.AtomicCmpxchg {
			operands = 2
		}
		for j := 0; j < operands; j++ {
			params = append(params, value)
		}
		result := []wasm.ValType{value}
		if d.Class == railshared.AtomicStore {
			result = nil
		}
		types = append(types, wasmtest.FuncType(params, result))
		functions = append(functions, wasmtest.ULEB(uint32(len(functions))))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("op%x", sub), 0, uint32(len(bodies))))
		body := []byte{0x20, 0}
		if constant {
			body = []byte{addressOp, 0}
		}
		for j := 1; j <= operands; j++ {
			body = append(body, 0x20, byte(j))
		}
		align := byte(0)
		for 1<<align < d.Size {
			align++
		}
		body = append(body, 0xfe, sub, 64|align, 1, 8, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}
	for _, sub := range []byte{0, 1, 2} {
		params := []wasm.ValType{address, wasm.I32}
		if sub == 2 {
			params[1] = wasm.I64
		}
		if sub != 0 {
			params = append(params, wasm.I64)
		}
		types = append(types, wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))
		functions = append(functions, wasmtest.ULEB(uint32(len(functions))))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("helper%d", sub), 0, uint32(len(bodies))))
		body := []byte{0x20, 0, 0x20, 1}
		align := byte(2)
		if sub == 2 {
			align = 3
		}
		if sub != 0 {
			body = append(body, 0x20, 2)
		}
		body = append(body, 0xfe, sub, 64|align, 1, 8, 0x0b)
		bodies = append(bodies, wasmtest.Code(body))
	}

	if secondary64 {
		for j, offset := range [][]byte{{0x80, 0x80, 0x80, 0x80, 0x10}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}} {
			types = append(types, wasmtest.FuncType([]wasm.ValType{address}, []wasm.ValType{wasm.I32}))
			functions = append(functions, wasmtest.ULEB(uint32(len(functions))))
			exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("high%d", j), 0, uint32(len(bodies))))
			body := append([]byte{0x20, 0, 0xfe, 0x10, 0x42, 1}, offset...)
			bodies = append(bodies, wasmtest.Code(append(body, 0x0b)))
		}
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if imported {
		m0 := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		m1 := append(wasmtest.Name("env"), wasmtest.Name("secondary")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(m0, 2, limits0, 1, 3), append(m1, 2, limits1, 1, 3))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(functions...)))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{limits0, 1, 3}, []byte{limits1, 1, 3})))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))...)
}

func TestThreadsSecondaryAtomicMatrix(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	testThreadsSecondaryAtomicMatrix(t, false)
}

func TestThreadsSharedSecondaryAtomicMatrix(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	testThreadsSecondaryAtomicMatrix(t, true)
}

func testThreadsSecondaryAtomicMatrix(t *testing.T, secondaryShared bool) {
	for _, primary64 := range []bool{false, true} {
		for _, secondary64 := range []bool{false, true} {
			for _, imported := range []bool{false, true} {
				for _, constant := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						t.Run(fmt.Sprintf("p64=%t/q64=%t/imported=%t/constant=%t/regabi=%t", primary64, secondary64, imported, constant, regABI), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(threadsSecondaryAtomicModuleWithSharing(primary64, secondary64, imported, constant, secondaryShared))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
								imports := NewImports()
								if imported {
									pc, err := cfg.Compile(threadsSecondaryAtomicModuleWithSharing(primary64, secondary64, false, constant, secondaryShared))
									if err != nil {
										t.Fatal(err)
									}
									defer pc.Close()
									provider, err := Instantiate(pc)
									if err != nil {
										t.Fatal(err)
									}
									defer provider.Close()
									m0, _ := provider.ExportedMemory("memory0")
									m1, _ := provider.ExportedMemory("memory1")
									imports.Memory("env", "memory", m0).Memory("env", "secondary", m1)
								}
								in, err := Instantiate(compiled, imports)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								m0, _ := in.ExportedMemory("memory0")
								m1, _ := in.ExportedMemory("memory1")
								binary.LittleEndian.PutUint64(m0.UnsafeBytes()[8:], 0x123456789abcdef0)
								for sub := byte(0x10); sub <= 0x4e; sub++ {
									d, _ := railshared.DecodeAtomic(wasm.NewReader([]byte{sub, 0, 8}))
									binary.LittleEndian.PutUint64(m1.UnsafeBytes()[8:], 7)
									args := []uint64{0}
									if d.Class != railshared.AtomicLoad {
										args = append(args, 9)
									}
									if d.Class == railshared.AtomicCmpxchg {
										args = []uint64{0, 7, 9}
									}
									got, err := in.Invoke(fmt.Sprintf("op%x", sub), args...)
									if err != nil {
										t.Fatalf("opcode %x: %v", sub, err)
									}
									if d.Class != railshared.AtomicStore && (len(got) != 1 || got[0] != 7) {
										t.Fatalf("opcode %x old=%v", sub, got)
									}
									want := uint64(7)
									if d.Class == railshared.AtomicStore || d.Class == railshared.AtomicCmpxchg {
										want = 9
									}
									if d.Class == railshared.AtomicRMW {
										switch d.Operation {
										case railshared.AtomicAdd:
											want = 16
										case railshared.AtomicSub:
											want = ^uint64(1)
										case railshared.AtomicAnd:
											want = 1
										case railshared.AtomicOr:
											want = 15
										case railshared.AtomicXor:
											want = 14
										case railshared.AtomicXchg:
											want = 9
										}
									}
									mask := ^uint64(0)
									if d.Size < 8 {
										mask = (uint64(1) << (d.Size * 8)) - 1
									}
									actual := binary.LittleEndian.Uint64(m1.UnsafeBytes()[8:])
									if actual != (7&^mask)|(want&mask) {
										t.Fatalf("opcode %x memory=%x want=%x", sub, actual, want&mask)
									}
									if !constant {
										badAddresses := []uint64{65536, 1 << 32}
										if d.Size > 1 {
											badAddresses = append(badAddresses, 1)
										}
										for _, addr := range badAddresses {
											if addr == 1<<32 && !secondary64 {
												continue
											}
											args[0] = addr
											before := bytes.Clone(m1.UnsafeBytes())
											_, err := in.Invoke(fmt.Sprintf("op%x", sub), args...)
											var trap *TrapError
											wantTrap := TrapLinMemOutOfBounds
											if addr == 1 {
												wantTrap = TrapAtomicUnaligned
											}
											if !errors.As(err, &trap) || trap.Code != wantTrap {
												t.Fatalf("opcode %x addr=%x: %v", sub, addr, err)
											}
											if !bytes.Equal(before, m1.UnsafeBytes()) {
												t.Fatal("trapping atomic changed backing")
											}
										}
									}
								}

								if secondary64 {
									for _, name := range []string{"high0", "high1"} {
										for _, addr := range []uint64{0, ^uint64(0) - 3} {
											_, err := in.Invoke(name, addr)
											var trap *TrapError
											if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
												t.Fatalf("%s address=%x: %v", name, addr, err)
											}
										}
									}
									_, err := in.Invoke("helper0", 1<<32, 1)
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
										t.Fatalf("high notify: %v", err)
									}
								}
								if got, err := in.Invoke("helper0", 0, 1); err != nil || len(got) != 1 || got[0] != 0 {
									t.Fatalf("unshared notify=%v,%v", got, err)
								}
								for _, name := range []string{"helper1", "helper2"} {
									got, err := in.Invoke(name, 0, 0, 0)
									if secondaryShared {
										if err != nil || len(got) != 1 || got[0] != 1 {
											t.Fatalf("shared %s=%v,%v", name, got, err)
										}
										binary.LittleEndian.PutUint64(m1.UnsafeBytes()[8:], 9)
										got, err = in.Invoke(name, 0, 9, 0)
										if err != nil || len(got) != 1 || got[0] != 2 {
											t.Fatalf("shared equal %s=%v,%v", name, got, err)
										}
									} else {
										var trap *TrapError
										if !errors.As(err, &trap) || trap.Code != TrapExpectedSharedMemory {
											t.Fatalf("unshared %s=%v", name, err)
										}
									}
								}
								if got := binary.LittleEndian.Uint64(m0.UnsafeBytes()[8:]); got != 0x123456789abcdef0 {
									t.Fatalf("primary changed to %x", got)
								}
							}
						})
					}
				}
			}
		}
	}
}
