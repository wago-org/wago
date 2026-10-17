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

const rv64PublicGCTypeSubtypingRefTestMultiFirstHex = "0061736d0100000001e480808000064e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406004e025001066000005f006000027f7f03838080800002080a078780808000010372756e000109858080800001030001000a9980808000028280808000000b8c8080800000d200fb1400d200fb14040b"

func rv64PublicGCTypeSubtypingRefTestMultiFirstBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestMultiFirstHex)
	want := [sha256.Size]byte{0x5f, 0x08, 0x06, 0x74, 0xa0, 0x0a, 0x73, 0xb3, 0xdb, 0xa3, 0x91, 0xbb, 0x19, 0x67, 0xaa, 0x22, 0xf4, 0xdd, 0x6f, 0x1b, 0x43, 0xb9, 0xb4, 0x9a, 0xff, 0x08, 0x52, 0x8c, 0x33, 0x05, 0xaa, 0x6b}
	if err != nil || len(b) != 178 || sha256.Sum256(b) != want {
		t.Fatal("first multi-result ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestMultiFirstCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-multi-first.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiFirstBytes(t))
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

func TestRV64PublicGCTypeSubtypingRefTestMultiFirstExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestMultiFirstCompile(t, artifact)
			view := compiled.executionView()
			if !rv64ArtifactContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public ref.test execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("first multi-result ref.test unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatal("first multi-result ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || !slices.Equal(got, []uint64{1, 1}) {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want [1 1]", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live first multi-result ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("first multi-result ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiFirstAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiFirstCompile(t, false)
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":                       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":                      func(v *Compiled) { v.Entry[1] += 4; v.InternalEntry[1] += 4 },
		"function":                   func(v *Compiled) { v.Funcs[0].TypeIndex ^= 1 },
		"type":                       func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
		"type-key":                   func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		"descriptor":                 func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
		"feature":                    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"escape":                     func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"function-abi":               func(v *Compiled) { v.nativeReferenceFunctionABI = !v.nativeReferenceFunctionABI },
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
	for name, prior := range map[string]*Compiled{
		"single":          rv64PublicGCTypeSubtypingRefTestSingleCompile(t, false),
		"recursive":       rv64PublicGCTypeSubtypingRefTestRecursiveCompile(t, false),
		"direction-false": rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, false),
		"supertype-true":  rv64PublicGCTypeSubtypingRefTestSupertypeTrueCompile(t, false),
	} {
		t.Run("multi-metadata-"+name+"-image", func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			candidate.code = slices.Clone(prior.executionView().code)
			if err := candidate.validate(); err == nil {
				t.Fatal("cross-paired retained metadata and prior image admitted", err)
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
		t.Run(name+"-metadata-multi-image", func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(prior.executionView())
			candidate.code = slices.Clone(base.code)
			if err := candidate.validate(); err == nil {
				t.Fatal("cross-paired prior metadata and multi image admitted", err)
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
		prior.Close()
	}
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestMultiFirstPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiFirstCompile(t, false)
	rt := NewRuntime()
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if err != nil {
		t.Fatal("first multi-result ref.test public runtime instantiation failed", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiFirstCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiFirstBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free first multi-result ref.test path used source compilation", err)
	}
}
