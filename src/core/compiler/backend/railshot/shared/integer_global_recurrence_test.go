package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntegerSumAdditiveGlobalRecurrence(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}}}
	body := []byte{0x40, 0x20, 1, 0x02, 0x7f, 0x23, 0, 0x20, 2, 0x6a, 0x24, 0, 0x23, 0, 0x0b, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoopForModule(&r, m, false)
	if !ok || !p.GlobalRecurrence || p.Global != 0 || p.IncrementCoefficient != 1 || p.IncrementConstant != 0 || p.GlobalCoefficient != 1 || p.Coefficient != 1 || p.Constant != 0 || p.Counter != 2 || p.Accumulator != 1 || p.Bound != 0 || p.End != len(body) || r.Offset() != 0 {
		t.Fatalf("recurrence rejected: %+v", p)
	}
	for cut := 0; cut < len(body); cut++ {
		r := wasm.ReaderFrom(body[:cut])
		if _, ok := InspectIntegerSumLoopForModule(&r, m, false); ok || r.Offset() != 0 {
			t.Fatalf("accepted truncation at %d", cut)
		}
	}
	fenced := bytes.Replace(body, []byte{0x40, 0x20, 1}, []byte{0x40, 0xfe, 3, 0, 0x20, 1}, 1)
	r = wasm.ReaderFrom(fenced)
	if _, ok := InspectIntegerSumLoopForModule(&r, m, false); ok {
		t.Fatal("accepted fenced global recurrence")
	}
}

func TestIntegerGlobalRecurrenceRejectsNonAdditiveAndEffects(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}, {Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}}}}
	for name, body := range map[string][]byte{
		"non-additive":    {0x23, 0, 0x41, 2, 0x6c, 0x24, 0, 0x23, 0, 0x0b},
		"nonlinear":       {0x23, 0, 0x20, 2, 0x6c, 0x24, 0, 0x23, 0, 0x0b},
		"other-global":    {0x23, 0, 0x23, 1, 0x6a, 0x24, 0, 0x23, 0, 0x0b},
		"other-store":     {0x23, 0, 0x20, 2, 0x6a, 0x24, 1, 0x23, 0, 0x0b},
		"repeated-store":  {0x23, 0, 0x20, 2, 0x6a, 0x24, 0, 0x23, 0, 0x24, 0, 0x23, 0, 0x0b},
		"trapping-value":  {0x23, 0, 0x20, 2, 0x6e, 0x24, 0, 0x23, 0, 0x0b},
		"call":            {0x23, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x0b},
		"memory":          {0x23, 0, 0x20, 2, 0x28, 2, 0, 0x6a, 0x24, 0, 0x23, 0, 0x0b},
		"other-local":     {0x23, 0, 0x20, 2, 0x6a, 0x20, 3, 0x6a, 0x24, 0, 0x23, 0, 0x0b},
		"converted-width": {0x23, 0, 0xad, 0xa7, 0x24, 0, 0x23, 0, 0x0b},
		"unknown-global":  {0x23, 2, 0x20, 2, 0x6a, 0x24, 2, 0x23, 2, 0x0b},
		"no-store":        {0x23, 0, 0x20, 2, 0x6a, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if p, ok := inspectIntegerGlobalRecurrence(&r, m, 4); ok {
			t.Fatalf("accepted %s: %+v", name, p)
		}
	}
	good := []byte{0x23, 0, 0x20, 2, 0x6a, 0x24, 0, 0x23, 0, 0x0b}
	for _, badType := range []wasm.GlobalType{{Type: wasm.I32, Mutable: false}, {Type: wasm.F32, Mutable: true}, {Type: wasm.I64, Mutable: true}} {
		m.Globals[0].Type = badType
		r := wasm.ReaderFrom(good)
		if _, ok := inspectIntegerGlobalRecurrence(&r, m, 4); ok {
			t.Fatalf("accepted type %+v", badType)
		}
	}
	m.Globals[0].Type = wasm.GlobalType{Type: wasm.I32, Mutable: true}
	m.Imports = []wasm.Import{{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32, Mutable: true})}}
	r := wasm.ReaderFrom(good)
	if _, ok := inspectIntegerGlobalRecurrence(&r, m, 4); ok {
		t.Fatal("accepted imported cell")
	}
	m.Imports = nil
	body := append([]byte{}, good[:len(good)-1]...)
	body = append(body, bytes.Repeat([]byte{0x41, 0, 0x6a}, 32)...)
	body = append(body, 0x0b)
	r = wasm.ReaderFrom(body)
	if _, ok := inspectIntegerGlobalRecurrence(&r, m, 4); ok {
		t.Fatal("exceeded expression budget")
	}
}
