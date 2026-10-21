//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDirectForwarderBounds(t *testing.T) {
	for _, hops := range []int{0, 1, 4, 5} {
		t.Run(fmt.Sprint(hops), func(t *testing.T) {
			defs := make([]funcDef, hops+1)
			for i := range defs {
				defs[i] = funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, byte(i + 1), 0x0b}}
			}
			defs[hops].body = []byte{0, 0x20, 0, 0x0b}
			m := modFuncs(t, defs...)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			got, ok := directForwarderTarget(m, 0, 0)
			want := hops > 0 && hops <= maxDirectForwarderHops
			if ok != want || want && got != uint32(hops) {
				t.Fatalf("target=%d,%t for %d hops", got, ok, hops)
			}
		})
	}
}

func TestDirectForwarderRejectsNonIdentity(t *testing.T) {
	for _, body := range [][]byte{
		{1, 1, 0x7f, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}, // declared local
		{0, 0x20, 1, 0x20, 0, 0x10, 1, 0x0b},          // reordered inputs
		{0, 0x20, 0, 0x20, 0, 0x10, 1, 0x0b},          // repeated input
		{0, 0x01, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b},    // extra instruction
		{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x01, 0x0b},    // work after call
		{0, 0x20, 0, 0x20, 1, 0x12, 1, 0x0b},          // return_call is not call
	} {
		ft := []wasm.ValType{wasm.I32, wasm.I32}
		m := modFuncs(t, funcDef{params: ft, results: ft[:1], body: body}, funcDef{params: ft, results: ft[:1], body: []byte{0, 0x20, 0, 0x0b}})
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted nonidentity %x -> %d", body, target)
		}
	}
	for _, cycle := range []int{1, 2, 5} {
		defs := make([]funcDef, cycle)
		for i := range defs {
			defs[i].body = []byte{0, 0x10, byte((i + 1) % cycle), 0x0b}
		}
		m := modFuncs(t, defs...)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted %d-cycle ->%d", cycle, target)
		}
	}
}

func TestDirectForwarderExactReferenceTypes(t *testing.T) {
	any := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	eq := wasm.RefVal(wasm.AbsRef(wasm.HeapEq))
	for _, paramChange := range []bool{false, true} {
		wrapperParam, targetResult := any, eq
		if paramChange {
			wrapperParam, targetResult = eq, any
		}
		m := modFuncs(t,
			funcDef{params: []wasm.ValType{wrapperParam}, results: []wasm.ValType{any}, body: []byte{0, 0x20, 0, 0x10, 1, 0x0b}},
			funcDef{params: []wasm.ValType{any}, results: []wasm.ValType{targetResult}, body: []byte{0, 0xd0, 0x6d, 0x0b}},
		)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		if target, ok := directForwarderTarget(m, 0, 0); ok {
			t.Fatalf("accepted same-machine-class reference mismatch ->%d", target)
		}
	}
	if equalForwarderTypes([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}) {
		t.Fatal("equal-width integer/float types must differ")
	}
	nonnull := wasm.RefVal(any.Ref().WithNullable(false))
	if equalForwarderTypes([]wasm.ValType{any}, []wasm.ValType{nonnull}) {
		t.Fatal("reference nullability must match exactly")
	}
}

func TestDirectForwarderBodyLimit(t *testing.T) {
	const params = 80
	types := make([]wasm.ValType, params)
	wrapper, target := []byte{0}, []byte{0}
	for i := range types {
		types[i] = wasm.I32
		wrapper = append(wrapper, 0x20, byte(i))
		target = append(target, 0x20, byte(i), 0x1a)
	}
	wrapper = append(wrapper, 0x10, 1, 0x0b)
	target = append(target, 0x0b)
	m := modFuncs(t, funcDef{params: types, body: wrapper}, funcDef{params: types, body: target})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if target, ok := directForwarderTarget(m, 0, 0); ok {
		t.Fatalf("accepted oversized wrapper -> %d", target)
	}
}

func TestDirectForwarderStopsAtImport(t *testing.T) {
	imp := append(wasmtest.Name("host"), wasmtest.Name("call")...)
	imp = append(imp, 0, 0)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}), wasmtest.Code([]byte{0x10, 1, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, first := range []uint32{0, 1, 2} {
		if target, ok := directForwarderTarget(m, 1, first); ok {
			t.Fatalf("import chain %d -> %d", first, target)
		}
	}
}
