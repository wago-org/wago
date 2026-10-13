package shared

import (
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
