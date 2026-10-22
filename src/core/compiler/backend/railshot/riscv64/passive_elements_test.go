//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func passiveElementsProfile(expressions bool) *wasm.Module {
	m := &wasm.Module{Code: make([]wasm.Func, 1), Elements: make([]wasm.Elem, MaxOwnedElementSegments)}
	for i := range m.Elements {
		m.Elements[i] = wasm.Elem{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0, 0, 0, 0}}}
		if expressions {
			m.Elements[i].Kind = wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.FuncRef.Ref(), Exprs: []wasm.Expr{
				{BodyBytes: []byte{0xd2, 0, 0x0b}}, {BodyBytes: []byte{0xd0, 0x70, 0x0b}},
				{BodyBytes: []byte{0xd2, 0, 0x0b}}, {BodyBytes: []byte{0xd0, 0x70, 0x0b}},
			}}
		}
	}
	return m
}

func TestTableFreePassiveElementProfile(t *testing.T) {
	for _, expressions := range []bool{false, true} {
		t.Run(fmt.Sprintf("expressions%t", expressions), func(t *testing.T) {
			m := passiveElementsProfile(expressions)
			if table, err := admitTableProfile(m, true, true); table || err != nil {
				t.Fatal("bounded passive profile needs no table", table, err)
			}
			// An additional empty segment consumes metadata, not element slots.
			larger := passiveElementsProfile(expressions)
			larger.Elements = append(larger.Elements, wasm.Elem{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}})
			if _, err := admitTableProfile(larger, true, true); err != nil {
				t.Fatal("empty public segment above the private count limit", err)
			}
			for _, production := range []bool{false, true} {
				if _, err := admitTableProfile(m, production, false); !errors.Is(err, ErrUnsupported) {
					t.Fatal("private table-free profile widened", err)
				}
			}
			for name, mutate := range map[string]func(*wasm.Module){
				"active": func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive },
				"offset": func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
				"table":  func(m *wasm.Module) { m.Elements[0].Mode.Table = 1 },
				"aggregate": func(m *wasm.Module) {
					if expressions {
						m.Elements[0].Kind.Exprs = append(m.Elements[0].Kind.Exprs, wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}})
					} else {
						m.Elements[0].Kind.Funcs = append(m.Elements[0].Kind.Funcs, 0)
					}
				},
				"function": func(m *wasm.Module) {
					if expressions {
						m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 1, 0x0b}
					} else {
						m.Elements[0].Kind.Funcs[0] = 1
					}
				},
			} {
				bad := passiveElementsProfile(expressions)
				mutate(bad)
				if _, err := admitTableProfile(bad, true, true); !errors.Is(err, ErrUnsupported) {
					t.Fatal("invalid passive profile admitted", name, err)
				}
			}
		})
	}
}

func TestTableFreePassiveInstructionsRetainTableAndIndexGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, public := range []bool{false, true} {
		state := moduleState{production: true, importReferences: public, elementCount: 256}
		for _, tc := range []struct {
			body  []byte
			valid bool
		}{
			{[]byte{0xfc, 13, 0xff, 1, 0xfc, 13, 0xff, 1, 0x0b}, public},
			{[]byte{0xfc, 13, 0x80, 2, 0x0b}, false},
			{[]byte{0x00, 0xfc, 13, 0x80, 2, 0x0b}, false},
			{[]byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 12, 0, 0, 0x0b}, false},
			{[]byte{0x00, 0xfc, 12, 0, 0, 0x0b}, false},
		} {
			fn := &wasm.Func{BodyBytes: tc.body}
			_, scalar := walkInteger(fn, ft, &state, nil)
			_, swar := walkSWARPairsWithImportMode(fn, ft, false, nil, nil, nil, 0, nil, nil, 256, 0, public, false, nil)
			for _, err := range []error{scalar, swar} {
				if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrUnsupported) {
					t.Fatalf("public=%t body=%x: %v", public, tc.body, err)
				}
			}
		}
	}
}
