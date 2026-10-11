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
)

const rv64PublicGCStructPackedHex = "0061736d01000000019680808000035f0478007801770077016000027f7f60017f027f7f038b808080000a0101010101010101020206a580808000026400004100410141024103fb00000b64000041fe0141ff0141feff0341ffff03fb00000b07c7818080000c026730030002673103010f6765745f7061636b65645f67305f3000000f6765745f7061636b65645f67315f3000010f6765745f7061636b65645f67305f3100020f6765745f7061636b65645f67315f3100030f6765745f7061636b65645f67305f3200040f6765745f7061636b65645f67315f3200050f6765745f7061636b65645f67305f3300060f6765745f7061636b65645f67315f330007137365745f6765745f7061636b65645f67305f310008137365745f6765745f7061636b65645f67305f3300090acf818080000a8e80808000002300fb0300002300fb0400000b8e80808000002301fb0300002301fb0400000b8e80808000002300fb0300012300fb0400010b8e80808000002301fb0300012301fb0400010b8e80808000002300fb0300022300fb0400020b8e80808000002301fb0300022301fb0400020b8e80808000002300fb0300032300fb0400030b8e80808000002301fb0300032301fb0400030b96808080000023002000fb0500012300fb0300012300fb0400010b96808080000023002000fb0500032300fb0300032300fb0400030b"

func rv64PublicGCStructPackedBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCStructPackedHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCStructPackedCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-struct-packed.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCStructPackedBytes(t))
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

func TestRV64PublicGCStructPackedExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCStructPackedBytes(t)
	wantSource := [sha256.Size]byte{0x96, 0xa4, 0x75, 0x80, 0xd9, 0xd8, 0x60, 0x53, 0xfd, 0xc3, 0x30, 0x6a, 0x59, 0xb2, 0x21, 0x3f, 0xd5, 0x81, 0xb9, 0xba, 0x19, 0x87, 0xda, 0x0c, 0x24, 0x0f, 0xcc, 0xb7, 0xbe, 0x1f, 0x6e, 0x58}
	if len(data) != 514 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc/struct packed source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCStructPackedCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCStructPacked(view) || !rv64GCStructPackedExecutionCode(view) || err != nil {
				t.Fatalf("missing exact gc/struct packed witness: validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v", err, view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs)
			}
			if params, results, err := railshotImportedCallSlots(view, 1, 2); err != nil || params != 4 || results != 2 {
				t.Fatalf("gc/struct packed call arena=%d/%d,%v, want 4/2", params, results, err)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					if in.gc == nil || len(in.serArgs) < 4*8 {
						t.Fatalf("gc/struct packed runtime provisioning: collector=%v argument arena=%d", in.gc != nil, len(in.serArgs))
					}
					var roots []gcGlobalRootMapping
					state := in.pluginState.Load()
					if state != nil {
						roots = append(roots, state.gcGlobalRoots[:state.gcGlobalRootCount]...)
					}
					stagedRoots := len(roots)
					public := in.publicGCState()
					roots = append(roots, public.globalRoots...)
					if len(roots) != 2 || view.stagedGCStructProduct() == stagedGCStructGeneric && (stagedRoots != 0 || len(public.globalRoots) != 2) ||
						view.stagedGCStructProduct() != stagedGCStructGeneric && (stagedRoots != 2 || len(public.globalRoots) != 0) {
						t.Fatalf("gc/struct packed global roots staged/generic=%d/%d, want exact two-slot representation", stagedRoots, len(public.globalRoots))
					}
					var seenRoots [2]bool
					for _, mapping := range roots {
						if mapping.GlobalIndex >= uint32(len(seenRoots)) || seenRoots[mapping.GlobalIndex] {
							t.Fatalf("gc/struct packed duplicate/out-of-range global root %+v", mapping)
						}
						seenRoots[mapping.GlobalIndex] = true
						want := uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef))
						rooted, err := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if err != nil || uint32(rooted) != want || !rooted.IsObj() {
							t.Fatalf("gc/struct packed global root %d=%v,%v, want object %d", mapping.GlobalIndex, rooted, err, want)
						}
					}
					for name, want := range map[string][]uint64{
						"get_packed_g0_0": {0, 0}, "get_packed_g1_0": {0xfffffffe, 254},
						"get_packed_g0_1": {1, 1}, "get_packed_g1_1": {0xffffffff, 255},
						"get_packed_g0_2": {2, 2}, "get_packed_g1_2": {0xfffffffe, 65534},
						"get_packed_g0_3": {3, 3}, "get_packed_g1_3": {0xffffffff, 65535},
					} {
						got, err := in.Invoke(name)
						if err != nil || !slices.Equal(got, want) {
							t.Fatalf("%s=%v,%v want %v", name, got, err, want)
						}
					}
					if got, err := in.Invoke("set_get_packed_g0_1", 0xffff000000000107); err != nil || !slices.Equal(got, []uint64{7, 7}) {
						t.Fatalf("set i8=%v,%v", got, err)
					}
					if got, err := in.Invoke("set_get_packed_g0_3", 0xaaaa000000010007); err != nil || !slices.Equal(got, []uint64{7, 7}) {
						t.Fatalf("set i16=%v,%v", got, err)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for name, want := range map[string][]uint64{"get_packed_g1_0": {0xfffffffe, 254}, "get_packed_g1_3": {0xffffffff, 65535}} {
						if got, err := in.Invoke(name); err != nil || !slices.Equal(got, want) {
							t.Fatalf("post-collection %s=%v,%v want %v", name, got, err, want)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCStructPackedAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCStructPackedCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":           func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":          func(v *Compiled) { v.Entry[9]++; v.InternalEntry[9]++ },
		"function":       func(v *Compiled) { v.Funcs[8].Params[0] = ValI64 },
		"type-field":     func(v *Compiled) { v.Types[0].Fields[1].Mutable = false },
		"type-key":       func(v *Compiled) { v.FuncTypeID[9] ^= 1 },
		"type-key-short": func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:9] },
		"descriptor":     func(v *Compiled) { v.GCTypeDescs[0].Fields[3].Offset++ },
		"global":         func(v *Compiled) { v.Globals[1].ValueTypeIndex++ },
		"initializer":    func(v *Compiled) { v.memoryDir.gcStructGlobals[1].Bits[3]-- },
		"export":         func(v *Compiled) { delete(v.Exports, "get_packed_g0_0"); v.Exports["get"] = 0 },
		"global-export":  func(v *Compiled) { delete(v.GlobalExports, "g0"); v.GlobalExports["global"] = 0 },
		"feature":        func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"inactive-start": func(v *Compiled) { v.StartLocalFunc = 1 },
		"extra-table":    func(v *Compiled) { v.extraTables = []tableDef{{}} },
		"table-export":   func(v *Compiled) { v.tableExports = map[string]int{"table": 0} },
		"table-init":     func(v *Compiled) { v.HasTableInitFunc = true },
		"compact-data":   func(v *Compiled) { v.compactData = &compactActiveData{} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			if base.memoryDir != nil {
				d := *base.memoryDir
				d.gcStructGlobals = slices.Clone(base.memoryDir.gcStructGlobals)
				for i := range d.gcStructGlobals {
					d.gcStructGlobals[i].Bits = slices.Clone(d.gcStructGlobals[i].Bits)
				}
				candidate.memoryDir = &d
			}
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCStructPacked(candidate) {
				t.Fatal("hostile retained mutation retained gc/struct packed witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile gc/struct packed mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCStructPackedCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCStructPackedBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc/struct packed path used source compilation", err)
	}
}
