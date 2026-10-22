//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func rv64I31OfficialRefTestCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_I31_REF_TEST_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE")
	path := filepath.Join(dir, "i31-official-ref-test.wago")
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
	original, err := rv64I31Config().Compile(stagedGCI31RefTestGateBytes(t))
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

func TestRV64PublicI31OfficialRefTestExecutionAndArtifact(t *testing.T) {
	data := stagedGCI31RefTestGateBytes(t)
	wantSource := [sha256.Size]byte{0x15, 0xae, 0x51, 0xeb, 0x55, 0x7d, 0xb9, 0x16, 0x94, 0xfc, 0x8c, 0xb2, 0xe2, 0xca, 0x14, 0x87, 0x92, 0xee, 0xc4, 0xae, 0x2b, 0x52, 0x4c, 0x61, 0x0e, 0xdf, 0x80, 0x05, 0xa0, 0x48, 0x37, 0xec}
	if len(data) != 255 || sha256.Sum256(data) != wantSource {
		t.Fatal("official i31 ref.test source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			c := rv64I31OfficialRefTestCompile(t, artifact)
			view := c.executionView()
			wantProduct, wantStaged := stagedGCI31ProductRefTest, CoreFeatureTypedFunctionReferences|CoreFeatureGC
			if artifact {
				wantProduct, wantStaged = stagedGCI31ProductCore, CoreFeatureGC
			}
			if !rv64I31OfficialRefTest(view) || !rv64I31Core(view) || railshotValidateCompiled(view) != nil || c.stagedGCI31Product() != wantProduct || c.stagedFeatures() != wantStaged {
				t.Fatal("missing exact retained official i31 ref.test witness")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
			defer rt.Close()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if in.gc != nil {
				t.Fatal("non-allocating official i31 ref.test provisioned a collector")
			}
			invoke := func(name string, want uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || !slices.Equal(got, []uint64{want}) {
					t.Fatalf("%s%v=%v err=%v want=[%d]", name, args, got, err, want)
				}
			}
			invoke("null-data", 1)
			invoke("null-i31", 1)
			invoke("i31", 2, 0xdeadbeef8000002a)
			invoke("any", 2, 0xcafebabe00000007)
			invoke("eq", 2, 0xfeedface00000007)
			invoke("struct", 0, 0xffffffff00000007)
			invoke("array", 0, 0x1234567800000007)
			_ = c.Close() // A live instance retains its executable lease.
			invoke("i31", 2, 0xaaaaaaaa80000000)
		})
	}
}

func TestRV64PublicI31OfficialRefTestCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(stagedGCI31RefTestGateBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free official i31 ref.test path used source compilation", err)
	}
}

func TestRV64PublicI31OfficialRefTestAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64I31OfficialRefTestCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) {
			v.code = slices.Clone(v.code)
			v.code[len(v.code)-1] ^= 1
		},
		"entry":      func(v *Compiled) { v.Entry[3], v.InternalEntry[3] = 2128, 2128 },
		"function":   func(v *Compiled) { v.Funcs[2].Params[0] = ValI64 },
		"type-key":   func(v *Compiled) { v.FuncTypeID[4] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].Final = false },
		"export": func(v *Compiled) {
			delete(v.Exports, "array")
			v.Exports["array_x"] = 6
		},
		"zero-export": func(v *Compiled) {
			delete(v.Exports, "null-data")
			v.Exports["null-x"] = 0
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
			if rv64I31OfficialRefTest(candidate) || rv64I31Core(candidate) {
				t.Fatal("hostile retained mutation retained the official i31 ref.test witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained i31 ref.test mutation admitted", err)
			}
			blob, err := marshalCompiled(candidate)
			if err != nil {
				return
			}
			loaded, loadErr := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
				if loadErr == nil && rv64I31OfficialRefTest(loaded.executionView()) {
					return // Canonical encoding repaired a derived/redundant field.
				}
			}
			if loadErr == nil {
				t.Fatal("hostile official i31 ref.test artifact mutation admitted")
			}
		})
	}
}
