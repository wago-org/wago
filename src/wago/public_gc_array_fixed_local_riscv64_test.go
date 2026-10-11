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

func rv64GCArrayFixedLocalCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-array-fixed-local.wago")
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
	original, err := rv64I31Config().Compile(stagedGCArrayNumericFixedLocalBytes(t))
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

func TestRV64PublicGCArrayFixedLocalExecutionAndArtifact(t *testing.T) {
	data := stagedGCArrayNumericFixedLocalBytes(t)
	wantSource := [sha256.Size]byte{0x41, 0x26, 0x8d, 0xe7, 0xa6, 0xcb, 0x79, 0xd2, 0x2f, 0xc7, 0x5f, 0x69, 0x07, 0xc8, 0xe8, 0xae, 0xe8, 0xf8, 0x70, 0x25, 0x22, 0x20, 0x91, 0xb7, 0x62, 0x1c, 0x4f, 0xa5, 0x93, 0x2c, 0x46, 0x45}
	if len(data) != 139 || sha256.Sum256(data) != wantSource {
		t.Fatal("gc array fixed local source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64GCArrayFixedLocalCompile(t, artifact)
			view := compiled.executionView()
			if err := railshotValidateCompiled(view); !rv64GCArrayFixedLocal(view) || !rv64GCArrayFixedLocalExecutionCode(view) || err != nil {
				t.Fatalf("missing exact retained gc array fixed local witness: validation=%v products=%v/%v code=%d/%x entries=%v/%v", err, view.stagedGCStructProduct(), view.stagedGCArrayProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			for _, profile := range []struct {
				name   string
				config GCConfig
			}{
				{name: "default-gc"},
				{name: "stress-gc", config: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
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
					if in.gc == nil || profile.name == "stress-gc" && !in.gc.CollectsOnAllocation() {
						t.Fatal("fixed local collector profile unavailable")
					}
					for _, tc := range []struct {
						index uint64
						want  uint64
					}{{0xdddd000000000000, 0x80000001}, {0xdddd000000000001, 0x92345678}, {0xdddd000000000002, 0xf0000003}} {
						if got, err := in.Invoke("get", 0xaaaa000080000001, 0xbbbb000092345678, 0xcccc0000f0000003, tc.index); err != nil || !slices.Equal(got, []uint64{tc.want}) {
							t.Fatalf("get(%#x)=%v err=%v", tc.index, got, err)
						}
					}
					if got, err := in.Invoke("set_get", 0xaaaa000000000001, 0xbbbb000000000002, 0xcccc000000000003, 0xdddd000000000002, 0xeeee000087654321); err != nil || !slices.Equal(got, []uint64{0x87654321}) {
						t.Fatalf("set_get()=%v err=%v", got, err)
					}
					if got, err := in.Invoke("len", 0xaaaa000000000001, 0xbbbb000000000002, 0xcccc000000000003); err != nil || !slices.Equal(got, []uint64{3}) {
						t.Fatalf("len()=%v err=%v", got, err)
					}
					if _, err = in.Invoke("get", 1, 2, 3, 3); err == nil {
						t.Fatal("out-of-bounds get succeeded")
					}
					if _, err = in.Invoke("set_get", 1, 2, 3, 3, 9); err == nil {
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

func TestRV64PublicGCArrayFixedLocalCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCArrayNumericFixedLocalBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc array fixed local path used source compilation", err)
	}
}

func TestRV64PublicGCArrayFixedLocalAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64GCArrayFixedLocalCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[1], v.InternalEntry[1] = 848, 848 },
		"function":   func(v *Compiled) { v.Funcs[1].Params[4] = ValI64 },
		"type":       func(v *Compiled) { v.Types[0].Array.Mutable = false },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].ElemSize++ },
		"export": func(v *Compiled) {
			delete(v.Exports, "get")
			v.Exports["read"] = 0
		},
		"feature": func(v *Compiled) { v.requiredFeatures |= CoreFeatureMemory64 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCArrayFixedLocal(candidate) {
				t.Fatal("hostile retained mutation retained the fixed local witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained fixed local mutation admitted", err)
			}
		})
	}
}
