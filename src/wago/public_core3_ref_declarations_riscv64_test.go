//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// rv64Core3RefDeclarationsSource is byte-identical to the no-debug ref.0.wasm
// emitted by WABT 1.0.41 for pinned Release 3 revision
// 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
func rv64Core3RefDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 0x0c, 0x70, 0x6f, 0x64, 0x70, 0x64, 0x6f, 0x64, 0x00, 0x64, 0x00, 0x64, 0x00, 0x64, 0x00, 0x70, 0x6f, 0x63, 0x00, 0x63, 0x00, 0x00},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func rv64Core3RefDeclarationsCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	c, err := rv64NativeTailConfig().Compile(rv64Core3RefDeclarationsSource())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func TestRV64Core3RefPinnedDeclarations(t *testing.T) {
	source := rv64Core3RefDeclarationsSource()
	if len(source) != 47 || fmt.Sprintf("%x", sha256.Sum256(source)) != "c9615f60f19a2d7a416ae8e7d6383b0f478cf8a5f9864adf8b095d14576d00a9" {
		t.Fatal("pinned declaration source identity", len(source))
	}
	for _, artifact := range []bool{false} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if os.Getenv("WAGO_RV64_CORE3_REF_ARTIFACT_MODE") == "load" && !artifact {
				t.Skip("compiler-free artifact case only")
			}
			c := rv64Core3RefDeclarationsCompile(t, artifact)
			view := c.executionView()
			if !rv64SourceContractValid(view) || railshotValidateCompiled(view) != nil || !c.nativeReferenceFunctionABI || !c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences || c.NumImports != 0 || len(c.Funcs) != 1 || len(c.Types) != 2 || len(c.Exports) != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
				t.Fatalf("pinned ref declaration-only source/artifact witnesses: contract=%t validation=%v function=%t control=%t call=%t features=%#x imports=%d funcs=%d types=%d exports=%d tables=%d memories=%d globals=%d elems=%d passive=%d start=%t", rv64SourceContractValid(view), railshotValidateCompiled(view), c.nativeReferenceFunctionABI, c.nativeReferenceControlABI, c.nativeReferenceCallABI, c.requiredFeatures, c.NumImports, len(c.Funcs), len(c.Types), len(c.Exports), c.tableCount(), c.memoryCount(), len(c.Globals), len(c.Elems), len(c.passiveElems), c.HasStart)
			}
			if len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != 0x452bd5aba9b3044d {
				t.Fatal("function type identity")
			}
			cache := c.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				t.Fatalf("ref declarations unpublished mapping state: artifact=%t base=%#x mem=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			if in.gc != nil || cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
				t.Fatalf("ref declarations publication state: gc=%p base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", in.gc, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("missing"); got != nil || err == nil {
				t.Fatal("declaration-only module published a callable surface", got, err)
			}
			if err := in.Close(); err != nil {
				t.Fatal("declaration-only immediate cleanup", err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if in.hasPhysicalResources() || c.codeCache.mem != nil || c.codeCache.refs != 0 {
				t.Fatal("declaration-only resource cleanup")
			}
		})
	}
}

func TestRV64Core3RefPinnedDeclarationFeatureGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_CORE3_REF_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	source := rv64Core3RefDeclarationsSource()
	for _, feature := range []CoreFeatures{CoreFeatureReferenceTypes, CoreFeatureTypedFunctionReferences} {
		cfg := rv64NativeTailConfig().WithFeature(feature, false)
		if c, err := cfg.Compile(source); c != nil || err == nil {
			if c != nil {
				_ = c.Close()
			}
			t.Fatal("pinned declaration compiled with required feature disabled", feature, err)
		}
	}
}

func TestRV64Core3RefPinnedDeclarationPublicRuntime(t *testing.T) {
	compiled := rv64Core3RefDeclarationsCompile(t, os.Getenv("WAGO_RV64_CORE3_REF_ARTIFACT_MODE") == "load")
	cfg := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		_ = mod.Close()
	}
	_ = compiled.Close()
	_ = rt.Close()
	if err != nil {
		t.Fatal("public Core 3 instantiation", err)
	}
}

func TestRV64Core3RefPinnedDeclarationSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_CORE3_REF_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64Core3RefDeclarationsSource()
	debug := append(bytes.Clone(base), 0x00, 0x0b, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74)
	if len(debug) != 60 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "8a3099a9c72a7064926f2e606f0b3fc720895f2ac64aec79244674c14afcfab5" {
		t.Fatal("canonical debug-name fixture")
	}
	mutations := map[string][]byte{
		"debug-name":      debug,
		"extern-nullable": bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f}, []byte{0x63, 0x6f}, 1),
		"extern-heap":     bytes.Replace(bytes.Clone(base), []byte{0x64, 0x6f}, []byte{0x64, 0x6e}, 1),
		"indexed-heap":    bytes.Replace(bytes.Clone(base), []byte{0x64, 0x00}, []byte{0x64, 0x01}, 1),
		"binding":         bytes.Replace(bytes.Clone(base), []byte{0x03, 0x02, 0x01, 0x01}, []byte{0x03, 0x02, 0x01, 0x00}, 1),
		"body":            bytes.Replace(bytes.Clone(base), []byte{0x02, 0x00, 0x0b}, []byte{0x02, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			rv64RequireCore3SourceValidation(t, source)
		})
	}
}

func TestRV64Core3RefPinnedInvalidModules(t *testing.T) {
	tests := []struct{ name, encoded, hash string }{
		{"ref.1", "0061736d0100000001060160016401000021046e616d65041a010017747970652d66756e632d706172616d2d696e76616c6964", "dabd93adbf7b189063f67c848a6e699552fefbc708a968fa146f559bd88dc074"},
		{"ref.2", "0061736d0100000001060160000164010022046e616d65041b010018747970652d66756e632d726573756c742d696e76616c6964", "8e13e24245901f5f4370f960a8f7ed2e4900ceb539cf82df8d4415fb63261afa"},
		{"ref.3", "0061736d01000000060701630100d0010b0018046e616d65071101000e676c6f62616c2d696e76616c6964", "376c57d45bc383292fe3bb1499cd9083e90a076d2e4ac7a024e54ecc7bf4ddf8"},
		{"ref.4", "0061736d010000000405016301000a0017046e616d65051001000d7461626c652d696e76616c6964", "f4218731228edde49f75a52eb564b607c6b887b45458706662101e69bfe14022"},
		{"ref.5", "0061736d01000000090501056401000016046e616d65080f01000c656c656d2d696e76616c6964", "e7240164e4b83a50045096f4452f0fd57cde25acc001b015cbe8c79ec116e644"},
		{"ref.6", "0061736d010000000106016001640100030201000a040102000b001c046e616d65011501001266756e632d706172616d2d696e76616c6964", "b9cce1f09385ffde7c4f509f36fc56090d4cf72ed28ff6512825d40991638f77"},
		{"ref.7", "0061736d010000000106016000016401030201000a040102000b001d046e616d65011601001366756e632d726573756c742d696e76616c6964", "5268b4b7c57763eedd225b1f5940b24130d2fe708a6b0f7414e9dd0b42a481e1"},
		{"ref.8", "0061736d01000000010401600000030201000a070105010163010b001c046e616d65011501001266756e632d6c6f63616c2d696e76616c6964", "123811b4059900f1f5c750915cd19cb00f8f98b313f3561542d27a1eb325a135"},
		{"ref.9", "0061736d01000000010401600000030201000a0a010800026401000b1a0b001e046e616d650117010014626c6f636b2d726573756c742d696e76616c6964", "d3e68fec9bbedb2b869d15f7f61cfd17fc746d27438f2fb999ee4790236417a4"},
		{"ref.10", "0061736d01000000010401600000030201000a0a010800036401000b1a0b001d046e616d6501160100136c6f6f702d726573756c742d696e76616c6964", "1a73d9ecea204c682a3e516229eba3d3ba70d4f678607265f5da2ae016b2631a"},
		{"ref.11", "0061736d01000000010401600000030201000a0a010800046401050b1a0b0014046e616d65010d01000a69662d696e76616c6964", "fb3af82c9ccf2a14f068b3f97c0341076adf82cbf943f2b7b65ad81584b7ee88"},
		{"ref.12", "0061736d01000000010401600000030201000a0a010800001c0164011a0b001f046e616d65011801001573656c6563742d726573756c742d696e76616c6964", "d7f4014c5fe6035143a00ec5f1c154b010be56bde3103061f5907e28ac598c36"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := hex.DecodeString(tc.encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != tc.hash {
				t.Fatalf("pinned source hash = %s, want %s", got, tc.hash)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || m == nil {
				t.Fatalf("pinned assert_invalid must decode: module=%v err=%v", m, err)
			}
			var validation *wasm.ValidationError
			if err := wasm.ValidateModule(m); !errors.As(err, &validation) || validation.Code != wasm.ErrUnknownType {
				t.Fatalf("validation rejection = %#v (%v), want unknown type", validation, err)
			}
			if os.Getenv("WAGO_RV64_CORE3_REF_ARTIFACT_MODE") == "load" {
				return
			}
			compiled, err := rv64NativeTailConfig().WithMaxNativeCodeBytes(1).Compile(source)
			if compiled != nil {
				_ = compiled.Close()
				t.Fatal("invalid module produced a compiled value")
			}
			if err == nil || !strings.Contains(err.Error(), "type") {
				t.Fatalf("public compile rejection = %v, want pre-publication type rejection", err)
			}
		})
	}
}

func rv64RequireCore3SourceValidation(t *testing.T, source []byte) {
	t.Helper()
	module, invalid := wasm.DecodeModule(source)
	if invalid == nil {
		invalid = wasm.ValidateModuleWithFeatures(module, wasm.ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true})
	}
	compiled, err := rv64NativeTailConfig().Compile(source)
	if invalid != nil {
		if compiled != nil {
			compiled.Close()
		}
		if err == nil {
			t.Fatal("invalid source compiled", invalid)
		}
		return
	}
	if err != nil || compiled == nil {
		t.Fatal("valid source rejected", err)
	}
	defer compiled.Close()
	if !rv64SourceContractValid(compiled.executionView()) || compiled.executionView().validate() != nil {
		t.Fatal("valid source lost its execution contract")
	}
}
