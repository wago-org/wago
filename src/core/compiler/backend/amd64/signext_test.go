//go:build linux && amd64

package amd64

import "testing"

// TestSignExtendI32 covers i32.extend8_s / i32.extend16_s on register operands.
func TestSignExtendI32(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		in   int32
		want int32
	}{
		{"extend8_s/-1", `local.get 0 i32.extend8_s`, 0xFF, -1},
		{"extend8_s/127", `local.get 0 i32.extend8_s`, 0x1234567F, 127},
		{"extend8_s/-128", `local.get 0 i32.extend8_s`, 0x80, -128},
		{"extend16_s/-1", `local.get 0 i32.extend16_s`, 0xFFFF, -1},
		{"extend16_s/min", `local.get 0 i32.extend16_s`, 0x8000, -32768},
		{"extend16_s/keeplow", `local.get 0 i32.extend16_s`, 0x12347FFF, 0x7FFF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := watToModule(t, `(module (func (export "f") (param i32) (result i32)`+"\n"+c.wat+"))")
			if got := runI32(t, m, c.in); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestSignExtendI64 covers i64.extend8/16/32_s on register operands.
func TestSignExtendI64(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		in   int64
		want int64
	}{
		{"extend8_s/-1", `local.get 0 i64.extend8_s`, 0xFF, -1},
		{"extend8_s/-128", `local.get 0 i64.extend8_s`, 0xFFFFFF80, -128},
		{"extend16_s/-1", `local.get 0 i64.extend16_s`, 0xFFFF, -1},
		{"extend16_s/min", `local.get 0 i64.extend16_s`, 0x8000, -32768},
		{"extend32_s/-1", `local.get 0 i64.extend32_s`, 0xFFFFFFFF, -1},
		{"extend32_s/min", `local.get 0 i64.extend32_s`, 0x80000000, -2147483648},
		{"extend32_s/keeplow", `local.get 0 i64.extend32_s`, 0x1122334400000001, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := watToModule(t, `(module (func (export "f") (param i64) (result i64)`+"\n"+c.wat+"))")
			if got := runI64(t, m, c.in); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestSignExtendConstFold covers the compile-time constant-folding path.
func TestSignExtendConstFold(t *testing.T) {
	m32 := watToModule(t, `(module (func (export "f") (param i32) (result i32)
		i32.const 255 i32.extend8_s))`)
	if got := runI32(t, m32, 0); got != -1 {
		t.Fatalf("i32.const 255 i32.extend8_s = %d, want -1", got)
	}
	m64 := watToModule(t, `(module (func (export "f") (param i64) (result i64)
		i64.const 0xFFFFFFFF i64.extend32_s))`)
	if got := runI64(t, m64, 0); got != -1 {
		t.Fatalf("i64.const 0xFFFFFFFF i64.extend32_s = %d, want -1", got)
	}
}
