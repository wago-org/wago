//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
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

const rv64PublicGCTypeSubtypingRefTestSupertypeTrueHex = "0061736d0100000001a780808000044e0250006000005f016400004e0250006000005f016402004e025001006000005f006000017f038380808000020406078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"

func rv64PublicGCTypeSubtypingRefTestSupertypeTrueBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestSupertypeTrueHex)
	want := [sha256.Size]byte{0x60, 0xad, 0xfe, 0xb1, 0xca, 0xe8, 0xb6, 0x5d, 0x15, 0x9f, 0x8c, 0x07, 0x29, 0x63, 0x0c, 0x00, 0x5f, 0x5b, 0x53, 0x0e, 0x90, 0xd1, 0x90, 0x18, 0x94, 0x87, 0xee, 0x24, 0x1f, 0x30, 0xc5, 0x23}
	if err != nil || len(b) != 112 || sha256.Sum256(b) != want {
		t.Fatal("supertype-true ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestSupertypeTrueCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-supertype-true.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestSupertypeTrueBytes(t))
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

func TestRV64PublicGCTypeSubtypingRefTestSupertypeTrueExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestSupertypeTrueCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataLens[3] || sha256.Sum256(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataDigests[3] ||
				rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(view, false, true) != 4 || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRefTestSingle || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact supertype-true ref.test witness: metadata=%d/%x err=%v variant=%d product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(view, false, true), view.stagedGCTypeSubtypingProduct(), validation, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("supertype-true ref.test unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatal("supertype-true ref.test runtime state is incomplete")
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
				t.Fatal("live supertype-true ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("supertype-true ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestSupertypeTrueAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestSupertypeTrueCompile(t, false)
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
			v.nativeReferenceFunctionABI = false
		},
		"product": func(v *Compiled) {
			cache := *v.codeCache
			v.codeCache = &cache
			v.codeCache.gcTypeSubtypingProduct = 0
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
	single := rv64PublicGCTypeSubtypingRefTestSingleCompile(t, false)
	recursive := rv64PublicGCTypeSubtypingRefTestRecursiveCompile(t, false)
	for name, pair := range map[string][2]*Compiled{
		"super-metadata-single-image":    {base, single.executionView()},
		"super-metadata-recursive-image": {base, recursive.executionView()},
		"single-metadata-super-image":    {single.executionView(), base},
		"recursive-metadata-super-image": {recursive.executionView(), base},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(pair[0])
			candidate.code = slices.Clone(pair[1].code)
			candidate.validateMemo = &validateMemo{}
			if rv64GCTypeSubtypingRefTestSingle(candidate) {
				t.Fatal("cross-paired retained metadata and image kept exact witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("cross-paired retained metadata and image admitted", err)
			}
		})
	}
	directionFalse := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, false)
	if !bytes.Equal(base.code, directionFalse.executionView().code) || rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(base, false, true) != 4 || rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(directionFalse.executionView(), false, true) != 3 {
		t.Fatal("shared type-4 lowering did not retain distinct .22/.23 graphs")
	}
	directionFalse.Close()
	recursive.Close()
	single.Close()
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestSupertypeTrueRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestSupertypeTrueCompile(t, false)
	rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("supertype-true ref.test instantiated without private runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestSupertypeTrueCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestSupertypeTrueBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free supertype-true ref.test path used source compilation", err)
	}
}
