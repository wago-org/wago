//go:build !tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package wago

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func compileDynamicFuncrefFixture(t testing.TB, filename string) *Compiled {
	t.Helper()
	wasmBytes, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit).
		Compile(wasmBytes)
	if err != nil {
		t.Fatalf("compile %s: %v", filename, err)
	}
	return compiled
}

func instantiateDynamicFuncrefImportPair(t testing.TB, consumerFilename string) (*Instance, *Instance, *Compiled) {
	t.Helper()
	providerCompiled := compileDynamicFuncrefFixture(t, "dynamic_funcref_import_provider.wasm")
	t.Cleanup(func() { providerCompiled.Close() })
	consumerCompiled := compileDynamicFuncrefFixture(t, consumerFilename)
	t.Cleanup(func() { consumerCompiled.Close() })
	provider, err := Instantiate(providerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate dynamic funcref provider: %v", err)
	}
	t.Cleanup(func() { provider.Close() })
	export, err := provider.ExportedFunc("f")
	if err != nil {
		t.Fatalf("export dynamic funcref provider: %v", err)
	}
	consumer, err := Instantiate(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		t.Fatalf("instantiate %s: %v", consumerFilename, err)
	}
	t.Cleanup(func() { consumer.Close() })
	return provider, consumer, consumerCompiled
}

func TestDynamicIndexedFunctionRefCastOnlyModule(t *testing.T) {
	_, consumer, compiled := instantiateDynamicFuncrefImportPair(t, "dynamic_funcref_cast.wasm")
	if compiled.usesDynamicFuncRefTest() || !compiled.NeedsFuncRefDescs {
		t.Fatalf("cast-only runtime metadata = ref.test %v descriptors %v, want false/true", compiled.usesDynamicFuncRefTest(), compiled.NeedsFuncRefDescs)
	}
	for _, name := range []string{"cast_import", "cast_local_after_imports_and_limit", "call_import", "call_local_after_imports_and_limit"} {
		got, err := consumer.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("%s = %v, %v; want [1], nil", name, got, err)
		}
	}
	if got, err := consumer.Invoke("call_with_arguments"); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call_with_arguments = %v, %v; want [42], nil", got, err)
	}
	if got, err := consumer.Invoke("cast_null"); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("cast_null = %v, %v; want [1], nil", got, err)
	}
	for _, name := range []string{"cast_null_nonnullable", "cast_unrelated"} {
		if got, err := consumer.Invoke(name); err == nil || got != nil {
			t.Fatalf("%s = %v, %v; want cast trap", name, got, err)
		}
	}
}

func TestDynamicIndexedFunctionRefTestCrossInstanceImport(t *testing.T) {
	_, consumer, compiled := instantiateDynamicFuncrefImportPair(t, "dynamic_funcref_import_consumer.wasm")
	if !compiled.usesDynamicFuncRefTest() {
		t.Fatal("dynamic imported ref.test lost exact runtime metadata")
	}
	for _, name := range []string{"test_super", "test_sub"} {
		got, err := consumer.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("%s = %v, %v; want [1], nil", name, got, err)
		}
	}
}

func TestDynamicIndexedFunctionRefTestClosureDispatch(t *testing.T) {
	wasmBytes, err := os.ReadFile("testdata/dynamic_funcref_ref_test.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := NewRuntimeConfig().
		WithCoreFeatures(CoreFeaturesV3).
		WithBoundsChecks(BoundsChecksExplicit).
		Compile(wasmBytes)
	if err != nil {
		t.Fatalf("compile dynamic closure ref.test: %v", err)
	}
	defer compiled.Close()
	loaded := roundTripCompiled(t, compiled)
	defer loaded.Close()
	if coreruntime.FuncRefDescBytes != 40 || !compiled.usesDynamicFuncRefTest() || !loaded.usesDynamicFuncRefTest() {
		t.Fatalf("dynamic ref.test metadata = descriptor %d source=%v loaded=%v; want 40/true/true", coreruntime.FuncRefDescBytes, compiled.usesDynamicFuncRefTest(), loaded.usesDynamicFuncRefTest())
	}
	for _, candidate := range []*Compiled{compiled, loaded} {
		in, err := Instantiate(candidate, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate dynamic closure ref.test: %v", err)
		}
		typeIDPtr := binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.TableEntryCodePtrOffset:])
		if typeIDPtr == 0 || len(candidate.FuncTypeID) < 2 {
			in.Close()
			t.Fatalf("dynamic closure type-ID directory = %#x with %d functions", typeIDPtr, len(candidate.FuncTypeID))
		}
		typeIDs := unsafe.Slice((*byte)(offHeapPtr(uintptr(typeIDPtr))), 4*len(candidate.FuncTypeID))
		directType := binary.LittleEndian.Uint32(typeIDs[0:])
		environmentType := binary.LittleEndian.Uint32(typeIDs[4:])
		if directType != candidate.Funcs[0].TypeIndex || environmentType != candidate.Funcs[1].TypeIndex || directType == environmentType {
			in.Close()
			t.Fatalf("dynamic closure function type IDs = %d/%d, want exact %d/%d", directType, environmentType, candidate.Funcs[0].TypeIndex, candidate.Funcs[1].TypeIndex)
		}
		for _, tc := range []struct {
			name string
			args []uint64
			want uint64
		}{
			{name: "direct", args: []uint64{41}, want: 42},
			{name: "environment", args: []uint64{32}, want: 42},
			{name: "concrete_is_base", want: 1},
			{name: "child_is_root", want: 1},
			{name: "root_is_child", want: 0},
			{name: "unrelated_is_root", want: 0},
		} {
			got, callErr := in.Invoke(tc.name, tc.args...)
			if callErr != nil || len(got) != 1 || got[0] != tc.want {
				in.Close()
				t.Fatalf("%s closure dispatch = %v, %v; want [%d]", tc.name, got, callErr, tc.want)
			}
			prepared, prepareErr := in.PrepareFunction(tc.name)
			if prepareErr != nil {
				in.Close()
				t.Fatal(prepareErr)
			}
			allocs := testing.AllocsPerRun(1000, func() {
				got, callErr = prepared.Invoke(tc.args...)
			})
			if callErr != nil || len(got) != 1 || got[0] != tc.want || allocs != 0 {
				in.Close()
				t.Fatalf("steady %s closure dispatch = %v, %v, allocs=%v; want [%d], nil, 0", tc.name, got, callErr, allocs, tc.want)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}

		rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
		producer, err := instantiateCore(candidate, InstantiateOptions{store: rt.refStore})
		if err != nil {
			rt.Close()
			t.Fatalf("instantiate dynamic closure producer: %v", err)
		}
		consumer, err := instantiateCore(candidate, InstantiateOptions{store: rt.refStore})
		if err != nil {
			producer.Close()
			rt.Close()
			t.Fatalf("instantiate dynamic closure consumer: %v", err)
		}
		for _, tc := range []struct {
			get  string
			want uint64
		}{{get: "get_child", want: 1}, {get: "get_unrelated", want: 0}} {
			foreign, callErr := producer.Call(context.Background(), tc.get)
			if callErr != nil || len(foreign) != 1 || foreign[0].Type() != ValFuncRef {
				consumer.Close()
				producer.Close()
				rt.Close()
				t.Fatalf("producer %s = %v, %v; want one funcref", tc.get, foreign, callErr)
			}
			got, callErr := consumer.Call(context.Background(), "foreign_is_root", foreign[0])
			if callErr != nil || len(got) != 1 || got[0].Bits() != tc.want {
				consumer.Close()
				producer.Close()
				rt.Close()
				t.Fatalf("foreign_is_root(%s) = %v, %v; want %d", tc.get, got, callErr, tc.want)
			}
		}
		if err := consumer.Close(); err != nil {
			producer.Close()
			rt.Close()
			t.Fatal(err)
		}
		if err := producer.Close(); err != nil {
			rt.Close()
			t.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
