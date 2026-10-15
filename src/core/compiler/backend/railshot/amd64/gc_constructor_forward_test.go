//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorForwardModule(t *testing.T, computed bool) *wasm.Module {
	t.Helper()
	base := []byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x00}
	child := []byte{0x50, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x00, 0x7f, 0x00}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00}
	if computed {
		body = append(body, 0x41, 0x01, 0x6a) // local.get 0 + 1 is not a direct local read.
	}
	body = append(body,
		0x41, 0x07,
		0xfb, 0x00, 0x01, // struct.new child
		0xfb, 0x16, 0x00, // ref.cast base
		0xfb, 0x02, 0x00, 0x00, // struct.get base field 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(base, child, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCConstructorGetForwardSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name     string
		computed bool
		want     int
	}{
		{name: "local", want: 1},
		{name: "computed", computed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcConstructorForwardModule(t, tc.computed), CompileOptions{GCStructHelpers: true, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-constructor-get-forward"]; got != tc.want {
				t.Fatalf("constructor forwarding = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGCSubtypeProofCacheBound(t *testing.T) {
	f := fn{gcSubtypeProofN: 4}
	for i := range f.gcSubtypeProofs {
		f.gcSubtypeProofs[i] = gcSubtypeProof{source: uint32(i), target: 9, result: true}
	}
	if !f.cachedGCSubtypeProof(2, 9) {
		t.Fatal("cached proof was not reused")
	}
	if f.cachedGCSubtypeProof(10, 9) {
		t.Fatal("proof beyond the fixed cache bound was admitted")
	}
}

func TestGCFreshStructFactEncoding(t *testing.T) {
	for _, fact := range []gcFreshStructFact{
		{
			value:       storage{idx: 11, kind: stLocalRef, typ: mtF64},
			objectLocal: -1,
			typeIndex:   17,
		},
		{
			value:       storage{idx: 0x101, kind: stLocalRef, typ: mtI64},
			objectLocal: 0x5060708,
			typeIndex:   0x1020304,
		},
	} {
		f := fn{localType: make([]machineType, fact.value.index()+1)}
		f.localType[fact.value.index()] = fact.value.typ
		f.setGCFreshStructFact(fact)
		got, ok := f.getGCFreshStructFact()
		if !ok || got != fact {
			t.Fatalf("fact round trip = %#v, %v; want %#v, true", got, ok, fact)
		}
		f.invalidateGCFreshStructFact()
		if _, ok := f.getGCFreshStructFact(); ok || f.gcSubtypeProofN != 0 {
			t.Fatal("fresh-struct fact was not invalidated")
		}
	}
}

func TestGCFreshStructFactPreservesSubtypeProofs(t *testing.T) {
	f := fn{
		localType:       []machineType{mtI32},
		gcSubtypeProofN: 4,
		gcSubtypeProofs: [4]gcSubtypeProof{
			{source: 2, target: 1, result: true},
			{source: 4, target: 3, result: false},
			{source: 6, target: 5, result: true},
			{source: 8, target: 7, result: false},
		},
	}
	wantProofs := f.gcSubtypeProofs
	f.setGCFreshStructFact(gcFreshStructFact{
		value:       storage{kind: stLocalRef, typ: mtI32},
		objectLocal: -1,
		typeIndex:   3,
	})
	if !f.cachedGCSubtypeProof(2, 1) {
		t.Fatal("established subtype proof was not reused while a fresh-struct fact was live")
	}
	if f.cachedGCSubtypeProof(4, 3) {
		t.Fatal("cached false subtype proof changed while a fresh-struct fact was live")
	}
	f.invalidateGCFreshStructFact()
	for i := range wantProofs {
		wantProofs[i].fresh = [3]byte{}
		f.gcSubtypeProofs[i].fresh = [3]byte{}
	}
	if f.gcSubtypeProofN != 4 || f.gcSubtypeProofs != wantProofs || !f.cachedGCSubtypeProof(2, 1) {
		t.Fatal("established subtype proof was not retained after fresh-struct invalidation")
	}
}

func gcFreshStructLocalModule(t *testing.T, overwriteSource, mutate bool) *wasm.Module {
	t.Helper()
	fieldMutability := byte(0x00)
	if mutate {
		fieldMutability = 0x01
	}
	item := []byte{0x5f, 0x01, 0x7f, fieldMutability} // (struct (field i32))
	roots := []byte{0x5e, 0x63, 0x00, 0x01}           // (array (mut (ref null 0)))
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{
		0x02, 0x01, 0x63, 0x01, 0x01, 0x63, 0x00, // locals: roots, item
		0x41, 0x01, 0xfb, 0x07, 0x01, 0x21, 0x01, // roots = array.new_default 1 (1)
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x02, // item = struct.new 0 (param)
	}
	if overwriteSource {
		body = append(body, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00)
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x02, 0xfb, 0x0e, 0x01, // roots[0] = item
	)
	if mutate {
		body = append(body, 0x20, 0x02, 0x41, 0x07, 0xfb, 0x05, 0x00, 0x00) // item.field 0 = 7
	}
	body = append(body,
		0x20, 0x02, 0xfb, 0x02, 0x00, 0x00, // item.field 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(item, roots, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCFreshStructLocalGetForwardSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name            string
		overwriteSource bool
		mutate          bool
		want            int
	}{
		{name: "root store", want: 1},
		{name: "source overwritten", overwriteSource: true},
		{name: "field mutated", mutate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcFreshStructLocalModule(t, tc.overwriteSource, tc.mutate), CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-fresh-struct-get-forward"]; got != tc.want {
				t.Fatalf("fresh struct forwarding = %d, want %d", got, tc.want)
			}
		})
	}
}
