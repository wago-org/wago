//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

func TestControlFlow(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		in   int32
		want int32
	}{
		{
			"if_else_value",
			`(module (func (export "f") (param i32) (result i32)
				(if (result i32) (local.get 0) (then (i32.const 10)) (else (i32.const 20)))))`,
			1, 10,
		},
		{
			"if_else_value_false",
			`(module (func (export "f") (param i32) (result i32)
				(if (result i32) (local.get 0) (then (i32.const 10)) (else (i32.const 20)))))`,
			0, 20,
		},
		{
			"if_no_else",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 (if (then (local.set 1 (i32.const 5))))
				local.get 1))`,
			1, 5,
		},
		{
			"if_no_else_false",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 (if (then (local.set 1 (i32.const 5))))
				local.get 1))`,
			0, 0,
		},
		{
			"br_block_value",
			`(module (func (export "f") (result i32)
				(block (result i32) (i32.const 42) (br 0))))`,
			0, 42,
		},
		{
			"br_to_outer",
			`(module (func (export "f") (result i32)
				(block (result i32)
					(block (br 1 (i32.const 7)))
					(i32.const 99))))`,
			0, 7,
		},
		{
			"return_early",
			`(module (func (export "f") (param i32) (result i32)
				local.get 0 (if (then (return (i32.const 123))))
				(i32.const 456)))`,
			1, 123,
		},
		{
			"return_early_false",
			`(module (func (export "f") (param i32) (result i32)
				local.get 0 (if (then (return (i32.const 123))))
				(i32.const 456)))`,
			0, 456,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := watToModule(t, c.wat)
			if got := runI32(t, m, c.in); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

// loop computing sum(0..n) = n(n+1)/2.
func TestLoopSum(t *testing.T) {
	wat := `(module (func (export "f") (param i32) (result i32) (local i32) (local i32)
		;; local0=n  local1=i  local2=sum
		(block
			(loop
				local.get 1 local.get 0 i32.gt_s
				br_if 1
				local.get 2 local.get 1 i32.add local.set 2
				local.get 1 i32.const 1 i32.add local.set 1
				br 0))
		local.get 2))`
	m := watToModule(t, wat)
	for _, n := range []int32{0, 1, 5, 10, 100} {
		got := runI32(t, m, n)
		want := n * (n + 1) / 2
		if got != want {
			t.Fatalf("sum(0..%d): got %d want %d", n, got, want)
		}
	}
}

func TestBrTable(t *testing.T) {
	wat := `(module (func (export "f") (param i32) (result i32)
		(block (block (block
			(br_table 0 1 2 (local.get 0)))
			(return (i32.const 10)))
			(return (i32.const 20)))
		(i32.const 30)))`
	m := watToModule(t, wat)
	cases := map[int32]int32{0: 10, 1: 20, 2: 30, 5: 30}
	for in, want := range cases {
		if got := runI32(t, m, in); got != want {
			t.Fatalf("br_table(%d): got %d want %d", in, got, want)
		}
	}
}

// A nested loop computing a 2D sum, exercising deeper control + spills together.
func TestNestedLoop(t *testing.T) {
	// sum over i in [0,n), j in [0,n) of 1  == n*n
	wat := `(module (func (export "f") (param i32) (result i32) (local i32) (local i32) (local i32)
		;; local0=n local1=i local2=j local3=acc
		(block (loop
			local.get 1 local.get 0 i32.ge_s br_if 1
			i32.const 0 local.set 2
			(block (loop
				local.get 2 local.get 0 i32.ge_s br_if 1
				local.get 3 i32.const 1 i32.add local.set 3
				local.get 2 i32.const 1 i32.add local.set 2
				br 0))
			local.get 1 i32.const 1 i32.add local.set 1
			br 0))
		local.get 3))`
	m := watToModule(t, wat)
	for _, n := range []int32{0, 1, 3, 8} {
		if got := runI32(t, m, n); got != n*n {
			t.Fatalf("n=%d: got %d want %d", n, got, n*n)
		}
	}
}

func TestControlSpillInteraction(t *testing.T) {
	// Build a deep expression (forces spills) guarded by a branch, so the spill
	// machinery and the flush-at-boundary interact. result = sum(1..16)*sel.
	var body string
	body += "(if (result i32) (local.get 0) (then\n"
	for i := 1; i <= 16; i++ {
		body += fmt.Sprintf("i32.const %d ", i) // 16 live temporaries -> spills
	}
	for i := 0; i < 15; i++ {
		body += "i32.add "
	}
	body += "\n) (else (i32.const -1)))"
	wat := `(module (func (export "f") (param i32) (result i32)` + "\n" + body + "))"
	m := watToModule(t, wat)
	// then-branch: 1+..+16 = 136 (selected when p0 != 0); else: -1.
	if got := runI32(t, m, 2); got != 136 {
		t.Fatalf("then: got %d want 136", got)
	}
	if got := runI32(t, m, 0); got != -1 {
		t.Fatalf("else: got %d want -1", got)
	}
}
