//go:build linux && amd64

package amd64

import "testing"

func TestSelect(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32 i32 i32) (result i32)
		local.get 1 local.get 2 local.get 0 select))`)
	if got := runI32(t, m, 1, 7, 9); got != 7 {
		t.Errorf("select(true,7,9)=%d want 7", got)
	}
	if got := runI32(t, m, 0, 7, 9); got != 9 {
		t.Errorf("select(false,7,9)=%d want 9", got)
	}
}
