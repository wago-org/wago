//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedFirstNullReferenceModule(mutableAnyGlobal bool) []byte {
	anyref := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	exnref := wasm.RefVal(wasm.AbsRef(wasm.HeapExn))
	indexed := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	results := []wasm.ValType{anyref, wasm.FuncRef, exnref, wasm.ExternRef, indexed}
	encodings := [][]byte{{byte(wasm.HeapAny)}, {byte(wasm.HeapFunc)}, {byte(wasm.HeapExn)}, {byte(wasm.HeapExtern)}, {0x63, 0x00}}
	heaps := []byte{byte(wasm.HeapAny), byte(wasm.HeapFunc), byte(wasm.HeapExn), byte(wasm.HeapExtern), 0}
	names := []string{"anyref", "funcref", "exnref", "externref", "ref"}

	types := [][]byte{wasmtest.FuncType(nil, nil)}
	funcs := make([][]byte, len(results))
	globals := make([][]byte, len(results))
	exports := make([][]byte, len(results))
	codes := make([][]byte, len(results))
	for i := range results {
		funcType := []byte{0x60, 0x00, 0x01}
		funcType = append(funcType, encodings[i]...)
		types = append(types, funcType)
		funcs[i] = wasmtest.ULEB(uint32(i + 1))
		mutable := byte(0)
		if i == 0 && mutableAnyGlobal {
			mutable = 1
		}
		global := append([]byte(nil), encodings[i]...)
		global = append(global, mutable, 0xd0, heaps[i], 0x0b)
		globals[i] = global
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
		codes[i] = wasmtest.Code([]byte{0xd0, heaps[i], 0x0b})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func compileStagedNullReferenceProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedFirstNullReferenceProductExecution(t *testing.T) {
	data := stagedFirstNullReferenceModule(false)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("public compile = %v, want closed any-reference gate", err)
	}
	c, err := compileStagedNullReferenceProductForTest(data)
	if err != nil {
		t.Fatalf("compile staged null-reference product: %v", err)
	}
	defer c.Close()

	wantFeatures := CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureGC | CoreFeatureExceptionHandling
	if got := compiledStructuralRequiredFeatures(c); got&wantFeatures != wantFeatures {
		t.Fatalf("required features = %v, want at least %v", got, wantFeatures)
	}
	if gc.HasHeapObjectTypes(c.GCTypeDescs) {
		t.Fatalf("null-only function type descriptors unexpectedly require a collector: %#v", c.GCTypeDescs)
	}
	meta := (&Module{c: c}).Metadata()
	wantResults := []ValType{ValAnyRef, ValFuncRef, ValExnRef, ValExternRef, ValFuncRef}
	if len(meta.Functions) != len(wantResults) || len(meta.Globals) != len(wantResults) {
		t.Fatalf("metadata functions/globals = %d/%d, want %d/%d", len(meta.Functions), len(meta.Globals), len(wantResults), len(wantResults))
	}
	for i, want := range wantResults {
		if !reflect.DeepEqual(meta.Functions[i].Results, []ValType{want}) || meta.Globals[i].Type != want || !meta.Globals[i].HasValueType {
			t.Fatalf("metadata %d = function %#v global %#v, want category %s", i, meta.Functions[i], meta.Globals[i], want)
		}
	}
	if got := meta.Functions[0].ResultTypes[0].Ref.Heap.Abstract; got != AbstractHeapAny {
		t.Fatalf("anyref exact result heap = %v, want any", got)
	}
	if got := meta.Functions[2].ResultTypes[0].Ref.Heap.Abstract; got != AbstractHeapExn {
		t.Fatalf("exnref exact result heap = %v, want exn", got)
	}
	if got := meta.Functions[4].ResultTypes[0].Ref.Heap; !got.Defined || got.TypeIndex != 0 {
		t.Fatalf("indexed exact result heap = %#v, want type 0", got)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatalf("marshal staged null-reference product: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("private reload staged null-reference product: %v", err)
	}
	defer loaded.Close()
	if got := (&Module{c: &loaded}).Metadata(); !reflect.DeepEqual(got.Functions, meta.Functions) || !reflect.DeepEqual(got.Globals, meta.Globals) {
		t.Fatalf("codec metadata changed: functions=%#v globals=%#v", got.Functions, got.Globals)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public codec load = %v, want unsupported GC/EH/typed feature gate", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
		t.Fatalf("snapshot capture = %v, want explicit null-reference/GC gate", err)
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged null-reference product: %v", err)
	}
	defer in.Close()
	if in.gc != nil {
		t.Fatal("null-only product allocated a WasmGC collector")
	}
	for i, name := range []string{"anyref", "funcref", "exnref", "externref", "ref"} {
		got, err := in.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("Invoke(%q) = %v, %v, want one zero slot", name, got, err)
		}
		typed, err := in.Call(context.Background(), name)
		if err != nil || len(typed) != 1 || typed[0].Type() != wantResults[i] || typed[0].Bits() != 0 {
			t.Fatalf("Call(%q) = %v, %v, want null %s", name, typed, err, wantResults[i])
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("anyref")
		if err != nil || len(got) != 1 || got[0] != 0 {
			panic("null-only anyref invocation failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("null-only anyref invocation allocations = %v, want 0", allocs)
	}
}

func TestStagedFirstNullReferenceProductRejectsWidening(t *testing.T) {
	if _, err := compileStagedNullReferenceProductForTest(stagedFirstNullReferenceModule(true)); err == nil || !strings.Contains(err.Error(), "immutable exact null") {
		t.Fatalf("mutable anyref global compile = %v, want exact-product rejection", err)
	}

	m, err := wasm.DecodeModule(stagedFirstNullReferenceModule(false))
	if err != nil {
		t.Fatal(err)
	}
	features := frontend.AllFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = false
	if err := frontend.RejectUnsupportedWithFeatures(m, features); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("frontend without null-product gate = %v, want explicit any-reference rejection", err)
	}
}
