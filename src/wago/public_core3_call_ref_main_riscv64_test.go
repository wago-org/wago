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
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const rv64Core3CallRefMainHash = "040c0ac4738b3f542201f1674ebe040a18d9675f18abc098689f701b4a6a465e"

func rv64Core3CallRefMainSource(t *testing.T) []byte {
	t.Helper()
	source, err := hex.DecodeString("0061736d01000000011c0560017f017f60017e017e60027e7e017e600264007f017f6000017f030b0a03000000040102010101061f05640100d2050b640200d2060b640100d2070b640100d2080b640100d2090b0731070372756e0003046e756c6c0004036661630005076661632d6163630006036669620007046576656e0008036f64640009091705030002010203000105030001060300010703000208090ab9010a08002001200014000b0700200020006c0b0700410020006b0b1701026300d2012101d2022102200020011400200214000b08004101d00014000b1700200050047e4201052000200042017d230014017e0b0b1900200050047e200105200042017d200020017e230114020b0b20002000420158047e420105200042027d23021401200042017d230214017c0b0b1400200050047e422c05200042017d230414010b0b1500200050047e42e30005200042017d230314010b0b")
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 348 || fmt.Sprintf("%x", sha256.Sum256(source)) != rv64Core3CallRefMainHash {
		t.Fatal("pinned call_ref.0 source identity")
	}
	return source
}

func rv64Core3CallRefMainCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()

	c, err := rv64NativeTailConfig().Compile(rv64Core3CallRefMainSource(t))
	if err != nil {
		t.Fatal(err)
	}
	sourceCompiled := c
	t.Cleanup(func() { _ = sourceCompiled.Close() })

	return c
}

func rv64AssertCore3CallRefMainMetadata(t *testing.T, c *Compiled) {
	t.Helper()
	wantFeatures := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
	wantExports := map[string]int{"run": 3, "null": 4, "fac": 5, "fac-acc": 6, "fib": 7, "even": 8, "odd": 9}
	if c.requiredFeatures != wantFeatures || compiledStructuralRequiredFeatures(c) != wantFeatures || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || !c.needsFuncRefDescs() || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Types) != 5 || len(c.Funcs) != 10 || !mapsEqual(c.Exports, wantExports) || c.tableCount() != 0 || c.memoryCount() != 0 || len(c.Globals) != 5 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 2 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasStart {
		t.Fatalf("call_ref.0 source/artifact witnesses: features=%v function=%t call=%t control=%t descriptors=%t types=%d funcs=%d globals=%d value-types=%d exports=%v", c.requiredFeatures, c.nativeReferenceFunctionABI, c.nativeReferenceCallABI, c.nativeReferenceControlABI, c.needsFuncRefDescs(), len(c.Types), len(c.Funcs), len(c.Globals), len(c.ValueTypes), c.Exports)
	}
	wantTargets := [...]uint32{5, 6, 7, 8, 9}
	wantTypes := [...]uint32{1, 2, 1, 1, 1}
	for i, global := range c.Globals {
		exact, err := c.globalExactType(i)
		if err != nil || global.Type != ValFuncRef || global.Mutable || !global.HasValueType || !global.HasInitFunc || global.InitFunc != wantTargets[i] || global.HasInitGlobal || global.InitGlobal != 0 || global.Bits != 0 || global.V128 != (V128{}) || len(global.InitExpr) != 0 || exact.Kind != ValueTypeReference || exact.Ref.Nullable || exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.TypeIndex != wantTypes[i] {
			t.Fatalf("typed function global %d = %#v exact=%#v err=%v", i, global, exact, err)
		}
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func TestRV64Core3CallRefMainExecutionAndArtifact(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" && !artifact {
				t.Skip("compiler-free artifact case only")
			}
			c := rv64Core3CallRefMainCompile(t, artifact)
			rv64AssertCore3CallRefMainMetadata(t, c)
			view, cache := c.executionView(), c.loadCodeCache()
			if cache == nil || !artifact && (cache.base != 0 || len(cache.mem) != 0 || cache.sealed || cache.flags&compiledCacheWritableCode != 0) || artifact && (cache.base == 0 || len(cache.mem) < len(view.code) || cache.sealed || cache.flags&compiledCacheWritableCode == 0 || cache.refs != 0) {
				t.Fatalf("unpublished mapping: artifact=%t base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", artifact, cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}

			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			if cache.base == 0 || len(cache.mem) < len(view.code) || !cache.sealed || (cache.flags&compiledCacheWritableCode != 0) != artifact || cache.refs == 0 {
				t.Fatalf("published mapping: base=%#x mem=%d code=%d sealed=%t writable=%t refs=%d", cache.base, len(cache.mem), len(view.code), cache.sealed, cache.flags&compiledCacheWritableCode != 0, cache.refs)
			}
			for function := 5; function <= 9; function++ {
				if descriptor, ok := in.localFuncrefDescriptor(function); !ok || descriptor <= uint64(^uint32(0)) {
					t.Fatal("full-width recursive ref.func descriptor", function, descriptor, ok)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}

			vectors := []struct {
				name string
				args []uint64
				want []uint64
			}{
				{"run", []uint64{0}, []uint64{0}},
				{"run", []uint64{0xdeadbeef00000003}, []uint64{uint64(^uint32(8))}},
				{"fac", []uint64{0}, []uint64{1}}, {"fac", []uint64{1}, []uint64{1}}, {"fac", []uint64{5}, []uint64{120}}, {"fac", []uint64{25}, []uint64{7034535277573963776}},
				{"fac-acc", []uint64{0, 1}, []uint64{1}}, {"fac-acc", []uint64{5, 1}, []uint64{120}}, {"fac-acc", []uint64{25, 1}, []uint64{7034535277573963776}},
				{"fib", []uint64{0}, []uint64{1}}, {"fib", []uint64{1}, []uint64{1}}, {"fib", []uint64{2}, []uint64{2}}, {"fib", []uint64{5}, []uint64{8}}, {"fib", []uint64{20}, []uint64{10946}},
				{"even", []uint64{0}, []uint64{44}}, {"even", []uint64{1}, []uint64{99}}, {"even", []uint64{100}, []uint64{44}}, {"even", []uint64{77}, []uint64{99}},
				{"odd", []uint64{0}, []uint64{99}}, {"odd", []uint64{1}, []uint64{44}}, {"odd", []uint64{200}, []uint64{99}}, {"odd", []uint64{77}, []uint64{44}},
			}
			for range 2 {
				for _, tc := range vectors {
					got, err := in.Invoke(tc.name, tc.args...)
					if err != nil || !slices.Equal(got, tc.want) {
						t.Fatalf("%s%v: got=%v err=%v want=%v", tc.name, tc.args, got, err, tc.want)
					}
				}
				got, err := in.Invoke("null")
				var trap *coreruntime.TrapError
				if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapNullReference || len(trap.Frames) == 0 {
					t.Fatal("null call_ref trap and reuse", got, err)
				}
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if in.hasPhysicalResources() || c.codeCache.mem != nil || c.codeCache.refs != 0 {
				t.Fatal("call_ref.0 cleanup")
			}
		})
	}
}

func TestRV64Core3CallRefMainFeatureAndSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	for _, feature := range []CoreFeatures{CoreFeatureReferenceTypes, CoreFeatureTypedFunctionReferences} {
		cfg := rv64NativeTailConfig().WithFeature(feature, false)
		if c, err := cfg.Compile(rv64Core3CallRefMainSource(t)); c != nil || err == nil {
			if c != nil {
				_ = c.Close()
			}
			t.Fatal("call_ref.0 compiled with required feature disabled", feature, err)
		}
	}
	base := rv64Core3CallRefMainSource(t)
	offset := bytes.Index(base, []byte{0x64, 1, 0, 0xd2, 5, 0x0b})
	if offset < 0 {
		t.Fatal("pinned first typed-global wire")
	}
	for name, mutate := range map[string]func([]byte){
		"mutable":         func(v []byte) { v[offset+2] = 1 },
		"nullable":        func(v []byte) { v[offset] = 0x63 },
		"target-mismatch": func(v []byte) { v[offset+4] = 6 },
	} {
		source := slices.Clone(base)
		mutate(source)
		t.Run(name, func(t *testing.T) { rv64RequireCore3SourceValidation(t, source) })
	}
}

func TestRV64Core3CallRefMainRetainedAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64Core3CallRefMainCompile(t, false)
	for name, mutate := range map[string]func(*Compiled){
		"mutable":             func(v *Compiled) { v.Globals[0].Mutable = true },
		"nullable":            func(v *Compiled) { v.ValueTypes[0].Ref.Nullable = true },
		"exact":               func(v *Compiled) { v.ValueTypes[0].Ref.Exact = true },
		"heap-mismatch":       func(v *Compiled) { v.ValueTypes[0].Ref.Heap.TypeIndex = 0 },
		"heap-out-of-range":   func(v *Compiled) { v.ValueTypes[0].Ref.Heap.TypeIndex = uint32(len(v.Types)) },
		"recursive-group":     func(v *Compiled) { v.Types[1].RecGroup = 0 },
		"target-mismatch":     func(v *Compiled) { v.Globals[0].InitFunc = 6 },
		"target-out-of-range": func(v *Compiled) { v.Globals[0].InitFunc = uint32(len(v.Funcs)) },
		"missing-target":      func(v *Compiled) { v.Globals[0].HasInitFunc = false; v.Globals[0].InitFunc = 0 },
		"global-copy":         func(v *Compiled) { v.Globals[0].HasInitGlobal = true; v.Globals[0].InitGlobal = 1 },
		"initializer-bits":    func(v *Compiled) { v.Globals[0].Bits = 1 },
		"pool-binding":        func(v *Compiled) { v.Globals[0].ValueTypeIndex = 1 },
		"global-export":       func(v *Compiled) { v.GlobalExports = map[string]int{"cell": 0} },
		"typed-feature":       func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := rv64ReferenceStartCopy(base.executionView())
			mutate(candidate)
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile private typed-global metadata admitted", err)
			}
		})
	}
}

func TestRV64Core3CallRefMainPublicRuntime(t *testing.T) {
	compiled := rv64Core3CallRefMainCompile(t, os.Getenv("WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE") == "load")

	cfg := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	mod, err := rt.Module(compiled)
	if err == nil {
		_, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports()))
		_ = mod.Close()
	}
	_ = rt.Close()
	if err != nil {
		t.Fatal("public Core 3 instantiation", err)
	}
	_ = compiled.Close()
}
