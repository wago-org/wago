package shared

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntegerAffineTermProof(t *testing.T) {
	for _, wide := range []bool{false, true} {
		c, add, sub, mul := byte(0x41), byte(0x6a), byte(0x6b), byte(0x6c)
		bits := 64
		mask := ^uint64(0)
		if wide {
			c, add, sub, mul = 0x42, 0x7c, 0x7d, 0x7e
		} else {
			bits = 32
			mask = 0xffffffff
		}
		constant := func(v int64) []byte {
			if wide {
				return append([]byte{c}, wasmtest.SLEB64(v)...)
			}
			return append([]byte{c}, wasmtest.SLEB32(int32(v))...)
		}
		i := []byte{0x20, 2}
		term := func(parts ...[]byte) []byte {
			var out []byte
			for _, p := range parts {
				out = append(out, p...)
			}
			return out
		}
		cases := []struct {
			name string
			body []byte
			a, b uint64
		}{
			{"multiply-add", term(i, constant(17), []byte{mul}, constant(3), []byte{add}), 17, 3},
			{"commuted", term(constant(17), i, []byte{mul}, constant(3), []byte{add}), 17, 3},
			{"repeated-counter", term(i, i, []byte{add}, constant(5), []byte{sub}), 2, mask - 4},
			{"zero-factor", term(i, constant(0), []byte{mul}, constant(9), []byte{add}), 0, 9},
			{"constant-only", constant(7), 0, 7},
			{"cancelled", term(i, i, []byte{sub}, constant(4), []byte{mul}), 0, 0},
			{"negative", term(i, constant(-1), []byte{mul}), mask, 0},
			{"wrapped-factor", term(i, constant(int64(uint64(1)<<(bits-1))), []byte{mul}, constant(2), []byte{mul}), 0, 0},
		}
		for _, tc := range cases {
			for _, blocked := range []bool{false, true} {
				body := append([]byte{}, tc.body...)
				finish := add
				if blocked {
					finish = 0x0b
				}
				body = append(body, finish)
				r := wasm.ReaderFrom(body)
				p, ok := inspectIntegerAffineTerm(&r, blocked)
				if !ok || p.coefficient != tc.a || p.constant != tc.b || r.Offset() != len(tc.body) {
					t.Fatalf("wide=%t %s block=%t plan=%+v ok=%t offset=%d", wide, tc.name, blocked, p, ok, r.Offset())
				}
			}
		}
		for name, body := range map[string][]byte{
			"nonlinear":     term(i, i, []byte{mul, add}),
			"changed-local": term(i, []byte{0x20, 3, add, add}),
			"memory":        {0x41, 0, 0x28, 2, 0, add},
			"call":          {0x10, 0, add},
			"division":      term(i, constant(2), []byte{mul + 1, add}),
			"mixed-width":   {0x41, 1, 0x42, 2, add, add},
		} {
			r := wasm.ReaderFrom(body)
			if _, ok := inspectIntegerAffineTerm(&r, false); ok {
				t.Fatalf("wide=%t accepted %s", wide, name)
			}
		}
	}
}

func TestIntegerSumPureHelperCall(t *testing.T) {
	helper := []byte{0x20, 0, 0x41, 3, 0x6c, 0x41, 1, 0x6a, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper)))))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0x40, 0x20, 1, 0x20, 2, 0x10, 0, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoopForModule(&r, m, true)
	if !ok || p.Coefficient != 3 || p.Constant != 1 || p.Calls != 1 || p.Counter != 2 || r.Offset() != 0 {
		t.Fatalf("plan=%+v ok=%t offset=%d", p, ok, r.Offset())
	}
	for calls := 4; calls <= 5; calls++ {
		term := append([]byte{0x20, 2}, bytes.Repeat([]byte{0x10, 0}, calls)...)
		changed := bytes.Replace(body, []byte{0x20, 2, 0x10, 0}, term, 1)
		r = wasm.ReaderFrom(changed)
		p, ok = InspectIntegerSumLoopForModule(&r, m, true)
		if ok != (calls == 4) || ok && (p.Calls != 4 || p.Coefficient != 81 || p.Constant != 40) {
			t.Fatalf("calls=%d plan=%+v ok=%t", calls, p, ok)
		}
	}
	m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.I32}}
	r = wasm.ReaderFrom(body)
	if _, ok := InspectIntegerSumLoopForModule(&r, m, true); ok {
		t.Fatal("accepted declared helper local")
	}
	m.Code[0].Locals.Runs = nil
	m.Imports = []wasm.Import{{Module: "env", Name: "f", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})}}
	r = wasm.ReaderFrom(body)
	if _, ok := InspectIntegerSumLoopForModule(&r, m, true); ok {
		t.Fatal("accepted imported helper")
	}
	changed := bytes.Replace(body, []byte{0x10, 0}, []byte{0x10, 1}, 1)
	r = wasm.ReaderFrom(changed)
	if _, ok := InspectIntegerSumLoopForModule(&r, m, true); !ok {
		t.Fatal("rejected local helper after import")
	}
	m.Imports = nil
	for name, bad := range map[string][]byte{
		"global": {0x23, 0, 0x0b}, "load": {0x20, 0, 0x28, 2, 0, 0x0b}, "call": {0x20, 0, 0x10, 0, 0x0b}, "trap": {0x00, 0x0b}, "nonlinear": {0x20, 0, 0x20, 0, 0x6c, 0x0b}, "mixed-width": {0x42, 1, 0x0b},
	} {
		m.Code[0].BodyBytes = bad
		r = wasm.ReaderFrom(body)
		if _, ok := InspectIntegerSumLoopForModule(&r, m, true); ok {
			t.Fatalf("accepted %s helper", name)
		}
	}
}

func TestIntegerSumInvariantGlobal(t *testing.T) {
	body := []byte{0x40, 0x20, 1, 0x23, 0, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32}}}}
	r := wasm.ReaderFrom(body)
	p, ok := InspectIntegerSumLoopForModule(&r, m, false)
	if !ok || !p.InvariantGlobal || p.Global != 0 || p.Calls != 0 || r.Offset() != 0 {
		t.Fatalf("plan=%+v ok=%t offset=%d", p, ok, r.Offset())
	}
	m.Globals[0].Type.Mutable = true
	r = wasm.ReaderFrom(body)
	if _, ok := InspectIntegerSumLoopForModule(&r, m, true); ok {
		t.Fatal("accepted mutable global")
	}
}
