package wago

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/tests/wasmtest"
)

type gcConstDirectTestRoots []corergc.Ref

func (r gcConstDirectTestRoots) RangeRoots(fn func(corergc.RootSlot) bool) {
	for i := range r {
		root := corergc.Root(r[i])
		if !fn(&root) {
			return
		}
	}
}

func (r gcConstDirectTestRoots) RangeRootRefs(sink corergc.RootRefSink) bool {
	for _, ref := range r {
		if !sink.VisitRootRef(ref) {
			return false
		}
	}
	return true
}

type gcConstStoppingClassifiedSink struct {
	refs  []corergc.Ref
	limit int
}

func (s *gcConstStoppingClassifiedSink) VisitClassifiedRootRef(_ corergc.RootClass, ref corergc.Ref) bool {
	s.refs = append(s.refs, ref)
	return s.limit == 0 || len(s.refs) < s.limit
}

func TestGCConstClassifiedRootsHonorDirectSinkStop(t *testing.T) {
	stack := []gcConstStackValue{{ref: corergc.I31New(2), kind: gcConstCollectorRef}}
	roots := gcConstStackRootSet{stack: &stack, extra: gcConstDirectTestRoots{corergc.I31New(1)}}
	sink := &gcConstStoppingClassifiedSink{limit: 1}
	if roots.RangeClassifiedRootRefs(sink) {
		t.Fatal("classified direct stop reported completion")
	}
	if len(sink.refs) != 1 || sink.refs[0] != corergc.I31New(1) {
		t.Fatalf("classified direct stop visited %v", sink.refs)
	}

	elements := &gcArrayElementRoots{Count: 1, Values: []corergc.Root{corergc.Root(corergc.I31New(3))}}
	roots.extra = elements
	sink = &gcConstStoppingClassifiedSink{limit: 1}
	if roots.RangeClassifiedRootRefs(sink) {
		t.Fatal("classified attributed stop reported completion")
	}
	if len(sink.refs) != 1 || sink.refs[0] != corergc.I31New(3) {
		t.Fatalf("classified attributed stop visited %v", sink.refs)
	}
}

func gcConstExprRootingModule() []byte {
	leaf := []byte{0x5f, 0x01, 0x7f, 0x00} // struct { const i32 }
	pair := []byte{0x5f, 0x02, 0x64, 0x00, 0x00, 0x64, 0x00, 0x00}
	first := []byte{
		0x64, 0x00, 0x00, // (global (ref 0))
		0x41, 0x21, 0xfb, 0x00, 0x00, 0x0b, // struct.new 0 (33)
	}
	second := []byte{
		0x64, 0x01, 0x00, // (global (ref 1))
		0x41, 0x0b, 0xfb, 0x00, 0x00, // first child remains live below the next allocation
		0x41, 0x16, 0xfb, 0x00, 0x00,
		0xfb, 0x00, 0x01, 0x0b, // struct.new 1
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(leaf, pair)),
		wasmtest.Section(6, wasmtest.Vec(first, second)),
	)
}

func TestGCConstExprRootsStackAndEarlierGlobals(t *testing.T) {
	requireCompleteCore3Backend(t)
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcConstExprRootingModule())
	if err != nil {
		t.Fatalf("compile GC constant-expression rooting module: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatalf("instantiate GC constant-expression rooting module: %v", err)
	}
	defer in.Close()

	first := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	value, err := in.gc.StructGet(first, 0)
	if err != nil || value.Bits != 33 {
		t.Fatalf("earlier global after later allocations = %+v, %v; want 33", value, err)
	}
	pair := corergc.Ref(uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)))
	for field, want := range []uint64{11, 22} {
		child, err := in.gc.StructGet(pair, uint32(field))
		if err != nil {
			t.Fatalf("pair field %d: %v", field, err)
		}
		got, err := in.gc.StructGet(child.Ref, 0)
		if err != nil || got.Bits != want {
			t.Fatalf("pair field %d value = %+v, %v; want %d", field, got, err, want)
		}
	}
}

func gcConstExprFuncrefModule() []byte {
	fn := wasmtest.FuncType(nil, nil)
	nullBox := []byte{0x5f, 0x01, 0x63, byte(wasm.HeapFunc), 0x00}
	funcBox := []byte{0x5f, 0x01, 0x64, byte(wasm.HeapFunc), 0x00}
	nullGlobal := []byte{
		0x64, 0x01, 0x00,
		0xd0, byte(wasm.HeapFunc), 0xfb, 0x00, 0x01, 0x0b,
	}
	funcGlobal := []byte{
		0x64, 0x02, 0x00,
		0xd2, 0x00, 0xfb, 0x00, 0x02, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(fn, nullBox, funcBox)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(nullGlobal, funcGlobal)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func TestGCConstExprStoresFunctionReferences(t *testing.T) {
	requireCompleteCore3Backend(t)
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcConstExprFuncrefModule())
	if err != nil {
		t.Fatalf("compile function-reference GC constants: %v", err)
	}
	defer compiled.Close()
	loaded := publicArtifactRoundTrip(t, compiled)
	defer loaded.Close()
	for _, candidate := range []*Compiled{compiled, loaded} {
		in, err := instantiateCore(candidate, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatalf("instantiate function-reference GC constants: %v", err)
		}
		nullBox := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
		nullValue, err := in.gc.StructGet(nullBox, 0)
		if err != nil || nullValue.Kind != corergc.StorageFuncRefNull || nullValue.Bits != 0 {
			in.Close()
			t.Fatalf("nullable function field = %+v, %v", nullValue, err)
		}
		funcBox := corergc.Ref(uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)))
		funcValue, err := in.gc.StructGet(funcBox, 0)
		if err != nil || funcValue.Kind != corergc.StorageFuncRef || funcValue.Bits == 0 {
			in.Close()
			t.Fatalf("non-null function field = %+v, %v", funcValue, err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCConstExprRootsEarlierElementEntries(t *testing.T) {
	requireCompleteCore3Backend(t)
	data, err := os.ReadFile("testdata/gc_constexpr_element_roots.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("sum")
	if err != nil || len(got) != 1 || got[0] != 33 {
		t.Fatalf("element values after later allocation = %v, %v; want [33]", got, err)
	}
}

func TestCompiledCodecRejectsIllTypedGCConstExpr(t *testing.T) {
	requireCompleteCore3Backend(t)
	compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcConstExprRootingModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	compiled = mutableCompiledFixture(compiled)
	compiled.Globals[1].InitExpr = []byte{0xd0, byte(wasm.HeapNone), 0x0b}
	if _, err := compiled.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted ref.null none for a non-null concrete global")
	}

	compiled, err = NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcConstExprRootingModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	compiled = mutableCompiledFixture(compiled)
	compiled.Globals[1].InitExpr = []byte{0xfb, 0x01, 0x7f, 0x0b}
	if _, err := compiled.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted an out-of-range GC constructor type")
	}

	data, err := os.ReadFile("testdata/gc_constexpr_element_roots.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err = NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	compiled = mutableCompiledFixture(compiled)
	compiled.Elems[0].Values[0].Expr = []byte{0xd0, byte(wasm.HeapNone), 0x0b}
	if _, err := compiled.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted a null GC element for a non-null concrete element type")
	}
}
