//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func iteratorFixtureSeq(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
func iteratorFixtureLocal(n uint32) []byte { return append([]byte{0x20}, wasmtest.ULEB(n)...) }
func iteratorFixtureSet(n uint32) []byte   { return append([]byte{0x21}, wasmtest.ULEB(n)...) }
func iteratorFixtureCall(n uint32) []byte  { return append([]byte{0x10}, wasmtest.ULEB(n)...) }
func iteratorFixtureFB(op byte, idx ...uint32) []byte {
	b := []byte{0xfb, op}
	for _, v := range idx {
		b = append(b, wasmtest.ULEB(v)...)
	}
	return b
}
func iteratorFixtureValueType(v wasm.ValType) []byte {
	if v.Kind() == wasm.ValRef && v.Ref().Heap().Kind() == wasm.HeapTypeIndex {
		prefix := byte(0x64)
		if v.Ref().Nullable() {
			prefix = 0x63
		}
		return append([]byte{prefix}, iteratorFixtureHeap(v.Ref().Heap().Type().Index)...)
	}
	return []byte{wasm.MustEncodeValType(v)}
}
func iteratorFixtureFunc(params, results []wasm.ValType) []byte {
	out := append([]byte{0x60}, wasmtest.ULEB(uint32(len(params)))...)
	for _, v := range params {
		out = append(out, iteratorFixtureValueType(v)...)
	}
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	for _, v := range results {
		out = append(out, iteratorFixtureValueType(v)...)
	}
	return out
}
func iteratorFixtureRef(n uint32) wasm.ValType {
	return wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: n}), false))
}
func iteratorFixtureHeap(n uint32) []byte {
	var b []byte
	for {
		v := byte(n & 127)
		n >>= 7
		if n != 0 || v&64 != 0 {
			b = append(b, v|128)
		} else {
			return append(b, v)
		}
	}
}
func iteratorFixtureRefBytes(n uint32) []byte { return append([]byte{0x64}, iteratorFixtureHeap(n)...) }
func iteratorFixtureCast(n uint32) []byte     { return append([]byte{0xfb, 22}, iteratorFixtureHeap(n)...) }
func iteratorFixtureConst(n uint32) []byte {
	b := []byte{0x41}
	for {
		v := byte(n & 127)
		n >>= 7
		if n != 0 || v&64 != 0 {
			b = append(b, v|128)
		} else {
			return append(b, v)
		}
	}
}
func iteratorFixtureBody(locals []byte, code []byte) []byte {
	body := append(append([]byte(nil), locals...), code...)
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}
func iteratorFixtureGlobal(typ uint32, code []byte) []byte {
	return iteratorFixtureSeq(iteratorFixtureRefBytes(typ), []byte{0}, code, []byte{0x0b})
}
func iteratorCarrierFixture(count, typeShift, functionShift uint32) ([]byte, uint32) {
	T := func(n uint32) uint32 { return n + typeShift }
	F := func(n uint32) uint32 { return n + functionShift }
	field := func(n uint32) []byte { return iteratorFixtureSeq(iteratorFixtureRefBytes(T(n)), []byte{0}) }
	types := make([][]byte, typeShift)
	for i := range types {
		types[i] = iteratorFixtureFunc(nil, nil)
	}
	types = append(types, []byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x6d, 1}, iteratorFixtureSeq([]byte{0x5f}, wasmtest.Vec(field(1), []byte{0x7f, 0})), iteratorFixtureSeq([]byte{0x5f}, wasmtest.Vec(field(2), []byte{0x7f, 1})), []byte{0x50, 0, 0x5f, 1, 0x7f, 0}, iteratorFixtureSeq([]byte{0x4f, 1}, wasmtest.ULEB(T(4)), []byte{0x5f, 2, 0x7f, 0, 0x6d, 0}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(2)), wasm.I32}, []wasm.ValType{wasm.EqRef}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{wasm.I32}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{wasm.EqRef}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{iteratorFixtureRef(T(4))}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{iteratorFixtureRef(T(4))}), iteratorFixtureFunc(nil, []wasm.ValType{wasm.I32}), iteratorFixtureFunc(nil, nil))
	get := iteratorFixtureSeq(iteratorFixtureLocal(1), iteratorFixtureLocal(0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 1), []byte{0x4f, 4, 0x6d, 0, 5}, iteratorFixtureLocal(0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 0), iteratorFixtureCast(T(1)), iteratorFixtureLocal(1), iteratorFixtureFB(11, T(1)), []byte{0x0b, 0x0b})
	has := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 1), []byte{0x49, 0x0b})
	payload := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 0), iteratorFixtureCast(T(2)), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), iteratorFixtureCall(F(0)), iteratorFixtureSet(1), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), []byte{0x41, 1, 0x6a}, iteratorFixtureFB(5, T(3), 1), iteratorFixtureLocal(1), []byte{0x0b})
	producer := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(1)), []byte{4, 0x6d}, iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(2)), iteratorFixtureSet(1), []byte{0x41, 1}, iteratorFixtureLocal(1), iteratorFixtureFB(0, T(5)), []byte{5, 0x41, 0}, iteratorFixtureFB(0, T(4)), []byte{0x0b}, iteratorFixtureCast(T(4)), []byte{0x0b})
	wrapper := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(3)), iteratorFixtureCast(T(4)), []byte{0x0b})
	step := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(4)), iteratorFixtureSet(1))
	tag := iteratorFixtureSeq(iteratorFixtureLocal(1), iteratorFixtureCast(T(4)), iteratorFixtureFB(2, T(4), 0))
	run := iteratorFixtureSeq([]byte{0x23, 3}, iteratorFixtureSet(0), step, []byte{2, 0x40, 3, 0x40, 2, 0x40}, tag, []byte{0x41, 1, 0x46, 4, 0x40}, iteratorFixtureLocal(1), iteratorFixtureCast(T(5)), iteratorFixtureFB(2, T(5), 1), iteratorFixtureSet(2), iteratorFixtureLocal(2), []byte{0xfb, 22, 0x6c, 0xfb, 30}, iteratorFixtureLocal(3), []byte{0x6a}, iteratorFixtureSet(3), step, []byte{0x0c, 2, 0x0c, 1, 0x0b}, tag, []byte{0x41, 0, 0x46, 4, 0x40, 0x0c, 3, 0x0c, 1, 0x0b, 0, 0x0b, 0, 0x0b, 0, 0x0b}, iteratorFixtureLocal(3), []byte{0x0b})
	index := iteratorFixtureSeq([]byte{0x23, 3}, iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), []byte{0x0b})
	declarations := make([][]byte, functionShift)
	bodies := make([][]byte, functionShift)
	for i := range declarations {
		declarations[i] = wasmtest.ULEB(T(12))
		bodies[i] = wasmtest.Code([]byte{0x0b})
	}
	for _, n := range []uint32{6, 7, 8, 9, 10, 11, 11} {
		declarations = append(declarations, wasmtest.ULEB(T(n)))
	}
	bodies = append(bodies, wasmtest.Code(get), wasmtest.Code(has), iteratorFixtureBody([]byte{1, 1, 0x6d}, payload), iteratorFixtureBody([]byte{1, 1, 0x6d}, producer), wasmtest.Code(wrapper), iteratorFixtureBody(iteratorFixtureSeq([]byte{4, 1, 0x63}, wasmtest.ULEB(T(3)), []byte{1, 0x6d, 1, 0x6d, 1, 0x7f}), run), wasmtest.Code(index))
	var elements []byte
	for i := uint32(0); i < count; i++ {
		elements = append(elements, iteratorFixtureConst(i)...)
		elements = append(elements, 0xfb, 28)
	}
	globals := wasmtest.Vec(iteratorFixtureGlobal(T(0), iteratorFixtureSeq([]byte{0x41, 42}, iteratorFixtureFB(0, T(0)))), iteratorFixtureGlobal(T(1), iteratorFixtureSeq(elements, iteratorFixtureFB(8, T(1), count))), iteratorFixtureGlobal(T(2), iteratorFixtureSeq([]byte{0x23, 1}, iteratorFixtureConst(count), iteratorFixtureFB(0, T(2)))), iteratorFixtureGlobal(T(3), iteratorFixtureSeq([]byte{0x23, 2, 0x41, 0}, iteratorFixtureFB(0, T(3)))))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(declarations...)), wasmtest.Section(6, globals), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, F(5)), wasmtest.ExportEntry("index", 0, F(6)), wasmtest.ExportEntry("next", 0, F(4)))), wasmtest.Section(10, wasmtest.Vec(bodies...))), T(5)
}
func iteratorPublishedType(t *testing.T, in *Instance, localType uint32) int {
	t.Helper()
	in.gc.Stats()
	in.gc.CancelNativeAllocationBatch()
	v := in.gc.NativeView()
	if v.HandleStride != gc.NativeHandleStride {
		t.Fatal("wrong handle stride")
	}
	entries := unsafe.Slice((*byte)(offHeapPtr(v.Handles)), int(v.HandleCount)*gc.NativeHandleStride)
	target := gc.TypeID(in.requireGCDomainType(localType))
	count := 0
	for h := uint32(1); h < v.HandleCount; h++ {
		if entries[int(h)*gc.NativeHandleStride+gc.NativeHandleSpaceOffset] == gc.NativeSpaceFree {
			continue
		}
		typ, e := in.gc.ObjectType(gc.Ref(h << 1))
		if e != nil {
			t.Fatalf("published handle%d invalid: %v", h, e)
		}
		if typ == target {
			count++
		}
	}
	return count
}
func TestIteratorCarrierElisionWitness(t *testing.T) {
	source, some := iteratorCarrierFixture(16, 0, 0)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	m, e := rt.Compile(source)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	in, e := rt.Instantiate(context.Background(), m, WithGC(GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	roots := in.c.GCNativeRootAdmission()
	if !roots.Required || !roots.Exact {
		t.Fatalf("root admission%+v", roots)
	}
	before := in.gc.Stats()
	got, e := in.Invoke("run")
	if e != nil || len(got) != 1 || got[0] != 120 {
		t.Fatalf("sum%v err%v want120", got, e)
	}
	index, e := in.Invoke("index")
	if e != nil || len(index) != 1 || index[0] != 16 {
		t.Fatalf("iterator advancement%v err%v want16", index, e)
	}
	after := in.gc.Stats()
	if after.Allocations-before.Allocations != 17 || after.MinorCollections != 0 || after.FullCollections != 0 {
		t.Fatalf("allocation effects before%+v after%+v", before, after)
	}
	published := iteratorPublishedType(t, in, some)
	t.Logf("result120 index16 logical17 physicalSome%d", published)
	if published > 1 {
		t.Fatalf("physical Some publications%d, want at most1 rooted fallback publication; hot iterator carriers still materialize", published)
	}
}
