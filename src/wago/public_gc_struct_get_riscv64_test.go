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

func rv64GCStructGetCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_STRUCT_GET_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-struct-get.wago")
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
	original, err := rv64I31Config().Compile(stagedGCStructGetOnlyBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	c := original
	if artifact || mode == "prepare" {
		blob, err := original.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loaded.Close() })
			c = loaded
		}
	}
	return c
}

func TestRV64PublicGCStructGetExecutionAndArtifact(t *testing.T) {
	data := stagedGCStructGetOnlyBytes(t)
	wantSource := [sha256.Size]byte{0xf5, 0xfc, 0x57, 0xa9, 0xa6, 0xb9, 0x59, 0xa1, 0xa6, 0x89, 0x38, 0x5c, 0xb7, 0x90, 0x50, 0xb6, 0x99, 0x8c, 0x86, 0x7c, 0x61, 0xea, 0xfd, 0x65, 0xff, 0x03, 0xb2, 0xd5, 0x7d, 0x12, 0x8f, 0xcf}
	if len(data) != 65 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc struct.get source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCStructGetCompile(t, artifact)
			view := compiled.executionView()
			if !rv64GCStructGet(view) || !rv64GCStructGetExecutionCode(view) || railshotValidateCompiled(view) != nil {
				t.Fatalf("missing exact retained gc struct.get witness: product=%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v flags=%t/%t/%t/%t/%t", view.stagedGCStructProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs, view.nativeContextTrapABI, view.nativeTableContextABI, view.nativeTableReferenceABI, view.registerABIDisabled, view.needsFuncRefContextHeader)
			}
			for _, profile := range []struct {
				name                      string
				config                    GCConfig
				requireCollectsOnAllocate bool
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}, requireCollectsOnAllocate: true},
			} {
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
						t.Fatal("allocating gc struct.get module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					got, err := in.Invoke("get")
					if err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("get()=%v err=%v want=[0]", got, err)
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCStructGetCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCStructGetOnlyBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc struct.get path used source compilation", err)
	}
}

func TestRV64PublicGCStructGetAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCStructGetCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[0], v.InternalEntry[0] = 16, 16 },
		"function":   func(v *Compiled) { v.Funcs[0].Results[0] = ValI64 },
		"type-field": func(v *Compiled) { v.Types[0].Fields[0].Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Size++ },
		"export": func(v *Compiled) {
			delete(v.Exports, "get")
			v.Exports["read"] = 0
		},
		"missing-gc": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) {
			v.requiredFeatures |= CoreFeatureTypedFunctionReferences
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCStructGet(candidate) {
				t.Fatal("hostile retained mutation retained the gc struct.get witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained gc struct.get mutation admitted", err)
			}
		})
	}
}
