//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

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

func rv64GCArrayLocalCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-local.wago")
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
	original, err := rv64I31Config().Compile(stagedGCArrayNumericLocalBytes(t))
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

func TestRV64PublicGCArrayLocalExecutionAndArtifact(t *testing.T) {
	data := stagedGCArrayNumericLocalBytes(t)
	wantSource := [sha256.Size]byte{0xcf, 0xa5, 0x15, 0xe6, 0x6b, 0x09, 0x4d, 0xb4, 0x34, 0xe5, 0x9a, 0x3b, 0xbd, 0x21, 0xb6, 0x6e, 0x99, 0xf3, 0x91, 0xaa, 0xa5, 0x26, 0x14, 0xe2, 0xfa, 0x1a, 0x5f, 0xec, 0x4f, 0x0e, 0x7b, 0x3b}
	if len(data) != 146 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array local source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayLocalCompile(t, artifact)
			view := compiled.executionView()
			validationErr := railshotValidateCompiled(view)
			if !rv64ArtifactContractValid(view) || validationErr != nil {
				t.Fatalf("missing exact retained gc array local witness: validation=%v products=%v/%v staged=%v required=%v code=%d/%x entries=%v/%v funcs=%+v types=%+v descs=%+v", validationErr, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), view.stagedFeatures(), view.requiredFeatures, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry, view.Funcs, view.Types, view.GCTypeDescs)
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
					rt := NewRuntime()
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
						t.Fatal("allocating gc array local module has no collector")
					}
					if profile.requireCollectsOnAllocate && !in.gc.CollectsOnAllocation() {
						t.Fatal("stress collector does not collect on allocation")
					}
					if got, err := in.Invoke("get", 0xaaaa00000003, 0xbbbb00000002); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("get()=%v err=%v want=[0]", got, err)
					}
					if got, err := in.Invoke("set_get", 0xaaaa00000003, 0xbbbb00000001, 0xcccc000012345678); err != nil || !slices.Equal(got, []uint64{0x12345678}) {
						t.Fatalf("set_get()=%v err=%v want=[0x12345678]", got, err)
					}
					if got, err := in.Invoke("len", 0xaaaa00000007); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatalf("len()=%v err=%v want=[7]", got, err)
					}
					if _, err = in.Invoke("get", 3, 3); err == nil {
						t.Fatal("out-of-bounds get succeeded")
					}
					if _, err = in.Invoke("set_get", 3, 3, 1); err == nil {
						t.Fatal("out-of-bounds set succeeded")
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestRV64PublicGCArrayLocalCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCArrayNumericLocalBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc array local path used source compilation", err)
	}
}

func TestRV64PublicGCArrayLocalAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCArrayLocalCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":     func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":    func(v *Compiled) { v.Entry[1], v.InternalEntry[1] = 800, 800 },
		"function": func(v *Compiled) { v.Funcs[1].Params[2] = ValI64 },
		"type-array": func(v *Compiled) {
			v.Types[0].Array.Mutable = false
		},
		"declared-param": func(v *Compiled) { v.Types[1].Params[0].Kind = ValueTypeI64 },
		"type-key":       func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor":     func(v *Compiled) { v.GCTypeDescs[0].ElemSize++ },
		"export": func(v *Compiled) {
			delete(v.Exports, "get")
			v.Exports["read"] = 0
		},
		"missing-gc": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"extra-feature": func(v *Compiled) {
			v.requiredFeatures |= CoreFeatureMemory64
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if rv64ArtifactContractValid(candidate) {
				t.Fatal("hostile retained mutation retained the gc array local witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained gc array local mutation admitted", err)
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
}
