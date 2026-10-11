//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func rv64GCStructMutationCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-struct-mutation.wago")
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
	original, err := rv64I31Config().Compile(stagedGCStructMutationBytes(t))
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

func TestRV64PublicGCStructMutationExecutionAndArtifact(t *testing.T) {
	data := stagedGCStructMutationBytes(t)
	wantSource := [sha256.Size]byte{0xe9, 0xf7, 0xa7, 0xec, 0x88, 0xc5, 0x66, 0x84, 0xad, 0x5b, 0x96, 0xe2, 0xa5, 0x47, 0x17, 0x65, 0xab, 0x28, 0x35, 0xdd, 0xea, 0x14, 0x06, 0x90, 0x06, 0xda, 0x51, 0xa9, 0x6e, 0xd5, 0xe8, 0x91}
	if len(data) != 106 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc struct mutation source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCStructMutationCompile(t, artifact)
			view := compiled.executionView()
			exact, validation := rv64GCStructMutation(view), railshotValidateCompiled(view)
			if !exact || !rv64GCStructMutationExecutionCode(view) || validation != nil {
				t.Fatalf("missing exact retained gc struct mutation witness: exact=%t validation=%v product=%v staged=%v required=%v code=%d/%x entries=%v/%v", exact, validation, view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			for _, profile := range []struct {
				name                      string
				config                    GCConfig
				requireCollectsOnAllocate bool
			}{{name: "default-gc"}, {name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}, requireCollectsOnAllocate: true}} {
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
						t.Fatal("allocating gc struct mutation module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					if got, err := in.Invoke("get"); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("get()=%v,%v want [0]", got, err)
					}
					for i, raw := range []uint64{0, 1, 0xffffffff, 0xdeadbeef0000002a, 0xffffffffffffffff} {
						want := uint64(uint32(raw))
						got, err := in.Invoke("set", raw)
						if err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatalf("set case %d raw=%#x got=%v,%v want=[%#x]", i, raw, got, err, want)
						}
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCStructMutationCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCStructMutationBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc struct mutation path used source compilation", err)
	}
}

func TestRV64PublicGCStructMutationAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCStructMutationCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[1], v.InternalEntry[1] = 0, 0 },
		"function":   func(v *Compiled) { v.Funcs[1].Params[0] = ValI64 },
		"type-field": func(v *Compiled) { v.Types[0].Fields[0].Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Size++ },
		"table-init": func(v *Compiled) { v.HasTableInitFunc = true },
		"inactive-table-init": func(v *Compiled) {
			v.TableInitFunc = 1
		},
		"export": func(v *Compiled) {
			delete(v.Exports, "set")
			v.Exports["write"] = 1
		},
		"missing-gc": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) {
			v.requiredFeatures |= CoreFeatureSIMD
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCStructMutation(candidate) {
				t.Fatal("hostile retained mutation kept the gc struct mutation witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained gc struct mutation admitted", err)
			}
		})
	}
}
