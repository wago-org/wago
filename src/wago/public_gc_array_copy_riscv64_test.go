//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCArrayCopyHex = "0061736d01000000019580808000055e78005e780160017f017f60000060037f7f7f000387808080000602030304030306958080800002640000410a410cfb06000b640101410cfb07010b078581808000060d61727261795f6765745f6e746800001461727261795f636f70792d6e756c6c2d6c65667400011561727261795f636f70792d6e756c6c2d726967687400020a61727261795f636f707900031961727261795f636f70795f6f7665726c61705f746573742d3100041961727261795f636f70795f6f7665726c61705f746573742d3200050c8180808000010a9a818080000689808080000023012000fb0d010b908080800000d0014100230041004100fb1101000b90808080000023014100d00041004100fb1101000b90808080000023012000230020012002fb1101000ba180808000010164014100410cfb09010021002000410120004100410bfb110101200024010ba180808000010164014100410cfb09010021002000410020004101410bfb110101200024010b0b8f8080800001010c6162636465666768696a6b6c"

func rv64PublicGCArrayCopyBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayCopyHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayCopyCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-copy.wago")
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loaded.Close() })
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCArrayCopyBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func rv64AssertGCArrayCopyTrap(t *testing.T, in *Instance, name string, code coreruntime.TrapCode, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if got != nil || err == nil {
		t.Fatalf("trapping %s%v=%v,%v", name, args, got, err)
	}
	var trap *coreruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("trapping %s%v returned %v, want trap %v", name, args, err, code)
	}
}

func rv64AssertGCArrayCopyValues(t *testing.T, in *Instance, want map[uint64]uint64) {
	t.Helper()
	for index, value := range want {
		got, err := in.Invoke("array_get_nth", index)
		if err != nil || !slices.Equal(got, []uint64{value}) {
			t.Fatalf("array_get_nth(%d)=%v,%v want %d", index, got, err, value)
		}
	}
}

func TestRV64PublicGCArrayCopyExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayCopyBytes(t)
	wantSource := [sha256.Size]byte{0x3c, 0xe0, 0xc2, 0x21, 0x05, 0x57, 0x16, 0x18, 0x83, 0x2b, 0x6d, 0x97, 0x16, 0x4a, 0x26, 0xe4, 0xb7, 0xde, 0xe0, 0x35, 0xf5, 0x40, 0x95, 0x74, 0x22, 0xb8, 0x87, 0xc4, 0xc0, 0x4d, 0x4f, 0x35}
	if len(data) != 402 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.copy source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayCopyCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayCopy(view) || !rv64GCArrayCopyExecutionCode(view) || err != nil {
				t.Fatalf("missing exact array.copy witness: validation=%v product=%v code=%d/%x entries=%v/%v globals=%#v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.memoryDir.gcArrayGlobals)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 1); err != nil || params != 7 || results != 1 {
				t.Fatalf("array.copy call arena=%d/%d,%v, want 7/1", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 224, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.config))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if len(in.serArgs) < 7*8 {
						t.Fatalf("array.copy argument arena=%d bytes, want at least 56", len(in.serArgs))
					}
					state := in.pluginState.Load()
					stagedRoots := 0
					if state != nil {
						stagedRoots = int(state.gcGlobalRootCount)
					}
					genericRoots := 0
					if public := in.existingPublicGCState(); public != nil {
						genericRoots = len(public.globalRoots)
					}
					if stagedRoots+genericRoots != 2 {
						t.Fatalf("array.copy roots staged/generic=%d/%d, want two mappings", stagedRoots, genericRoots)
					}
					var mutableRoot gcGlobalRootMapping
					var foundMutableRoot bool
					if state != nil {
						for _, mapping := range state.gcGlobalRoots[:state.gcGlobalRootCount] {
							if mapping.GlobalIndex == 1 {
								mutableRoot, foundMutableRoot = mapping, true
							}
						}
					}
					if public := in.existingPublicGCState(); public != nil {
						for _, mapping := range public.globalRoots {
							if mapping.GlobalIndex == 1 {
								mutableRoot, foundMutableRoot = mapping, true
							}
						}
					}
					if !foundMutableRoot {
						t.Fatal("array.copy mutable global root mapping is absent")
					}
					rv64AssertGCArrayCopyTrap(t, in, "array_copy-null-left", coreruntime.TrapNullReference)
					rv64AssertGCArrayCopyTrap(t, in, "array_copy-null-right", coreruntime.TrapNullReference)
					for _, args := range [][]uint64{{13, 0, 0}, {0, 13, 0}, {0, 0, 13}} {
						rv64AssertGCArrayCopyTrap(t, in, "array_copy", coreruntime.TrapBuiltin, args...)
					}
					if got, err := in.Invoke("array_copy", 0xffff00000000000c, 0xaaaa000000000000, 0xbbbb000000000000); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper destination-end zero-length copy=%v,%v", got, err)
					}
					if got, err := in.Invoke("array_copy", 0xaaaa000000000000, 0xffff00000000000c, 0xbbbb000000000000); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper source-end zero-length copy=%v,%v", got, err)
					}
					rv64AssertGCArrayCopyValues(t, in, map[uint64]uint64{0: 0, 5: 0, 11: 0})
					rv64AssertGCArrayCopyTrap(t, in, "array_get_nth", coreruntime.TrapBuiltin, 12)
					if got, err := in.Invoke("array_copy", 0xaaaa000000000000, 0xbbbb000000000000, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.copy=%v,%v", got, err)
					}
					rv64AssertGCArrayCopyValues(t, in, map[uint64]uint64{0: 10, 1: 10, 2: 0})
					if got, err := in.Invoke("array_copy_overlap_test-1"); err != nil || len(got) != 0 {
						t.Fatalf("overlap copy 1=%v,%v", got, err)
					}
					rv64AssertGCArrayCopyValues(t, in, map[uint64]uint64{0: 97, 1: 97, 2: 98, 5: 101, 10: 106, 11: 107})
					if got, err := in.Invoke("array_copy_overlap_test-2"); err != nil || len(got) != 0 {
						t.Fatalf("overlap copy 2=%v,%v", got, err)
					}
					wantAfter := map[uint64]uint64{0: 98, 1: 99, 5: 103, 9: 107, 10: 108, 11: 108}
					rv64AssertGCArrayCopyValues(t, in, wantAfter)
					beforeTrap := readGlobalObject(in.globalCells[1], ValAnyRef)
					rv64AssertGCArrayCopyTrap(t, in, "array_copy", coreruntime.TrapBuiltin, 0, 0, 13)
					if afterTrap := readGlobalObject(in.globalCells[1], ValAnyRef); afterTrap != beforeTrap {
						t.Fatalf("trapping array.copy changed mutable global %#x -> %#x", beforeTrap, afterTrap)
					}
					rooted, err := in.gc.CheckedGlobalSlot(mutableRoot.SlotIndex)
					if err != nil || uint64(rooted) != beforeTrap {
						t.Fatalf("mutable root=%v,%v want %#x", rooted, err, beforeTrap)
					}
					rv64AssertGCArrayCopyValues(t, in, wantAfter)
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d, want immutable plus current mutable array", live)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayCopyAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayCopyCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":        func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":       func(v *Compiled) { v.Entry[5]++; v.InternalEntry[5]++ },
		"function":    func(v *Compiled) { v.Funcs[3].Params[2] = ValI64 },
		"type":        func(v *Compiled) { v.Types[1].Array.Mutable = false },
		"type-key":    func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
		"descriptor":  func(v *Compiled) { v.GCTypeDescs[1].ElemSize++ },
		"global":      func(v *Compiled) { v.Globals[1].Mutable = false },
		"initializer": func(v *Compiled) { v.memoryDir.gcArrayGlobals[0].Bits[0]++ },
		"passive-data": func(v *Compiled) {
			v.PassiveData[0].Bytes = slices.Clone(v.PassiveData[0].Bytes)
			v.PassiveData[0].Bytes[0] ^= 1
		},
		"export":  func(v *Compiled) { delete(v.Exports, "array_copy"); v.Exports["copy"] = 3 },
		"feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			if candidate.memoryDir != nil {
				dir := *candidate.memoryDir
				dir.gcArrayGlobals = slices.Clone(dir.gcArrayGlobals)
				for i := range dir.gcArrayGlobals {
					dir.gcArrayGlobals[i].Bits = slices.Clone(dir.gcArrayGlobals[i].Bits)
				}
				candidate.memoryDir = &dir
			}
			candidate.PassiveData = slices.Clone(candidate.PassiveData)
			mutate(candidate)
			if rv64GCArrayCopy(candidate) {
				t.Fatal("hostile mutation retained array.copy witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayCopyCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayCopyBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free array.copy path used source compilation", err)
	}
}
