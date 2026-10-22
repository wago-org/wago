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

const rv64PublicGCTypeSubtypingRefTestMultiPairsHex = "0061736d0100000001aa80808000034e025000600001647050010060000164004e025000600001647050010260000164026000047f7f7f7f03848080800003000104078780808000010372756e00020989808080000203000100030001010aac8080800003838080800000000b838080800000000b968080800000d200fb1400d200fb1402d201fb1401d201fb14030b"

func rv64PublicGCTypeSubtypingRefTestMultiPairsBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestMultiPairsHex)
	want := [sha256.Size]byte{0xb5, 0x61, 0xb7, 0xbc, 0xd1, 0x31, 0x22, 0x3f, 0x57, 0x3b, 0x78, 0x7f, 0xf0, 0x02, 0xce, 0xc3, 0xef, 0x83, 0xd1, 0xcb, 0x90, 0xfc, 0x44, 0x0e, 0xc2, 0x4d, 0x34, 0x7c, 0xc7, 0x89, 0xdf, 0x1d}
	if err != nil || len(b) != 144 || sha256.Sum256(b) != want {
		t.Fatal("paired multi-result ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestMultiPairsCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-multi-pairs.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiPairsBytes(t))
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

func TestRV64PublicGCTypeSubtypingRefTestMultiPairsExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestMultiPairsCompile(t, artifact)
			view := compiled.executionView()
			if !rv64ArtifactContractValid(view) || view.validate() != nil {
				compiled.Close()
				t.Fatal("missing public ref.test execution contract")
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatal("paired multi-result ref.test unpublished mapping state")
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
				t.Fatal("paired multi-result ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || !slices.Equal(got, []uint64{1, 1, 1, 1}) {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want [1 1 1 1]", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live paired multi-result ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("paired multi-result ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiPairsAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiPairsCompile(t, false)
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":                       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":                      func(v *Compiled) { v.Entry[2] += 4; v.InternalEntry[2] += 4 },
		"function":                   func(v *Compiled) { v.Funcs[0].TypeIndex ^= 1 },
		"type":                       func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
		"type-key":                   func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		"descriptor":                 func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
		"feature":                    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMultiValue },
		"escape":                     func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"function-abi":               func(v *Compiled) { v.nativeReferenceFunctionABI = !v.nativeReferenceFunctionABI },
		"missing-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base)
			mutate(candidate)
			if err := candidate.validate(); err == nil {
				t.Fatalf("mutation %s admitted: contract=%t primitive=%t rooted=%t legacy=%d functionABI=%t", name, rv64ArtifactContractValid(candidate), rv64PrimitiveReferenceContract(candidate), rv64FreshRootedGC(candidate), rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(candidate, true, true), candidate.nativeReferenceFunctionABI)
			}
			if name == "feature" {
				rv64RequireRestoredGCFeature(t, candidate)
				return
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	first := rv64PublicGCTypeSubtypingRefTestMultiFirstCompile(t, false)
	for _, pair := range []struct {
		name                     string
		metadataOwner, codeOwner *Compiled
	}{
		{"pairs-metadata-first-image", base, first.executionView()},
		{"first-metadata-pairs-image", first.executionView(), base},
	} {
		t.Run(pair.name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(pair.metadataOwner)
			candidate.code = slices.Clone(pair.codeOwner.code)
			if candidate.validate() == nil {
				t.Fatal("cross-paired multi-result artifact admitted")
			}
			rv64RequireRejectedArtifact(t, candidate)
		})
	}
	first.Close()
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestMultiPairsPublicRuntime(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiPairsCompile(t, false)
	rt := NewRuntime()
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if err != nil {
		t.Fatal("paired multi-result ref.test public runtime instantiation failed", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiPairsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiPairsBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free paired multi-result ref.test path used source compilation", err)
	}
}
