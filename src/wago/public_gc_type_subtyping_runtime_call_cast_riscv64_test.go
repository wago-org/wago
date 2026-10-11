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

const rv64PublicGCTypeSubtypingRuntimeCallCastHex = "0061736d01000000019a808080000450006000017050010060000163015001016000016302600000038b808080000a00010203030303030303048580808000017001030307b780808000070372756e0003056661696c310004056661696c320005056661696c330006056661696c340007056661696c350008056661696c360009098f80808000010441000b03d2000bd2010bd2020b0a80828080000a848080800000d0700b848080800000d0010b848080800000d0020bf98080800000027041001100000b027041011100000b027041021100000b02630141011101000b02630141021101000b02630241021102000b02630041002500fb16000b02630041012500fb16000b02630041022500fb16000b02630141012500fb16010b02630141022500fb16010b02630241022500fb16020b0c000b8d808080000002630141001101000b0c000b8d808080000002630141001102000b0c000b8d808080000002630141011102000b0c000b8b808080000041002500fb16010c000b8b808080000041002500fb16020c000b8b808080000041012500fb16020c000b"

func rv64PublicGCTypeSubtypingRuntimeCallCastBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRuntimeCallCastHex)
	want := [sha256.Size]byte{0x50, 0x5e, 0x94, 0xdb, 0xd6, 0x6f, 0xc2, 0xe3, 0xb5, 0xd2, 0xd4, 0xaf, 0x76, 0x34, 0x16, 0x18, 0xb1, 0x95, 0x71, 0x07, 0x4c, 0x7b, 0x42, 0xa5, 0x51, 0x39, 0x2f, 0xd5, 0x8a, 0xa6, 0x92, 0xf3}
	if err != nil || len(b) != 412 || sha256.Sum256(b) != want {
		t.Fatal("runtime call/cast source identity changed", err)
	}
	return b
}

func rv64PublicGCTypeSubtypingRuntimeCallCastCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-runtime-call-cast.wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeCallCastBytes(t))
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

func rv64AssertGCTypeSubtypingRuntimeTrap(t *testing.T, in *Instance, name string, code TrapCode) {
	t.Helper()
	got, err := in.Invoke(name)
	var trap *TrapError
	if got != nil || !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("%s = %v, %v; want trap %v", name, got, err, code)
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeCallCastExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCTypeSubtypingRuntimeCallCastCompile(t, artifact)
			view := compiled.executionView()
			metadata, metadataErr := marshalCompiledMetadata(view)
			validation := railshotValidateCompiled(view)
			if metadataErr != nil || len(metadata) != rv64GCTypeSubtypingRuntimeCallCastMetadataLen || sha256.Sum256(metadata) != rv64GCTypeSubtypingRuntimeCallCastMetadataDigest ||
				!rv64GCTypeSubtypingRuntimeCallCast(view) || view.stagedGCTypeSubtypingProduct() != stagedGCTypeSubtypingRuntimeCallCast || validation != nil {
				compiled.Close()
				t.Fatalf("missing exact runtime call/cast witness: metadata=%d/%x err=%v exact=%t product=%v validation=%v code=%d/%x entries=%v/%v", len(metadata), sha256.Sum256(metadata), metadataErr, rv64GCTypeSubtypingRuntimeCallCast(view), view.stagedGCTypeSubtypingProduct(), validation, len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			cache := compiled.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
				artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				compiled.Close()
				t.Fatalf("runtime call/cast unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				t.Fatalf("runtime call/cast publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, base, memoryBytes, codeBytes, sealed, writable, refs)
			}
			if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
				in.Close()
				compiled.Close()
				rt.Close()
				t.Fatalf("run = %v, %v; want empty success", got, err)
			}
			for i := 1; i <= 3; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapIndirectWrongSig)
			}
			for i := 4; i <= 6; i++ {
				rv64AssertGCTypeSubtypingRuntimeTrap(t, in, "fail"+string(rune('0'+i)), TrapCastFailure)
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
				t.Fatal("live runtime call/cast instance lost its executable lease")
			}
			if err = in.Close(); err != nil {
				rt.Close()
				t.Fatal(err)
			}
			rt.Close()
			if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
				t.Fatalf("runtime call/cast executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeCallCastAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeCallCastCompile(t, false)
	defer compiled.Close()
	base := compiled.executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[3] += 16; v.InternalEntry[3] += 16 },
		"function":   func(v *Compiled) { v.Funcs[1].TypeIndex = 0 },
		"type":       func(v *Compiled) { v.Types[0].Final = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[1].HasSuper = false },
		"element":    func(v *Compiled) { v.Elems[0].Values[2].FuncIndex = 1 },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
		"escape":     func(v *Compiled) { v.dynamicFuncrefEscape = false },
		"function-abi": func(v *Compiled) {
			v.nativeReferenceFunctionABI = false
		},
		"product": func(v *Compiled) { v.codeCache.gcTypeSubtypingProduct = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCTypeSubtypingRuntimeCallCast(candidate) {
				t.Fatal("hostile retained mutation kept runtime call/cast witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained runtime call/cast mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeCallCastRequiresPrivateRuntimePermission(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	compiled := rv64PublicGCTypeSubtypingRuntimeCallCastCompile(t, false)
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
		t.Fatal("runtime call/cast instantiated without private Runtime permission", err)
	}
}

func TestRV64PublicGCTypeSubtypingRuntimeCallCastCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRuntimeCallCastBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free runtime call/cast path used source compilation", err)
	}
}
