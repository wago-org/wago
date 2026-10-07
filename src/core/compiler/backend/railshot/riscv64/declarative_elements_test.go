//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestTableFreeDeclarativeElementProfile(t *testing.T) {
	base := func() *wasm.Module {
		return &wasm.Module{Code: make([]wasm.Func, 1), Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}}
	}
	if table, err := admitTableProfile(base(), true, true); table || err != nil {
		t.Fatal("declarative profile", table, err)
	}
	for _, production := range []bool{false, true} {
		if _, err := admitTableProfile(base(), production, false); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private profile widened", err)
		}
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"active":  func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive },
		"passive": func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive },
		"offset":  func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
		"index":   func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 1 },
		"count":   func(m *wasm.Module) { m.Elements = make([]wasm.Elem, 257) },
		"payload": func(m *wasm.Module) { m.Elements[0].Kind.Funcs = make([]wasm.FuncIdx, 1025) },
	} {
		m := base()
		mutate(m)
		if _, err := admitTableProfile(m, true, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal(name, err)
		}
	}
}

func TestTableFreeElementDropInstructionGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	state := moduleState{production: true, importReferences: true, elementCount: 256}
	for _, tc := range []struct {
		body    []byte
		allowed bool
	}{
		{[]byte{0xfc, 13, 0xff, 1, 0xfc, 13, 0, 0x0b}, true},
		{[]byte{0xfc, 13, 0x80, 2, 0x0b}, false},
		{[]byte{0x00, 0xfc, 13, 0x80, 2, 0x0b}, false},
		{[]byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 12, 0, 0, 0x0b}, false},
		{[]byte{0x00, 0xfc, 12, 0, 0, 0x0b}, false},
	} {
		fn := &wasm.Func{BodyBytes: tc.body}
		_, scalar := walkInteger(fn, ft, &state, nil)
		_, vector := walkSWARPairsWithImportMode(fn, ft, false, nil, nil, nil, 0, nil, nil, 256, 0, true, false, nil)
		for _, err := range []error{scalar, vector} {
			if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, ErrUnsupported) {
				t.Fatalf("%x: %v", tc.body, err)
			}
		}
	}
}
