//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCTypeSubtypingRefTestSingleHex = "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016402004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"

func rv64PublicGCTypeSubtypingRefTestSingleBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestSingleHex)
	want := [sha256.Size]byte{0x47, 0xa4, 0xb6, 0x08, 0x0c, 0x4c, 0x63, 0x22, 0x1e, 0x32, 0xdd, 0x45, 0x2f, 0xd9, 0xbc, 0x66, 0x21, 0xc9, 0x15, 0xb3, 0xf1, 0x13, 0xe1, 0x4e, 0x46, 0xe0, 0xf2, 0xff, 0x90, 0x72, 0x80, 0xd5}
	if err != nil || len(b) != 122 || sha256.Sum256(b) != want {
		t.Fatal("single ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestSingleCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-single.wago")
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
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestSingleBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		original.Close()
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			original.Close()
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			original.Close()
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestRV64PublicGCTypeSubtypingRefTestSingleExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestSingleCompile(t, artifact)
			view := compiled.executionView()
			if !rv64ArtifactContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public ref.test execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("single ref.test unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			rt := NewRuntime()
			mod, err := rt.Module(compiled)
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
			mod.Close()
			if err != nil {
				compiled.Close()
				rt.Close()
				t.Fatal(err)
			}
			if in.gc != nil || len(in.funcRefDescs) != (len(view.Funcs)+1)*coreruntime.FuncRefDescBytes || cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatal("single ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 1 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want [1]", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live single ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("single ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestSingleAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestSingleCompile(t, false)
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code": func(v *Compiled) {
			v.code = slices.Clone(v.code)
			v.code[len(v.code)-1] ^= 1
		},
		"entry":      func(v *Compiled) { v.Entry[1] += 4; v.InternalEntry[1] += 4 },
		"function":   func(v *Compiled) { v.Funcs[0].TypeIndex ^= 1 },
		"type":       func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
		"type-key":   func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"escape":     func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"function-abi": func(v *Compiled) {
			v.nativeReferenceFunctionABI = !v.nativeReferenceFunctionABI
		},
		"missing-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("hostile retained mutation admitted", err)
			}
			if name == "feature" {
				rv64RequireRestoredGCFeature(t, candidate)
				return
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestSinglePublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestSingleCompile(t, false)
	rt := NewRuntime()
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if err != nil {
		t.Fatal("public single ref.test instantiation", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestSingleCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestSingleBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free single ref.test path used source compilation", err)
	}
}
