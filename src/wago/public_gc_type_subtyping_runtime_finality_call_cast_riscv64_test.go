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

const rv64PublicGCTypeSubtypingRuntimeFinalityCallCastHex = "0061736d0100000001898080800002500060000060000003878080800006000101010101048580808000017001020207a18080800004056661696c310002056661696c320003056661696c330004056661696c340005098c80808000010441000b02d2000bd2010b0acb80808000068280808000000b8280808000000b8a8080800000024041011100000b0b8a8080800000024041001101000b0b8a808080000041012500fb16001a0b8a808080000041002500fb16011a0b"

func rv64PublicGCTypeSubtypingRuntimeFinalityCallCastBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRuntimeFinalityCallCastHex)
	want := [sha256.Size]byte{0x37, 0x5a, 0x32, 0x7f, 0x84, 0x69, 0xd4, 0x1d, 0x4f, 0x15, 0xf0, 0x51, 0x09, 0x53, 0x3a, 0x90, 0x12, 0x7f, 0xc5, 0x28, 0x74, 0x14, 0x36, 0x4e, 0x22, 0x72, 0x03, 0xd7, 0xd4, 0x8e, 0x76, 0x62}
	if err != nil || len(b) != 185 || sha256.Sum256(b) != want {
		t.Fatal("runtime finality call/cast source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-runtime-finality-call-cast.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeFinalityCallCastBytes(t))
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

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataLen || sha256.Sum256(metadata) != rv64GCTypeSubtypingRuntimeFinalityCallCastMetadataDigest ||
				!rv64GCTypeSubtypingRuntimeFinalityCallCast(view) || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRuntimeFinalityCallCast || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact runtime finality witness: metadata=%d/%x err=%v exact=%t product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, rv64GCTypeSubtypingRuntimeFinalityCallCast(view), view.stagedGCTypeSubtypingProduct(), validation, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("runtime finality unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatalf("runtime finality publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, base, memoryBytes, codeBytes, sealed, writable, refs)
			}
			for i := 1; i <= 2; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapIndirectWrongSig)
			}
			for i := 3; i <= 4; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapCastFailure)
			}
			// Re-enter after both helper-mediated trap classes to prove that the
			// framed stack and invocation context were restored.
			rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail1", TrapIndirectWrongSig)
			rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail3", TrapCastFailure)
			if err = compiled.Close(); err != nil {
				in.Close()
				rt.Close()
				t.Fatal(err)
			}
			if cache.base == 0 || !cache.sealed || cache.refs == 0 {
				in.Close()
				rt.Close()
				t.Fatal("live runtime finality instance lost its executable lease")
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("runtime finality executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t, false)
	defer compiled.Close()
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[2] += 16; v.InternalEntry[2] += 16 },
		"function":   func(v *Compiled) { v.Funcs[1].TypeIndex = 0 },
		"type":       func(v *Compiled) { v.Types[0].Final = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].Final = false },
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
			if rv64GCTypeSubtypingRuntimeFinalityCallCast(candidate) {
				t.Fatal("hostile retained mutation kept runtime finality witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained runtime finality mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeFinalityCallCastCompile(t, false)
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
		t.Fatal("runtime finality call/cast instantiated without private Runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeFinalityCallCastCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeFinalityCallCastBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free runtime finality path used source compilation", err)
	}
}
