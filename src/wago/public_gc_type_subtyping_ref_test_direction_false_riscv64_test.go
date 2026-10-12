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

const rv64PublicGCTypeSubtypingRefTestDirectionFalseHex = "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"

func rv64PublicGCTypeSubtypingRefTestDirectionFalseBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRefTestDirectionFalseHex)
	want := [sha256.Size]byte{0x9b, 0x81, 0x11, 0xee, 0x2e, 0x3f, 0xb9, 0x1c, 0xc7, 0x80, 0x1a, 0x63, 0xb0, 0xa5, 0xa8, 0xe9, 0x7e, 0xca, 0x7b, 0x56, 0x65, 0xf7, 0xe6, 0xfe, 0xd5, 0xbe, 0x8a, 0x83, 0x27, 0x53, 0x42, 0x13}
	if err != nil || len(b) != 122 || sha256.Sum256(b) != want {
		t.Fatal("direction-false ref.test source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-ref-test-direction-false.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestDirectionFalseBytes(t))
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

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalseExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataLens[2] || sha256.Sum256(metadata) != rv64GCTypeSubtypingRefTestSingleMetadataDigests[2] ||
				rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(view, false, true) != 3 || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRefTestSingle || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact direction-false ref.test witness: metadata=%d/%x err=%v variant=%d product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, rv64GCTypeSubtypingRefTestSingleProfileVariantWithCode(view, false, true), view.stagedGCTypeSubtypingProduct(), validation, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("direction-false ref.test unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatal("direction-false ref.test runtime state is incomplete")
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want [0]", got, err)
			}
			if err = compiled.Close(); err != nil || cache.base == 0 || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live direction-false ref.test instance lost its code lease", err)
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatal("direction-false ref.test executable mapping survived final close")
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalseAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, false)
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
		"false-metadata-single-image":    {base, single.executionView()},
		"false-metadata-recursive-image": {base, recursive.executionView()},
		"single-metadata-false-image":    {single.executionView(), base},
		"recursive-metadata-false-image": {recursive.executionView(), base},
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
	recursive.Close()
	single.Close()
	compiled.Close()
}

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalseRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRefTestDirectionFalseCompile(t, false)
	rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		mod.Close()
	}
	compiled.Close()
	rt.Close()
	if !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("direction-false ref.test instantiated without private runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRefTestDirectionFalseCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRefTestDirectionFalseBytes(t))
	if c != nil {
		c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free direction-false ref.test path used source compilation", err)
	}
}
