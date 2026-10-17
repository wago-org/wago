//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var rv64PublicGCTypeSubtypingRecursiveFunctionsHex = []string{
	"0061736d0100000001ac80808000044e03500060027f64020050010060027f64010050010160027f640000600164000060016401006001640200038480808000030304050aae8080800003868080800000200010000b8a808080000020001000200010010b8e80808000002000100020001001200010020b",
	"0061736d0100000001c880808000054e0250006000027f640150006000027d64004e045001006000027f64055001016000027d64045001006000027f64035001016000027d6402600164000060016402006001640400038480808000030607080aaa8080800003868080800000200010000b8a808080000020001000200010010b8a808080000020001000200010020b",
}

var rv64PublicGCTypeSubtypingRecursiveFunctionsSourceDigests = [][sha256.Size]byte{
	{0x6c, 0x51, 0x62, 0x87, 0x09, 0x07, 0xb8, 0x8c, 0x44, 0x4e, 0x61, 0x52, 0x8f, 0xe9, 0x07, 0xf2, 0x80, 0xfb, 0x2b, 0x38, 0xb8, 0x87, 0x7b, 0xbe, 0x98, 0xed, 0x58, 0xbf, 0xeb, 0xdd, 0xd4, 0x96},
	{0x74, 0x21, 0xec, 0x51, 0xf0, 0xe5, 0x74, 0xac, 0x12, 0x48, 0xb3, 0x2b, 0xc3, 0x7a, 0x7c, 0xc0, 0xa9, 0x34, 0x45, 0xcc, 0xf5, 0x88, 0x79, 0xe7, 0x57, 0xde, 0xf2, 0xaf, 0x49, 0x03, 0x9e, 0x3a},
}

func rv64PublicGCTypeSubtypingRecursiveFunctionsBytes(t testing.TB, variant uint8) []byte {
	t.Helper()
	if variant == 0 || int(variant) > len(rv64PublicGCTypeSubtypingRecursiveFunctionsHex) {
		t.Fatal("invalid recursive-function variant", variant)
	}
	b, err := hex.DecodeString(rv64PublicGCTypeSubtypingRecursiveFunctionsHex[variant-1])
	if err != nil {
		t.Fatal(err)
	}
	wantLength := []int{120, 144}[variant-1]
	if len(b) != wantLength || sha256.Sum256(b) != rv64PublicGCTypeSubtypingRecursiveFunctionsSourceDigests[variant-1] {
		t.Fatal("recursive-function source identity changed", variant)
	}
	return b
}

func rv64PublicGCTypeSubtypingRecursiveFunctionsCompile(t *testing.T, variant uint8, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-type-subtyping-recursive-functions-"+strconv.Itoa(int(variant))+".wago")
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
	original, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRecursiveFunctionsBytes(t, variant))
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

func TestRV64PublicGCTypeSubtypingRecursiveFunctionsExecutionAndArtifact(t *testing.T) {
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingRecursiveFunctionsHex); variant++ {
		for _, artifact := range []bool{false, true} {
			t.Run(strconv.Itoa(int(variant))+map[bool]string{false: "/source", true: "/artifact"}[artifact], func(t *testing.T) {
				compiled := rv64PublicGCTypeSubtypingRecursiveFunctionsCompile(t, variant, artifact)
				t.Cleanup(func() { _ = compiled.Close() })
				view := compiled.executionView()
				if !rv64ArtifactContractValid(view) || view.validate() != nil {
					t.Fatal("invalid public recursive-function execution contract")
				}
				cache := compiled.loadCodeCache()
				if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) ||
					artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
					compiled.Close()
					t.Fatalf("recursive-function unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
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
				gc, base, memoryBytes, codeBytes, sealed, writable, refs := in.gc, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs
				if gc != nil || base == 0 || memoryBytes < codeBytes || !sealed || writable != artifact || refs == 0 {
					in.Close()
					compiled.Close()
					rt.Close()
					t.Fatalf("recursive-function publication/collector state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", gc, base, memoryBytes, codeBytes, sealed, writable, refs)
				}
				if err = compiled.Close(); err != nil {
					in.Close()
					rt.Close()
					t.Fatal(err)
				}
				if cache.base == 0 || !cache.sealed || cache.refs == 0 {
					in.Close()
					rt.Close()
					t.Fatal("live instance lost its executable lease")
				}
				if err = in.Close(); err != nil {
					rt.Close()
					t.Fatal(err)
				}
				rt.Close()
				if cache.base != 0 || len(cache.mem) != 0 || cache.refs != 0 || !cache.closed {
					t.Fatalf("recursive-function executable mapping survived final close: base=%#x mem=%d refs=%d closed=%t", cache.base, len(cache.mem), cache.refs, cache.closed)
				}
			})
		}
	}
}

func TestRV64PublicGCTypeSubtypingRecursiveFunctionsAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingRecursiveFunctionsHex); variant++ {
		compiled := rv64PublicGCTypeSubtypingRecursiveFunctionsCompile(t, variant, false)
		base := compiled.executionView()
		for name, mutate := range map[string]func(*Compiled){
			"code":       func(v *Compiled) { v.code[len(v.code)-1] ^= 1 },
			"entry":      func(v *Compiled) { v.Entry[2] += 16; v.InternalEntry[2] += 16 },
			"function":   func(v *Compiled) { v.Funcs[1].TypeIndex = 0 },
			"type":       func(v *Compiled) { v.Types[0].Final = true },
			"type-key":   func(v *Compiled) { v.FuncTypeID[1] ^= 1 },
			"descriptor": func(v *Compiled) { v.GCTypeDescs[0].Final = true },
			"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
			"function-abi": func(v *Compiled) {
				v.nativeReferenceFunctionABI = !v.nativeReferenceFunctionABI
			},
			"invalid-execution-contract": func(v *Compiled) { v.setRV64ExecutionContract(&rv64ExecutionContract{}) },
		} {
			t.Run(strconv.Itoa(int(variant))+"/"+name, func(t *testing.T) {
				candidate := rv64ReferenceStartCopy(base)
				mutate(candidate)
				if err := candidate.validate(); err == nil {
					t.Fatal("hostile retained recursive-function mutation admitted")
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
}

func TestRV64PublicGCTypeSubtypingRecursiveFunctionsCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	for variant := uint8(1); int(variant) <= len(rv64PublicGCTypeSubtypingRecursiveFunctionsHex); variant++ {
		c, err := rv64I31Config().Compile(rv64PublicGCTypeSubtypingRecursiveFunctionsBytes(t, variant))
		if c != nil {
			_ = c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
			t.Fatal("compiler-free recursive-function path used source compilation", variant, err)
		}
	}
}
