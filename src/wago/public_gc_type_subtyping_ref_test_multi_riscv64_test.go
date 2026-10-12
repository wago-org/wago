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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64PublicGCTypeSubtypingRefTestMultiHex = "0061736d0100000001d280808000054e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e02500102600001647050010660000164066000087f7f7f7f7f7f7f7f03848080800003040508078780808000010372756e00020989808080000203000100030001010ac08080800003838080800000000b838080800000000baa8080800000d200fb1400d200fb1402d201fb1400d201fb1402d200fb1404d200fb1406d201fb1405d201fb14070b"

func rv64PublicGCTypeSubtypingRefTestMultiBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestMultiHex)
	want := [sha256.Size]byte{0x89, 0x3d, 0xcf, 0x05, 0x8c, 0x5b, 0x28, 0x43, 0x65, 0x67, 0x02, 0x8a, 0xb4, 0x1b, 0xfb, 0x40, 0x9c, 0x5f, 0x1a, 0xcc, 0x73, 0x7e, 0x76, 0x4a, 0x3d, 0xfc, 0xc5, 0x1f, 0x6b, 0xe8, 0x20, 0x0e}
	if err != nil || len(b) != 204 || sha256.Sum256(b) != want {
		t.Fatal("full multi-result ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestMultiCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-multi.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiBytes(t))
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

func TestRV64PublicGCTypeSubtypingRefTestMultiExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestMultiCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			codeLen, codeDigest := len(view.code), sha256.Sum256(view.code)
			variant := rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(view, false, true)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataLens[6] || sha256.Sum256(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataDigests[6] || variant != 7 || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRefTestMulti || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact full multi-result ref.test witness: metadata=%d/%x err=%v variant=%d product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, variant, view.stagedGCTypeSubtypingProduct(), validation, codeLen, codeDigest, view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < codeLen || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatal("full multi-result ref.test unpublished mapping state")
			}
			cfg := rv64I31Config()
			cfg.railshotState.tailProducts = true
			rt := NewRuntime(WithRuntimeConfig(cfg))
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
				t.Fatal("full multi-result ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || !slices.Equal(got, []uint64{1, 1, 1, 1, 1, 1, 1, 1}) {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want eight normalized true results", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live full multi-result ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("full multi-result ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiCompile(t, false)
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":         func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":        func(v *Compiled) { v.Entry[2] += 4; v.InternalEntry[2] += 4 },
		"function":     func(v *Compiled) { v.Funcs[0].TypeIndex ^= 1 },
		"type":         func(v *Compiled) { v.Types[0].Final = !v.Types[0].Final },
		"type-key":     func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		"descriptor":   func(v *Compiled) { v.GCTypeDescs[0].Final = !v.GCTypeDescs[0].Final },
		"feature":      func(v *Compiled) { v.requiredFeatures &^= CoreFeatureMultiValue },
		"escape":       func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"function-abi": func(v *Compiled) { v.nativeReferenceFunctionABI = false },
		"product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = stagedGCTypeSubtypingRefTestSingle
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingRefTestSingle(candidate) {
				t.Fatal("hostile retained mutation kept exact witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained mutation admitted", err)
			}
		})
	}
	pairs := rv64PublicGCTypeSubtypingRefTestMultiPairsCompile(t, false)
	for _, pair := range []struct {
		name                     string
		metadataOwner, codeOwner *Compiled
	}{
		{"multi-metadata-pairs-image", base, pairs.executionView()},
		{"pairs-metadata-multi-image", pairs.executionView(), base},
	} {
		t.Run(pair.name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(pair.metadataOwner)
			candidate.code = slices.Clone(pair.codeOwner.code)
			candidate.validateMemo = &validateMemo{}
			if rv64GCTypeSubtypingRefTestSingle(candidate) || !errors.Is(railshotValidateCompiled(candidate), railshot.ErrUnsupported) {
				t.Fatal("cross-paired full multi-result artifact admitted")
			}
		})
	}
	pairs.Close()
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestMultiRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestMultiCompile(t, false)
	rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("full multi-result ref.test instantiated without private runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestMultiCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestMultiBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free full multi-result ref.test path used source compilation", err)
	}
}
