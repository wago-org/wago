//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type rv64Core3CallRefFixture struct {
	name, encoded, hash string
	types, funcs        int
	descriptorTarget    int
}

// These are byte-identical to the three valid unreachable-typing binaries
// emitted without debug names by WABT 1.0.41 from Release 3 core/call_ref.wast
// at revision 9d36019973201a19f9c9ebb0f10828b2fe2374aa.
var rv64Core3CallRefUnreachableFixtures = []rv64Core3CallRefFixture{
	{
		name: "call_ref.1", types: 2, funcs: 1, descriptorTarget: -1,
		encoded: "0061736d010000000108026000006000017f03020101070f010b756e726561636861626c6500000a070105000014000b",
		hash:    "37f5b78fcdc7ced4bf49f2fbbdfd38047fafb3b08f5942bcc93fefbb901a7045",
	},
	{
		name: "call_ref.2", types: 2, funcs: 2, descriptorTarget: 0,
		encoded: "0061736d01000000010a0260017f017f6000017f0303020001070f010b756e726561636861626c650001090501030001000a0e02040020000b070000d20014000b",
		hash:    "8947b13ba7d12d33a253507f19078dcf255cb3ee5ddb5596a04852e7f0b1387e",
	},
	{
		name: "call_ref.3", types: 2, funcs: 2, descriptorTarget: 0,
		encoded: "0061736d01000000010a0260017f017f6000017f0303020001070f010b756e726561636861626c650001090501030001000a1302040020000b0c00004100d20014001a41000b",
		hash:    "d315acc38824915cc6df9a53cf207c73486d9e48f438e0d4ddc03569f9524f2a",
	},
}

func rv64Core3CallRefSource(t *testing.T, fixture rv64Core3CallRefFixture) []byte {
	t.Helper()
	source, err := hex.DecodeString(fixture.encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != fixture.hash {
		t.Fatalf("%s source hash = %s, want %s", fixture.name, got, fixture.hash)
	}
	return source
}

func rv64Core3CallRefCompile(t *testing.T, fixture rv64Core3CallRefFixture, artifact bool) *Compiled {
	t.Helper()

	c, err := rv64NativeTailConfig().Compile(rv64Core3CallRefSource(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	sourceCompiled := c
	t.Cleanup(func() { _ = sourceCompiled.Close() })

	return c
}

func TestRV64Core3CallRefUnreachableExecutionAndArtifact(t *testing.T) {
	for _, fixture := range rv64Core3CallRefUnreachableFixtures {
		fixture := fixture
		for _, artifact := range []bool{false} {
			t.Run(fmt.Sprintf("%s-artifact%t", fixture.name, artifact), func(t *testing.T) {
				if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" && !artifact {
					t.Skip("compiler-free artifact case only")
				}
				c := rv64Core3CallRefCompile(t, fixture, artifact)
				wantFeatures := CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
				wantDescriptors := false
				if fixture.descriptorTarget >= 0 {
					wantFeatures |= CoreFeatureBulkMemoryOperations
					wantDescriptors = true
				}
				export, exported := c.Exports["unreachable"]
				if c.requiredFeatures != wantFeatures || compiledStructuralRequiredFeatures(c) != wantFeatures || !c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.nativeReferenceFunctionABI || c.needsFuncRefDescs() != wantDescriptors || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Types) != fixture.types || len(c.Funcs) != fixture.funcs || len(c.Exports) != 1 || !exported || export != fixture.funcs-1 || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
					t.Fatalf("%s source/artifact witnesses: features=%#x function=%t call=%t control=%t descriptors=%t passive=%d", fixture.name, c.requiredFeatures, c.nativeReferenceFunctionABI, c.nativeReferenceCallABI, c.nativeReferenceControlABI, c.needsFuncRefDescs(), len(c.passiveElems))
				}
				view, cache := c.executionView(), c.loadCodeCache()
				if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
					t.Fatalf("%s unpublished mapping: artifact=%t base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", fixture.name, artifact, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
				}

				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				if cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
					t.Fatalf("%s publication state: base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", fixture.name, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
				}
				if fixture.descriptorTarget >= 0 {
					if descriptor, ok := in.localFuncrefDescriptor(fixture.descriptorTarget); !ok || descriptor <= uint64(^uint32(0)) {
						t.Fatal("pinned full-width ref.func descriptor", descriptor, ok)
					}
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					got, err := in.Invoke("unreachable")
					var trap *coreruntime.TrapError
					if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) == 0 {
						t.Fatal("pinned unreachable trap and reuse", got, err)
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
				if in.hasPhysicalResources() || c.codeCache.mem != nil || c.codeCache.refs != 0 {
					t.Fatal("pinned call_ref cleanup")
				}
			})
		}
	}
}

func TestRV64Core3CallRefUnreachableFeatureGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	for _, fixture := range rv64Core3CallRefUnreachableFixtures {
		cfg := rv64NativeTailConfig().WithFeature(CoreFeatureTypedFunctionReferences, false)
		compiled, err := cfg.Compile(rv64Core3CallRefSource(t, fixture))
		if compiled != nil {
			_ = compiled.Close()
		}
		if err == nil {
			t.Fatal("pinned call_ref module compiled with typed references disabled", fixture.name)
		}
	}
}

func TestRV64Core3CallRefPinnedInvalidModules(t *testing.T) {
	tests := []struct {
		name, encoded, hash, detail string
		function                    int
	}{
		{"call_ref.4", "0061736d01000000010a0260017f017f6000017f0303020001070f010b756e726561636861626c650001090501030001000a1002040020000b0900004200d20014000b", "4e3495ee70397e811b33e7360f3a147829765b958bf055335f8a6daca0092a4c", "i64 is not i32", 1},
		{"call_ref.5", "0061736d01000000010a0260017f017f6000017f0303020001070f010b756e726561636861626c650001090501030001000a1102040020000b0a0000d20014001a42000b", "61cc0bad2f87bd1677e27aa92bf22e488567d117b2e4e8819986f7c46c4c3977", "i64 is not i32", 1},
		{"call_ref.6", "0061736d0100000001080260000060016f00030201010a08010600200014000b", "af6463eb32a43ba6cddefd5307407821054f8b48779e2b7b5f41891927b1fbae", "call_ref callee", 0},
		{"call_ref.7", "0061736d0100000001080260000060017000030201010a08010600200014000b", "01554fea538d9549c5dda7d7eae8d7eb178f266ea68d66913f0668ed3ffa22e0", "call_ref callee", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := hex.DecodeString(tc.encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != tc.hash {
				t.Fatalf("source hash = %s, want %s", got, tc.hash)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil || m == nil {
				t.Fatalf("assert_invalid must decode: module=%v err=%v", m, err)
			}
			assertRV64Core3CallRefTypeMismatch(t, wasm.ValidateModule(m), tc.function, tc.detail)
			if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" {
				return
			}
			for range 16 {
				compiled, err := rv64NativeTailConfig().WithMaxNativeCodeBytes(1).Compile(source)
				if compiled != nil {
					_ = compiled.Close()
					t.Fatal("invalid module produced a compiled value")
				}
				assertRV64Core3CallRefTypeMismatch(t, err, tc.function, tc.detail)
			}
		})
	}
}

func assertRV64Core3CallRefTypeMismatch(t *testing.T, err error, function int, detail string) {
	t.Helper()
	var validation *wasm.ValidationError
	if !errors.As(err, &validation) || validation.Code != wasm.ErrTypeMismatch || validation.Func != function || validation.Detail != detail {
		t.Fatalf("validation rejection = %#v (%v), want function %d type mismatch %q", validation, err, function, detail)
	}
}
