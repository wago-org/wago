//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCArrayInitDataHex = "0061736d01000000019880808000065e78005e78015e770160017f017f60000060037f7f7f0003878080800006030304050504069e8080800003640000410a410cfb06000b640101410cfb07010b6402004106fb07020b07f080808000060d61727261795f6765745f6e746800001161727261795f6765745f6e74685f69313600011461727261795f696e69745f646174612d6e756c6c00020f61727261795f696e69745f6461746100031361727261795f696e69745f646174615f69313600040964726f705f7365677300050c8180808000010ae0808080000689808080000023012000fb0d010b89808080000023022000fb0d020b8e8080800000d001410041004100fb1201000b8e80808000002301200020012002fb1201000b8e80808000002302200020012002fb1202000b858080800000fc09000b0b8f8080800001010c6162636465666768696a6b6c"

func rv64PublicGCArrayInitDataBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCArrayInitDataHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCArrayInitDataCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-init-data.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCArrayInitDataBytes(t))
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

func TestRV64PublicGCArrayInitDataExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCArrayInitDataBytes(t)
	wantSource := [sha256.Size]byte{0xc1, 0x7d, 0xa5, 0x6e, 0xd5, 0xc6, 0x50, 0x83, 0xee, 0x20, 0x02, 0x37, 0x38, 0xcc, 0x5d, 0x9a, 0x36, 0xd1, 0xe3, 0x01, 0xd2, 0xf0, 0x6f, 0x23, 0xe2, 0x64, 0x5d, 0x6e, 0xc8, 0xa9, 0xca, 0x77}
	if len(data) != 335 || sha256.Sum256(data) != wantSource {
		t.Fatal("array.init_data source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCArrayInitDataCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayInitData(view) || !rv64GCArrayInitDataExecutionCode(view) || err != nil {
				t.Fatalf("missing exact array.init_data witness: validation=%v product=%v code=%d/%x entries=%v/%v globals=%#v", err, view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.memoryDir.gcArrayGlobals)
			}
			if params, results, err := railshotImportedCallSlots(view, 3, 0); err != nil || params != 6 || results != 0 {
				t.Fatalf("array.init_data call arena=%d/%d,%v, want 6/0", params, results, err)
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
					if len(in.serArgs) < 6*8 {
						t.Fatalf("array.init_data argument arena=%d bytes, want at least 48", len(in.serArgs))
					}
					if len(in.passiveDataDesc) != 16 || binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 12 {
						t.Fatalf("passive descriptor=%x", in.passiveDataDesc)
					}
					state := in.pluginState.Load()
					var roots []gcGlobalRootMapping
					if state != nil {
						roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
					}
					roots = append(roots, in.publicGCState().globalRoots...)
					if len(roots) != 3 {
						t.Fatalf("array.init_data global roots=%d, want 3", len(roots))
					}
					var globalRefs [3]gc.Ref
					for _, mapping := range roots {
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
						rooted, slotErr := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if slotErr != nil || rooted != ref || !rooted.IsObj() {
							t.Fatalf("global root %d=%v,%v want %v", mapping.GlobalIndex, rooted, slotErr, ref)
						}
						globalRefs[mapping.GlobalIndex] = ref
					}
					for _, call := range []struct {
						name string
						args []uint64
					}{{"array_init_data-null", nil}, {"array_init_data", []uint64{13, 0, 0}}, {"array_init_data", []uint64{0, 13, 0}}, {"array_init_data", []uint64{0, 0, 13}}, {"array_init_data_i16", []uint64{0, 0, 7}}} {
						if _, err = in.Invoke(call.name, call.args...); err == nil {
							t.Fatalf("trapping %s%v returned normally", call.name, call.args)
						}
					}
					for _, call := range []struct {
						name string
						args []uint64
					}{{"array_init_data", []uint64{0xffff00000000000c, 0, 0}}, {"array_init_data", []uint64{0, 0xffff00000000000c, 0}}, {"array_init_data_i16", []uint64{0, 0xffff000000000006, 0}}} {
						if got, err := in.Invoke(call.name, call.args...); err != nil || len(got) != 0 {
							t.Fatalf("zero-length %s%v=%v,%v", call.name, call.args, got, err)
						}
					}
					if _, err = in.Invoke("array_init_data", 0, 11, 2); err == nil {
						t.Fatal("short source returned normally")
					}
					if got, err := in.Invoke("array_get_nth", 0); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("trapping init changed destination: %v,%v", got, err)
					}
					if got, err := in.Invoke("array_init_data", 0xaaaa000000000004, 0xbbbb000000000002, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.init_data=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{3: 0, 4: 99, 5: 100, 6: 0} {
						if got, err := in.Invoke("array_get_nth", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if got, err := in.Invoke("array_init_data_i16", 0xaaaa000000000002, 0xbbbb000000000005, 0xcccc000000000002); err != nil || len(got) != 0 {
						t.Fatalf("dirty-upper array.init_data_i16=%v,%v", got, err)
					}
					for index, want := range map[uint64]uint64{1: 0, 2: 0x6766, 3: 0x6968, 4: 0} {
						if got, err := in.Invoke("array_get_nth_i16", index); err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("array_get_nth_i16(%d)=%v,%v want %d", index, got, err, want)
						}
					}
					if err = in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 3 {
						t.Fatalf("live objects=%d, want three rooted arrays", live)
					}
					if got, err := in.Invoke("drop_segs"); err != nil || len(got) != 0 {
						t.Fatalf("drop segment=%v,%v", got, err)
					}
					if binary.LittleEndian.Uint32(in.passiveDataDesc[8:]) != 0 {
						t.Fatalf("dropped descriptor=%x", in.passiveDataDesc)
					}
					if _, err = in.Invoke("array_init_data", 0, 0, 0); err != nil {
						t.Fatalf("zero length after drop: %v", err)
					}
					if _, err = in.Invoke("array_init_data", 0, 0, 1); err == nil {
						t.Fatal("non-zero init after drop returned normally")
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayInitDataAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCArrayInitDataCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[3]++; v.InternalEntry[3]++ },
		"function":   func(v *Compiled) { v.Funcs[4].Params[2] = ValI64 },
		"type":       func(v *Compiled) { v.Types[2].Array.Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].ElemSize++ },
		"global":     func(v *Compiled) { v.Globals[1].Mutable = false },
		"data": func(v *Compiled) {
			v.PassiveData[0].Bytes = slices.Clone(v.PassiveData[0].Bytes)
			v.PassiveData[0].Bytes[2] ^= 1
		},
		"initializer": func(v *Compiled) { v.memoryDir.gcArrayGlobals[0].Bits[0]++ },
		"export":      func(v *Compiled) { delete(v.Exports, "array_init_data"); v.Exports["init"] = 3 },
		"feature":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
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
			mutate(candidate)
			if rv64GCArrayInitData(candidate) {
				t.Fatal("hostile mutation retained array.init_data witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCArrayInitDataCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCArrayInitDataBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free array.init_data path used source compilation", err)
	}
}
