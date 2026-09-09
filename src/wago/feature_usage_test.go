package wago

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestValidatedAnalysisRequirementsCorpusParity(t *testing.T) {
	paths, err := filepath.Glob("../../bench/corpus/*.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			continue
		}
		var analysis wasm.ValidatedModuleAnalysis
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{
			CompactImports:       true,
			MultiMemory:          true,
			ExtendedConstGlobals: true,
			GCConstExpr:          true,
		}, 1, wasm.ValidationLimits{}, &analysis); err != nil {
			continue
		}
		want := analyzeModuleRequirements(m)
		got := analyzeModuleRequirementsWithValidation(m, &analysis)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s requirements differ:\nvalidation: %#v\nlegacy:     %#v", filepath.Base(path), got, want)
		}
		compared++
	}
	if compared < 50 {
		t.Fatalf("compared %d validated corpus modules, want at least 50", compared)
	}
}

func typedBottomElementModule(heap wasm.AbsHeapType, declarative bool) []byte {
	flags := byte(0x05) // passive, typed expression elements
	if declarative {
		flags = 0x07
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{flags, byte(heap), 0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x42, 0x00, 0x0b}))),
	)
}

func TestTypedBottomElementRequirementsAndAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		heap        wasm.AbsHeapType
		declarative bool
	}{
		{name: "passive-nofunc", heap: wasm.HeapNoFunc},
		{name: "declarative-nofunc", heap: wasm.HeapNoFunc, declarative: true},
		{name: "passive-noextern", heap: wasm.HeapNoExtern},
		{name: "declarative-noextern", heap: wasm.HeapNoExtern, declarative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := typedBottomElementModule(tc.heap, tc.declarative)
			decoded, err := wasm.DecodeModule(module)
			if err != nil {
				t.Fatal(err)
			}
			features := moduleRequiredFeatures(decoded)
			want := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureGC
			if !features.IsEnabled(want) {
				t.Fatalf("moduleRequiredFeatures = %s, want at least %s", features, want)
			}

			if compiled, err := compatibilityDefaultConfig().Compile(module); err == nil {
				compiled.Close()
				t.Fatal("compatibility feature set accepted a typed bottom element")
			} else if !strings.Contains(err.Error(), "unsupported reference type") {
				t.Fatalf("compatibility feature error = %v, want typed reference rejection", err)
			}
			if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
				t.Skip("Core 3 execution backend is unavailable")
			}

			compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(module)
			if err != nil {
				t.Fatalf("Core 3 compile: %v", err)
			}
			defer compiled.Close()
			if !compiled.requiredFeatures.IsEnabled(CoreFeatureGC) {
				t.Fatalf("compiled required features = %s, want GC", compiled.requiredFeatures)
			}
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run")
			if err != nil || len(result) != 1 || result[0] != 0 {
				t.Fatalf("run = %v, %v; want [0], nil", result, err)
			}

			if defaultCompiled, err := NewRuntimeConfig().Compile(module); err == nil {
				defaultCompiled.Close()
				t.Fatal("default GC-off feature set accepted a typed bottom element")
			} else if !strings.Contains(err.Error(), "unsupported reference type") {
				t.Fatalf("default feature error = %v, want GC reference rejection", err)
			}
		})
	}
}

func aggregateStorageTypeModule(types ...[]byte) []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)))
}

func TestAggregateStorageTypesRequireIndependentFeatures(t *testing.T) {
	requireCompleteCore3Backend(t)
	anyrefStruct := []byte{0x5f, 0x01, byte(wasm.HeapAny), 0x00}
	exnrefArray := []byte{0x5e, byte(wasm.HeapExn), 0x00}
	emptyStruct := []byte{0x5f, 0x00}
	indexedStruct := []byte{0x5f, 0x01, 0x63, 0x00, 0x00} // field (ref null 0)

	for _, tc := range []struct {
		name     string
		module   []byte
		enabled  CoreFeatures
		required CoreFeatures
		missing  string
	}{
		{
			name:     "struct-anyref-needs-reference-types",
			module:   aggregateStorageTypeModule(anyrefStruct),
			enabled:  CoreFeatureGC,
			required: CoreFeatureGC | CoreFeatureReferenceTypes,
			missing:  "reference-types disabled",
		},
		{
			name:     "array-exnref-needs-exception-handling",
			module:   aggregateStorageTypeModule(exnrefArray),
			enabled:  CoreFeatureGC | CoreFeatureReferenceTypes,
			required: CoreFeatureGC | CoreFeatureReferenceTypes | CoreFeatureExceptionHandling,
			missing:  "exception-handling disabled",
		},
		{
			name:     "indexed-field-needs-typed-function-references",
			module:   aggregateStorageTypeModule(emptyStruct, indexedStruct),
			enabled:  CoreFeatureGC | CoreFeatureReferenceTypes,
			required: CoreFeatureGC | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences,
			missing:  "typed-function-references disabled",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := wasm.DecodeModule(tc.module)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := moduleRequiredFeatures(decoded); !got.IsEnabled(tc.required) {
				t.Fatalf("module required features = %s, want at least %s", got, tc.required)
			}
			if compiled, err := NewRuntimeConfig().WithCoreFeatures(tc.enabled).Compile(tc.module); err == nil {
				compiled.Close()
				t.Fatalf("compile without %q unexpectedly succeeded", tc.missing)
			} else if !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("compile error = %v, want %q", err, tc.missing)
			}

			compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(tc.module)
			if err != nil {
				t.Fatalf("compile with all features: %v", err)
			}
			defer compiled.Close()
			if got := (&Module{c: compiled}).Metadata().RequiredFeatures; !got.IsEnabled(tc.required) {
				t.Fatalf("compiled metadata features = %s, want at least %s", got, tc.required)
			}
			loaded := publicArtifactRoundTrip(t, compiled)
			defer loaded.Close()
			if got := (&Module{c: loaded}).Metadata().RequiredFeatures; !got.IsEnabled(tc.required) {
				t.Fatalf("loaded metadata features = %s, want at least %s", got, tc.required)
			}
		})
	}
}

func tableReferenceModule(ref wasm.RefType, imported bool) []byte {
	tableType := []byte{wasm.MustEncodeValType(wasm.RefVal(ref)), 0x00, 0x00}
	if !imported {
		return wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(tableType)))
	}
	entry := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	entry = append(entry, byte(wasm.ExternTable))
	entry = append(entry, tableType...)
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entry)))
}

func TestBottomTableReferenceRequirementsAndAdmission(t *testing.T) {
	for _, heap := range []wasm.AbsHeapType{wasm.HeapNoFunc, wasm.HeapNoExtern} {
		for _, imported := range []bool{false, true} {
			name := heap.String() + "/local"
			if imported {
				name = heap.String() + "/import"
			}
			t.Run(name, func(t *testing.T) {
				module := tableReferenceModule(wasm.AbsRef(heap), imported)
				decoded, err := wasm.DecodeModule(module)
				if err != nil {
					t.Fatal(err)
				}
				want := CoreFeatureReferenceTypes | CoreFeatureGC
				if got := moduleRequiredFeatures(decoded); got != want {
					t.Fatalf("module required features = %s, want %s", got, want)
				}
				if compiled, err := compatibilityDefaultConfig().Compile(module); err == nil {
					compiled.Close()
					t.Fatal("compatibility feature set accepted a typed bottom table")
				} else if !strings.Contains(err.Error(), "unsupported reference type") {
					t.Fatalf("compatibility feature error = %v, want typed reference rejection", err)
				}
				if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
					return
				}
				gcOnly, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureGC).Compile(module)
				if err != nil {
					t.Fatalf("GC without typed function references compile: %v", err)
				}
				gcOnly.Close()
				compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
				if err != nil {
					t.Fatalf("Core 3 compile: %v", err)
				}
				defer compiled.Close()
				if !compiled.requiredFeatures.IsEnabled(want) {
					t.Fatalf("compiled required features = %s, want %s", compiled.requiredFeatures, want)
				}
				if got := (&Module{c: compiled}).Metadata().RequiredFeatures; !got.IsEnabled(want) {
					t.Fatalf("metadata required features = %s, want %s", got, want)
				}
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				var loaded Compiled
				if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				defer loaded.Close()
				if !loaded.requiredFeatures.IsEnabled(want) {
					t.Fatalf("loaded required features = %s, want %s", loaded.requiredFeatures, want)
				}
			})
		}
	}
}

func TestMVPFuncrefTableRemainsFeatureFree(t *testing.T) {
	for _, imported := range []bool{false, true} {
		name := "local"
		if imported {
			name = "import"
		}
		t.Run(name, func(t *testing.T) {
			module := tableReferenceModule(wasm.FuncRef.Ref(), imported)
			decoded, err := wasm.DecodeModule(module)
			if err != nil {
				t.Fatal(err)
			}
			if got := moduleRequiredFeatures(decoded); got != 0 {
				t.Fatalf("module required features = %s, want none", got)
			}
			compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
			if err != nil {
				t.Fatalf("Core 1 compile: %v", err)
			}
			defer compiled.Close()
			if compiled.requiredFeatures != 0 {
				t.Fatalf("compiled required features = %s, want none", compiled.requiredFeatures)
			}
			if got := (&Module{c: compiled}).Metadata().RequiredFeatures; got != 0 {
				t.Fatalf("metadata required features = %s, want none", got)
			}
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatalf("load trusted artifact: %v", err)
			}
			defer loaded.Close()
			if loaded.requiredFeatures != 0 {
				t.Fatalf("loaded required features = %s, want none", loaded.requiredFeatures)
			}
		})
	}
}

func mvpFuncrefElementModule() []byte {
	element := append([]byte{0x00, 0x41, 0x00, 0x0b}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{wasm.MustEncodeValType(wasm.FuncRef), 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec(element)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func nonNullFuncrefElementModule() []byte {
	element := []byte{0x05, 0x64, byte(wasm.HeapFunc)} // passive (ref func)
	element = append(element, wasmtest.Vec([]byte{0xd2, 0x00, 0x0b})...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(9, wasmtest.Vec(element)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func nullableFuncrefElementModule() []byte {
	element := []byte{0x05, byte(wasm.HeapFunc)} // passive funcref expression segment
	element = append(element, wasmtest.Vec([]byte{0xd0, byte(wasm.HeapFunc), 0x0b})...)
	return wasmtest.Module(wasmtest.Section(9, wasmtest.Vec(element)))
}

func TestFuncrefElementArtifactRequirements(t *testing.T) {
	t.Run("MVP function-index segment", func(t *testing.T) {
		module := mvpFuncrefElementModule()
		if got := moduleRequiredFeatures(mustDecodeModule(t, module)); got != 0 {
			t.Fatalf("module required features = %s, want none", got)
		}
		compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatalf("Core 1 compile: %v", err)
		}
		defer compiled.Close()
		if len(compiled.Elems) != 1 || compiled.Elems[0].HasValueType {
			t.Fatalf("compiled legacy element type = %#v, want implicit function-index metadata", compiled.Elems)
		}
		exact, err := compiled.elemExactType(compiled.Elems[0])
		if err != nil || exact.Kind != ValueTypeReference || exact.Ref.Nullable || exact.Ref.Heap.Abstract != AbstractHeapFunc {
			t.Fatalf("compiled legacy element exact type = %#v, %v; want non-null (ref func)", exact, err)
		}
		if got := (&Module{c: compiled}).Metadata().RequiredFeatures; got != 0 {
			t.Fatalf("metadata required features = %s, want none", got)
		}
		blob, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatalf("load trusted artifact: %v", err)
		}
		defer loaded.Close()
		if loaded.requiredFeatures != 0 {
			t.Fatalf("loaded required features = %s, want none", loaded.requiredFeatures)
		}
	})

	t.Run("legacy segment initializes non-null table", func(t *testing.T) {
		if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
			t.Skip("typed function reference execution backend is unavailable")
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x40, 0x00, 0x64, byte(wasm.HeapFunc), 0x00, 0x01, 0xd2, 0x00, 0x0b})),
			wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
		)
		compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		instance.Close()

		blob, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatalf("load trusted artifact: %v", err)
		}
		defer loaded.Close()
		loadedInstance, err := Instantiate(loaded, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate loaded artifact: %v", err)
		}
		loadedInstance.Close()
	})

	t.Run("nullable expression segment", func(t *testing.T) {
		module := nullableFuncrefElementModule()
		compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		defer compiled.Close()
		if len(compiled.passiveElems) != 1 || !compiled.passiveElems[0].HasValueType {
			t.Fatalf("compiled expression element type = %#v, want structural metadata", compiled.passiveElems)
		}
		compiled.requiredFeatures = 0 // structural inference must recover tampered/lost feature bits.
		want := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes
		if got := (&Module{c: compiled}).Metadata().RequiredFeatures; got != want {
			t.Fatalf("metadata required features = %s, want %s", got, want)
		}
		blob, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatalf("load trusted artifact: %v", err)
		}
		defer loaded.Close()
		if loaded.requiredFeatures != want {
			t.Fatalf("loaded required features = %s, want %s", loaded.requiredFeatures, want)
		}
	})

	t.Run("non-null expression segment", func(t *testing.T) {
		if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
			t.Skip("typed function reference execution backend is unavailable")
		}
		module := nonNullFuncrefElementModule()
		compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatalf("Core 3 compile: %v", err)
		}
		defer compiled.Close()
		compiled.requiredFeatures = 0 // structural inference must recover tampered/lost feature bits.
		want := CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences
		if got := (&Module{c: compiled}).Metadata().RequiredFeatures; got != want {
			t.Fatalf("metadata required features = %s, want %s", got, want)
		}
		blob, err := compiled.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatalf("load trusted artifact: %v", err)
		}
		defer loaded.Close()
		if loaded.requiredFeatures != want {
			t.Fatalf("loaded required features = %s, want %s", loaded.requiredFeatures, want)
		}
	})
}

func mustDecodeModule(t testing.TB, module []byte) *wasm.Module {
	t.Helper()
	decoded, err := wasm.DecodeModule(module)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestBottomReferenceDescriptorRequirements(t *testing.T) {
	descriptors := []ValueTypeDescriptor{
		{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapNoFunc}}},
		{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapNoExtern}}},
	}
	want := CoreFeatureReferenceTypes | CoreFeatureGC
	if got := requiredFeaturesForTypeDescriptors(descriptors); got != want {
		t.Fatalf("descriptor required features = %s, want %s", got, want)
	}
}

func typedGCNullElementInitializerModule(heap wasm.AbsHeapType, declarative bool) []byte {
	flags := byte(0x05) // passive, typed expression elements
	if declarative {
		flags = 0x07
	}
	element := []byte{flags, byte(heap)}
	element = append(element, wasmtest.Vec(
		[]byte{0xd0, byte(heap), 0x0b},
		[]byte{0xd0, byte(heap), 0x0b},
	)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, wasmtest.Vec(element)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x42, 0x00, 0x0b}))),
	)
}

func TestTypedGCNullElementInitializers(t *testing.T) {
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Core 3 execution backend is unavailable")
	}
	for _, heap := range []wasm.AbsHeapType{wasm.HeapNone, wasm.HeapI31} {
		for _, declarative := range []bool{false, true} {
			name := heap.String() + "/passive"
			if declarative {
				name = heap.String() + "/declarative"
			}
			t.Run(name, func(t *testing.T) {
				compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(typedGCNullElementInitializerModule(heap, declarative))
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				defer compiled.Close()
				if !declarative {
					if len(compiled.passiveElems) != 1 || len(compiled.passiveElems[0].Values) != 2 {
						t.Fatalf("passive element metadata = %#v, want two values", compiled.passiveElems)
					}
					for i, value := range compiled.passiveElems[0].Values {
						if !value.Null {
							t.Fatalf("passive element value %d = %#v, want null", i, value)
						}
					}
				}
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatalf("instantiate: %v", err)
				}
				defer instance.Close()
				result, err := instance.Invoke("run")
				if err != nil || len(result) != 1 || result[0] != 0 {
					t.Fatalf("run = %v, %v; want [0], nil", result, err)
				}
			})
		}
	}
}

func exactI31ArithmeticElementModule() []byte {
	i31ref := wasm.MustEncodeValType(wasm.I31Ref)
	element := []byte{0x06, 0x00, 0x41, 0x00, 0x0b, i31ref}
	element = append(element, wasmtest.Vec([]byte{0x41, 0x01, 0x41, 0x02, 0x6a, 0xfb, 0x1c, 0x0b})...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{i31ref, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, wasmtest.Vec(element)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x25, 0x00, 0xfb, 0x1e, 0x0b}))),
	)
}

func TestExactI31ArithmeticElementInitializer(t *testing.T) {
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Core 3 execution backend is unavailable")
	}
	module := exactI31ArithmeticElementModule()
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	if len(compiled.Elems) != 1 || len(compiled.Elems[0].Values) != 1 || len(compiled.Elems[0].Values[0].Expr) == 0 {
		t.Fatalf("compiled element initializer = %#v, want deferred arithmetic expression", compiled.Elems)
	}
	wantFeatures := CoreFeatureReferenceTypes | CoreFeatureGC | CoreFeatureExtendedConst
	if got := compiled.requiredFeatures; !got.IsEnabled(wantFeatures) {
		t.Fatalf("compiled required features = %s, want at least %s", got, wantFeatures)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run")
	if err != nil || len(result) != 1 || result[0] != 3 {
		t.Fatalf("run = %v, %v; want [3], nil", result, err)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatalf("load trusted artifact: %v", err)
	}
	defer loaded.Close()
	if len(loaded.Elems) != 1 || len(loaded.Elems[0].Values) != 1 || len(loaded.Elems[0].Values[0].Expr) == 0 {
		t.Fatalf("loaded element initializer = %#v, want deferred arithmetic expression", loaded.Elems)
	}
	if got := loaded.requiredFeatures; !got.IsEnabled(wantFeatures) {
		t.Fatalf("loaded required features = %s, want at least %s", got, wantFeatures)
	}
	loadedInstance, err := Instantiate(loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate loaded artifact: %v", err)
	}
	defer loadedInstance.Close()
	loadedResult, err := loadedInstance.Invoke("run")
	if err != nil || len(loadedResult) != 1 || loadedResult[0] != 3 {
		t.Fatalf("loaded run = %v, %v; want [3], nil", loadedResult, err)
	}
}

func refNullDropModule(heap wasm.AbsHeapType) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, byte(heap), 0x1a, 0x42, 0x00, 0x0b}))),
	)
}

func TestGCRefNullHeapImmediateRequirements(t *testing.T) {
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Core 3 execution backend is unavailable")
	}
	for _, heap := range []wasm.AbsHeapType{wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray} {
		t.Run(heap.String(), func(t *testing.T) {
			compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(refNullDropModule(heap))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			defer compiled.Close()
			if !compiled.requiredFeatures.IsEnabled(CoreFeatureGC) {
				t.Fatalf("compiled required features = %s, want GC", compiled.requiredFeatures)
			}
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run")
			if err != nil || len(result) != 1 || result[0] != 0 {
				t.Fatalf("run = %v, %v; want [0], nil", result, err)
			}
		})
	}
}

func TestDynamicFuncrefEscapeIncludesReferenceResults(t *testing.T) {
	if CoreFeaturesV3&^platformCoreFeatures() != 0 {
		t.Skip("this product does not compile the CoreFeaturesV3 typed-reference fixture")
	}
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef})
	callerType := wasmtest.FuncType(nil, nil)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType, callerType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x00,       // unreachable supplies the polymorphic function reference
			0x14, 0x00, // call_ref type 0
			0x1a, // drop the returned aggregate reference
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.dynamicFuncrefEscape {
		t.Fatal("dynamic reference result did not mark local funcrefs as escaping")
	}
}

func TestModuleRequiredFeaturesFindsSIMDInEveryExpressionForm(t *testing.T) {
	simdBytes := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	simdBytes = append(simdBytes, 0x0b)
	simdExpr := wasm.Expr{BodyBytes: simdBytes}

	cases := []struct {
		name string
		m    *wasm.Module
	}{
		{name: "global initializer", m: &wasm.Module{Globals: []wasm.Global{{Init: simdExpr}}}},
		{name: "table initializer", m: &wasm.Module{Tables: []wasm.Table{{Init: &simdExpr}}}},
		{name: "element offset", m: &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: simdExpr}}}}},
		{name: "element expression", m: &wasm.Module{Elements: []wasm.Elem{{Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{simdExpr}}}}}},
		{name: "data offset", m: &wasm.Module{Data: []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataActive, Offset: simdExpr}}}}},
		{name: "function body", m: &wasm.Module{Code: []wasm.Func{{BodyBytes: simdBytes}}}},
		{name: "programmatic function body", m: &wasm.Module{Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrV128Const}}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleRequiredFeatures(tc.m); !got.IsEnabled(CoreFeatureSIMD) {
				t.Fatalf("moduleRequiredFeatures = %s, want SIMD", got)
			}
		})
	}
}

func TestRequiredFeaturesBodyScannerIgnoresSIMDByteInImmediate(t *testing.T) {
	features := requiredFeaturesForBodyBytes([]byte{0x41, 0xfd, 0x00, 0x0b})
	if features.IsEnabled(CoreFeatureSIMD) {
		t.Fatalf("requiredFeaturesForBodyBytes = %s for scalar i32.const", features)
	}
}

func TestRequiredFeaturesFindsThreadsDeclarationsAndInstructions(t *testing.T) {
	max := uint64(1)
	shared := wasm.MemType{Shared: true, Limits: wasm.Limits{Min: 1, Max: max, HasMax: true}}
	for _, tc := range []struct {
		name string
		m    *wasm.Module
	}{
		{name: "imported shared memory", m: &wasm.Module{Imports: []wasm.Import{{Type: wasm.NewMemExternType(shared)}}}},
		{name: "local shared memory", m: &wasm.Module{Memories: []wasm.MemType{shared}}},
		{name: "atomic instruction", m: &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{0xfe, 0x1e, 0x02, 0x00, 0x0b}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleRequiredFeatures(tc.m); !got.IsEnabled(CoreFeatureThreads) {
				t.Fatalf("moduleRequiredFeatures = %s, want threads", got)
			}
		})
	}
}

func TestRequiredFeaturesBodyScannerFastImmediatePaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want CoreFeatures
	}{
		{name: "throw", body: []byte{0x08, 0x00, 0x0b}, want: CoreFeatureExceptionHandling},
		{name: "return_call", body: []byte{0x12, 0x00, 0x0b}, want: CoreFeatureTailCall},
		{name: "call_ref", body: []byte{0x14, 0x00, 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences},
		{name: "return_call_ref", body: []byte{0x15, 0x00, 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureTailCall},
		{name: "table_get", body: []byte{0x25, 0x00, 0x0b}, want: CoreFeatureReferenceTypes},
		{name: "ref_func", body: []byte{0xd2, 0x00, 0x0b}, want: CoreFeatureReferenceTypes},
		{name: "ref_null_nofunc", body: []byte{0xd0, byte(wasm.HeapNoFunc), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "ref_null_noextern", body: []byte{0xd0, byte(wasm.HeapNoExtern), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "ref_null_eq", body: []byte{0xd0, byte(wasm.HeapEq), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "ref_null_i31", body: []byte{0xd0, byte(wasm.HeapI31), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "ref_null_struct", body: []byte{0xd0, byte(wasm.HeapStruct), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "ref_null_array", body: []byte{0xd0, byte(wasm.HeapArray), 0x0b}, want: CoreFeatureReferenceTypes | CoreFeatureGC},
		{name: "sign_extension", body: []byte{0xc0, 0x0b}, want: CoreFeatureSignExtensionOps},
		{name: "throw_ref", body: []byte{0x0a, 0x0b}, want: CoreFeatureExceptionHandling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiredFeaturesForBodyBytes(tc.body); got != tc.want {
				t.Fatalf("features = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRequiredFeaturesBareReferenceImmediates(t *testing.T) {
	for _, tc := range []struct {
		heap wasm.AbsHeapType
		want CoreFeatures
	}{
		{wasm.HeapFunc, CoreFeatureReferenceTypes},
		{wasm.HeapExtern, CoreFeatureReferenceTypes},
		{wasm.HeapAny, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapEq, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapI31, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapStruct, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapArray, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapNone, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapNoFunc, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapNoExtern, CoreFeatureReferenceTypes | CoreFeatureGC},
		{wasm.HeapExn, CoreFeatureReferenceTypes | CoreFeatureExceptionHandling},
		{wasm.HeapNoExn, CoreFeatureReferenceTypes | CoreFeatureExceptionHandling},
	} {
		for _, shape := range []struct {
			name string
			body []byte
		}{
			{name: "block", body: []byte{0x02, byte(tc.heap), 0x00, 0x0b, 0x1a, 0x0b}},
			{name: "typed-select", body: []byte{0x00, 0x1c, 0x01, byte(tc.heap), 0x1a, 0x0b}},
		} {
			t.Run(tc.heap.String()+"/"+shape.name, func(t *testing.T) {
				if got := requiredFeaturesForBodyBytes(shape.body); got != tc.want {
					t.Fatalf("features = %s, want %s", got, tc.want)
				}
			})
		}
	}
}

func BenchmarkAnalyzeModuleRequirementsMixedManyFunctionsAndMemories(b *testing.B) {
	const count = 1000
	m := &wasm.Module{Imports: make([]wasm.Import, count), Code: make([]wasm.Func, count)}
	memory32 := wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{Min: 1}})
	memory64 := wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{Min: 1, Addr64: true}})
	for i := range m.Imports {
		if i&1 == 0 {
			m.Imports[i].Type = memory32
		} else {
			m.Imports[i].Type = memory64
		}
	}
	body := []byte{0x28, 0x40, 0xe7, 0x07, 0x00, 0x1a, 0x0b} // i32.load memory 999; drop; end
	for i := range m.Code {
		m.Code[i].BodyBytes = body
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		got := analyzeModuleRequirements(m)
		if !got.features.IsEnabled(CoreFeatureMultiMemory) || !got.features.IsEnabled(CoreFeatureMemory64) {
			b.Fatal(got.features)
		}
	}
}

func BenchmarkRequiredFeaturesScalarBody(b *testing.B) {
	// A representative scalar stream: two immediate-bearing constants followed by
	// two immediate-free ALU/stack instructions. The feature scanner must still
	// consume every immediate exactly rather than searching raw opcode bytes.
	body := bytes.Repeat([]byte{0x41, 1, 0x41, 2, 0x6a, 0x1a}, 16<<10)
	body = append(body, 0x0b)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := requiredFeaturesForBodyBytes(body); got != 0 {
			b.Fatalf("features = %s, want none", got)
		}
	}
}
