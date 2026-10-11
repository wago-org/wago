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
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCStructNumericGlobalsHex = "0061736d01000000018780808000015f027f007f0106978080800002640000410a410bfb00000b64000041144115fb00000b078b808080000202673003000267310301"

func rv64PublicGCStructNumericGlobalsBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCStructNumericGlobalsHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCStructNumericGlobalsCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-struct-numeric-globals.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCStructNumericGlobalsBytes(t))
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

func TestRV64PublicGCStructNumericGlobalsExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCStructNumericGlobalsBytes(t)
	wantSource := [sha256.Size]byte{0x03, 0x87, 0xe5, 0x19, 0xfa, 0x92, 0x1b, 0x90, 0x5d, 0x06, 0x57, 0xa6, 0xfa, 0xfb, 0x63, 0x0a, 0xb7, 0xac, 0xaa, 0x3a, 0x62, 0x82, 0xe3, 0x54, 0xb3, 0xf0, 0xf2, 0xe4, 0x5a, 0xdb, 0xfe, 0xee}
	if len(data) != 67 || sha256.Sum256(data) != wantSource {
		t.Fatal("numeric struct globals source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCStructNumericGlobalsCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCStructNumericGlobals(view) || err != nil {
				t.Fatalf("missing exact numeric struct globals witness: validation=%v product=%v staged=%v required=%v code=%d entries=%v/%v globals=%+v types=%+v descs=%+v flags=%t/%t/%t/%t/%t", err, view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), view.Entry, view.InternalEntry, view.Globals, view.Types, view.GCTypeDescs, view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.needsFuncRefContextHeader)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 32, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
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
					if in.gc == nil {
						t.Fatal("numeric struct globals runtime has no collector")
					}
					state := in.pluginState.Load()
					if state == nil || state.gcGlobalRootCount != 2 || len(state.gcGlobalRoots) != 2 {
						t.Fatalf("numeric struct global root mapping = %#v", state)
					}
					var seen [2]bool
					for _, mapping := range state.gcGlobalRoots {
						if mapping.GlobalIndex >= uint32(len(seen)) || seen[mapping.GlobalIndex] {
							t.Fatalf("duplicate/out-of-range global root %+v", mapping)
						}
						seen[mapping.GlobalIndex] = true
						wantRef := nativegc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
						gotRef, err := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if err != nil || gotRef != wantRef || !gotRef.IsObj() {
							t.Fatalf("global %d root=%v,%v want %v", mapping.GlobalIndex, gotRef, err, wantRef)
						}
						wantFields := [2][2]int32{{10, 11}, {20, 21}}
						for field, want := range wantFields[mapping.GlobalIndex] {
							value, err := in.gc.StructGet(gotRef, uint32(field))
							if err != nil || value.I32() != want {
								t.Fatalf("global %d field %d=%v,%v want %d", mapping.GlobalIndex, field, value, err, want)
							}
						}
					}
					if err := in.gc.CollectFull(nativegc.EmptyRoots{}); err != nil {
						t.Fatal(err)
					}
					if stats := in.gc.Stats(); stats.LiveObjects != 2 {
						t.Fatalf("post-collection stats=%+v, want two rooted objects", stats)
					}
					for _, mapping := range state.gcGlobalRoots {
						rooted, err := in.gc.CheckedGlobalSlot(mapping.SlotIndex)
						if err != nil || !rooted.IsObj() {
							t.Fatalf("post-collection global %d root=%v,%v", mapping.GlobalIndex, rooted, err)
						}
						wantFields := [2][2]int32{{10, 11}, {20, 21}}
						for field, want := range wantFields[mapping.GlobalIndex] {
							value, err := in.gc.StructGet(rooted, uint32(field))
							if err != nil || value.I32() != want {
								t.Fatalf("post-collection global %d field %d=%v,%v want %d", mapping.GlobalIndex, field, value, err, want)
							}
						}
					}
					for _, name := range []string{"g0", "g1"} {
						if got, err := in.Global(name); got != 0 || err == nil || !strings.Contains(err.Error(), "reference type") {
							t.Fatalf("Global(%q)=%d,%v want opaque reference rejection", name, got, err)
						}
						if _, err := in.GlobalValue(name); err == nil || !strings.Contains(err.Error(), "public GC/exception reference egress is unsupported") {
							t.Fatalf("GlobalValue(%q)=%v want explicit GC egress rejection", name, err)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCStructNumericGlobalsAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCStructNumericGlobalsCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"entry":          func(v *Compiled) { v.Entry = []int{0}; v.InternalEntry = []int{0} },
		"type-field":     func(v *Compiled) { v.Types[0].Fields[1].Mutable = false },
		"descriptor":     func(v *Compiled) { v.GCTypeDescs[0].Fields[1].Offset++ },
		"global":         func(v *Compiled) { v.Globals[1].ValueTypeIndex++ },
		"initializer":    func(v *Compiled) { v.memoryDir.gcStructGlobals[1].Bits[1]-- },
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
			if rv64GCStructNumericGlobals(candidate) {
				t.Fatal("hostile retained mutation kept numeric struct globals witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile numeric struct globals mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCStructNumericGlobalsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCStructNumericGlobalsBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free numeric struct globals path used source compilation", err)
	}
}
