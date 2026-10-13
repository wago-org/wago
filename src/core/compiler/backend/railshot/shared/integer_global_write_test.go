package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAffineDefinedGlobalSwap(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}, {Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}, {Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}}}
	body := []byte{0x20, 2, 0x24, 0, 0x20, 2, 0x41, 3, 0x6a, 0x24, 1, 0x23, 0, 0x24, 2, 0x23, 1, 0x24, 0, 0x23, 2, 0x24, 1, 0x23, 0, 0x23, 1, 0x6b, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := inspectIntegerAffineTermForModule(&r, true, m, false)
	if !ok || p.coefficient != 0 || p.constant != 3 || p.width != 4 || p.writeCount != 3 {
		t.Fatalf("plan=%+v ok=%t", p, ok)
	}
	want := [3]IntegerAffineGlobalWrite{{Index: 0, Coefficient: 1, Constant: 3}, {Index: 1, Coefficient: 1}, {Index: 2, Coefficient: 1}}
	for j, w := range want {
		if p.writes[j] != w {
			t.Fatalf("write%d=%+v expected=%+v", j, p.writes[j], w)
		}
	}
}

func TestAffineGlobalWritesRejectUnknownValuesAliasesAndBudget(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}}}
	for name, body := range map[string][]byte{
		"read-before-write":    {0x20, 2, 0x23, 0, 0x6a, 0x24, 0, 0x23, 0, 0x0b},
		"unknown-global":       {0x20, 2, 0x24, 1, 0x23, 1, 0x0b},
		"trapping-write-value": {0x20, 2, 0x41, 0, 0x6e, 0x24, 0, 0x23, 0, 0x0b},
		"memory-read":          {0x41, 0, 0x28, 2, 0, 0x24, 0, 0x23, 0, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if _, ok := inspectIntegerAffineTermForModule(&r, true, m, false); ok {
			t.Fatalf("accepted %s", name)
		}
	}
	good := []byte{0x20, 2, 0x24, 0, 0x23, 0, 0x0b}
	for _, blocked := range []bool{false, true} {
		r := wasm.ReaderFrom(good)
		p, ok := inspectIntegerAffineTermForModule(&r, blocked, m, false)
		if ok != blocked {
			t.Fatalf("blocked=%t p=%+v ok=%t", blocked, p, ok)
		}
	}
	m.Imports = []wasm.Import{{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32, Mutable: true})}}
	r := wasm.ReaderFrom(good)
	if _, ok := inspectIntegerAffineTermForModule(&r, true, m, false); ok {
		t.Fatal("accepted imported global")
	}
	m.Imports = nil
	m.Globals[0].Type.Mutable = false
	r = wasm.ReaderFrom(good)
	if _, ok := inspectIntegerAffineTermForModule(&r, true, m, false); ok {
		t.Fatal("accepted immutable global")
	}
	m.Globals = make([]wasm.Global, 5)
	var body []byte
	for j := byte(0); j < 5; j++ {
		m.Globals[j].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
		body = append(body, 0x20, 2, 0x24, j)
	}
	body = append(body, 0x23, 0, 0x0b)
	r = wasm.ReaderFrom(body)
	if _, ok := inspectIntegerAffineTermForModule(&r, true, m, false); ok {
		t.Fatal("write budget exceeded")
	}
}
