//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/wasmtest"
)

// TestBorrowedLocalSurvivesDeferredCompareLoad is the minimized LZ4 regression.
// The outer add keeps local 0 live while the equality comparison consumes a
// deferred load whose effective address borrows local 0's pinned register. The
// compare must neither load into nor release that borrowed address register.
func TestBorrowedLocalSurvivesDeferredCompareLoad(t *testing.T) {
	body := []byte{0x01, 0x01, 0x7f} // one declared i32 local
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body,
		0x21, 0x00, // local.set 0
		0x41,
	)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(5)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[100] = 5
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(5)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[200] = 5
		0x20, 0x00, // local.get 0: preserved outer add operand
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body,
		0x2d, 0x00, 0x00, // i32.load8_u mem[200]
		0x20, 0x00,
		0x2d, 0x00, 0x00, // i32.load8_u mem[local 0], borrowing local 0's register
		0x46, // i32.eq
		0x6a, // i32.add: 100 + 1
		0x0b,
	)
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)

	for _, tc := range []struct {
		name string
		opts CompileOptions
	}{
		{name: "explicit"},
		{name: "guard", opts: CompileOptions{ElideBoundsChecks: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := runMemAmd64WithOptions(t, m, tc.opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != 101 {
				t.Fatalf("borrowed local after deferred comparison = %d, want 101", got)
			}
		})
	}
}

func TestApplyMulMemRefOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		borrow int
		want   bool
	}{
		{name: "borrowed/foldable", size: 4, borrow: 0, want: true},
		{name: "borrowed/materialized", size: 1, borrow: 0, want: true},
		{name: "owned/foldable", size: 4, borrow: -1},
		{name: "owned/materialized", size: 1, borrow: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// regUser is a canary for release: borrowed address ownership must be
			// untouched, while an allocator-owned address must become available.
			owner := &elem{}
			f := fn{a: &x86.Asm{}}
			f.regUser[R12] = owner
			if tc.borrow >= 0 {
				f.pinnedLocalMask = maskOf(R12)
			}
			right := &elem{kind: ekValue, st: memRefStorage(R12, 0, tc.size, false, false, tc.borrow)}

			f.applyMul(RAX, right, false)

			if got := f.regUser[R12] == owner; got != tc.want {
				t.Fatalf("address-register ownership retained = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBorrowedLocalSurvivesDeferredMultiplyLoad keeps local 0 live as an outer
// add operand while i32.mul consumes a deferred load addressed through that
// local's pinned register. Multiplication must not release the borrowed address
// register before the outer add reads the local again.
func TestBorrowedLocalSurvivesDeferredMultiplyLoad(t *testing.T) {
	body := []byte{0x01, 0x01, 0x7f} // one declared i32 local
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body,
		0x21, 0x00, // local.set 0
		0x41,
	)
	body = append(body, wasmtest.SLEB32(100)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(7)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[100] = 7
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(3)...)
	body = append(body,
		0x3a, 0x00, 0x00, // i32.store8 mem[200] = 3
		0x20, 0x00, // local.get 0: preserved outer add operand
		0x41,
	)
	body = append(body, wasmtest.SLEB32(200)...)
	body = append(body,
		0x2d, 0x00, 0x00, // i32.load8_u mem[200]
		0x20, 0x00,
		0x2d, 0x00, 0x00, // i32.load8_u mem[local 0], borrowing local 0's register
		0x6c, // i32.mul: 3 * 7
		0x6a, // i32.add: 100 + 21
		0x0b,
	)
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)

	for _, tc := range []struct {
		name string
		opts CompileOptions
	}{
		{name: "explicit"},
		{name: "guard", opts: CompileOptions{ElideBoundsChecks: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := runMemAmd64WithOptions(t, m, tc.opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != 121 {
				t.Fatalf("borrowed local after deferred multiplication = %d, want 121", got)
			}
		})
	}
}
