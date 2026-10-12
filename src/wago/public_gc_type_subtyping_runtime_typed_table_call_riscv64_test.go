//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

const rv64PublicGCTypeSubtypingRuntimeTypedTableCallHex = "0061736d01000000019580808000045000600000500100600000500101600000600000038680808000050102030303048680808000016301010202079780808000030372756e0002056661696c310003056661696c320004098f8080800001060041000b630102d2000bd2010b0ac780808000058280808000000b8280808000000b9b8080800000410011000041011100004100110100410111010041011102000b87808080000041001102000b87808080000041001103000b"

func rv64PublicGCTypeSubtypingRuntimeTypedTableCallBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRuntimeTypedTableCallHex)
	want := [sha256.Size]byte{0x2a, 0xd9, 0x54, 0x57, 0x82, 0x1c, 0xeb, 0x82, 0x11, 0xd5, 0x73, 0x3f, 0xe3, 0x08, 0xf0, 0x31, 0xf1, 0x10, 0x37, 0x55, 0x73, 0x3b, 0xbf, 0x8b, 0x5d, 0xb9, 0xc8, 0x5d, 0xb0, 0xeb, 0x6d, 0x9b}
	if err != nil || len(b) != 186 || sha256.Sum256(b) != want {
		t.Fatal("runtime typed-table call source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-runtime-typed-table-call.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeTypedTableCallBytes(t))
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

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRuntimeTypedTableCallMetadataLen || sha256.Sum256(metadata) != rv64GCTypeSubtypingRuntimeTypedTableCallMetadataDigest ||
				!rv64GCTypeSubtypingRuntimeTypedTableCall(view) || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRuntimeTypedTableCall || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact runtime typed-table witness: metadata=%d/%x err=%v exact=%t product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, rv64GCTypeSubtypingRuntimeTypedTableCall(view), view.stagedGCTypeSubtypingProduct(), validation, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("runtime typed-table unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
			base, memoryBytes, codeBytes, sealed, writable, refs := cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs
			if in.gc != nil || base == 0 || memoryBytes < codeBytes || !sealed || writable != artifact || refs == 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("runtime typed-table publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, base, memoryBytes, codeBytes, sealed, writable, refs)
			}
			if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want empty success", got, err)
			}
			for i := 1; i <= 2; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapIndirectWrongSig)
			}
			if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
				t.Fatalf("run after traps = %v, %v; want empty success", got, err)
			}
			if err = compiled.Close(); err != nil {
				in.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live runtime typed-table instance lost its executable lease")
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("runtime typed-table executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t, false)
	defer compiled.Close()
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[2] += 16; v.InternalEntry[2] += 16 },
		"function":   func(v *Compiled) { v.Funcs[1].TypeIndex = 1 },
		"type":       func(v *Compiled) { v.Types[1].Final = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].HasSuper = false },
		"table-type": func(v *Compiled) { v.ValueTypes[v.TableValueTypeIndex].Ref.Nullable = false },
		"element":    func(v *Compiled) { v.Elems[0].Values[1].FuncIndex = 0 },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"escape":     func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"function-abi": func(v *Compiled) {
			v.nativeReferenceFunctionABI = false
		},
		"product": func(v *Compiled) { v.codeCache.gcTypeSubtypingProduct = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingRuntimeTypedTableCall(candidate) {
				t.Fatal("hostile retained mutation kept runtime typed-table witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained runtime typed-table mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeTypedTableCallCompile(t, false)
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
	defer rt.Close()
	mod, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
	if in != nil {
		_ = in.Close()
	}
	if !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("runtime typed-table call instantiated without private Runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeTypedTableCallCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeTypedTableCallBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free runtime typed-table path used source compilation", err)
	}
}
