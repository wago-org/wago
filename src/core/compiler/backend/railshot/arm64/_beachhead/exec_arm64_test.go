//go:build linux && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

// TestCompileAndExec is the P2 beachhead: compile a wasm i32 function with the
// railshot arm64 backend, map it executable, and run it through the no-cgo
// trampoline under qemu — proving the full wasm→arm64→execute path.
func TestCompileAndExec(t *testing.T) {
	cases := []execCase{
		{
			// local.get 0; local.get 1; i32.add  → a0 + a1
			name: "add", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b},
			a0:   40, a1: 2, want: 42,
		},
		{
			// i32.const 1000; local.get 0; i32.add  → 1000 + a0
			name: "const-add", numParams: 1,
			body: []byte{0x00, 0x41, 0xe8, 0x07, 0x20, 0x00, 0x6a, 0x0b},
			a0:   337, want: 1337,
		},
		{
			// local.get 0; local.get 1; i32.sub  → a0 - a1
			name: "sub", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b},
			a0:   100, a1: 58, want: 42,
		},
		{
			// local.get 0; local.get 1; i32.mul  → a0 * a1
			name: "mul", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b},
			a0:   6, a1: 7, want: 42,
		},
		{
			// local.get 0; local.get 1; i32.and
			name: "and", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x71, 0x0b},
			a0:   0xf0, a1: 0x3c, want: 0x30,
		},
		{
			// local.get 0; local.get 1; i32.shl  → a0 << a1
			name: "shl", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x74, 0x0b},
			a0:   1, a1: 5, want: 32,
		},
		{
			// local.get 0; local.get 1; i32.lt_s  → a0 < a1 (signed) ? 1 : 0
			name: "lt_s_true", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x48, 0x0b},
			a0:   3, a1: 10, want: 1,
		},
		{
			name: "lt_s_false", numParams: 2,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x48, 0x0b},
			a0:   10, a1: 3, want: 0,
		},
		{
			// local.get 0; i32.eqz
			name: "eqz_zero", numParams: 1,
			body: []byte{0x00, 0x20, 0x00, 0x45, 0x0b},
			a0:   0, want: 1,
		},
		{
			name: "eqz_nonzero", numParams: 1,
			body: []byte{0x00, 0x20, 0x00, 0x45, 0x0b},
			a0:   7, want: 0,
		},
		{
			// if (a0 != 0) { 100 } else { 200 }, using i32.eqz + if/else
			// local.get 0; i32.eqz; if (void) i32.const 200; local.set 1
			//   else i32.const 100; local.set 1; end; local.get 1
			name: "if_else", numParams: 1,
			body: []byte{
				0x01, 0x01, 0x7f, // 1 local (i32) -> local 1
				0x20, 0x00, 0x45, // local.get 0; i32.eqz
				0x04, 0x40, // if void
				0x41, 0xc8, 0x01, 0x21, 0x01, // const 200; local.set 1
				0x05,                         // else
				0x41, 0xe4, 0x00, 0x21, 0x01, // const 100 (LEB e4 00); local.set 1
				0x0b,       // end if
				0x20, 0x01, // local.get 1
				0x0b, // end func
			},
			a0: 5, want: 100, // a0!=0 -> else -> 100
		},
	}
	runExec(t, cases)
}

type execCase struct {
	name      string
	numParams int
	body      []byte
	a0, a1    uintptr
	want      uintptr
}

func runExec(t *testing.T, cases []execCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := Compile(tc.numParams, tc.body)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			mem, err := arm64spike.MapExec(code)
			if err != nil {
				t.Fatalf("MapExec: %v", err)
			}
			entry := uintptr(unsafe.Pointer(&mem[0]))
			if got := arm64spike.Call2(entry, tc.a0, tc.a1); got != tc.want {
				t.Fatalf("%s(%d,%d) = %d, want %d", tc.name, tc.a0, tc.a1, got, tc.want)
			}
		})
	}
}

// TestFibExec compiles an iterative fib(n) — locals + block + loop + br_if + br —
// and runs it under qemu. This exercises the full control-flow path: a backward
// branch (br to loop header) and a forward conditional branch (br_if out of the
// block), with branch-offset patching.
func TestFibExec(t *testing.T) {
	// (func (param n i32) (result i32) (local a b i i32)
	//   a=0; b=1; i=0
	//   block (loop
	//     get i; get n; i32.ge_s; br_if $block
	//     get a; get b; i32.add; get b; set a; set b   ; (a,b) = (b, a+b)
	//     get i; i32.const 1; i32.add; set i           ; i++
	//     br $loop))
	//   get a)
	fib := []byte{
		0x01, 0x03, 0x7f, // 3 i32 locals -> a=1, b=2, i=3
		0x41, 0x00, 0x21, 0x01, // a = 0
		0x41, 0x01, 0x21, 0x02, // b = 1
		0x41, 0x00, 0x21, 0x03, // i = 0
		0x02, 0x40, // block $out
		0x03, 0x40, // loop $cont
		0x20, 0x03, 0x20, 0x00, 0x4e, 0x0d, 0x01, // get i; get n; ge_s; br_if $out
		0x20, 0x01, 0x20, 0x02, 0x6a, // get a; get b; add   -> [a+b]
		0x20, 0x02, 0x21, 0x01, // get b; set a          -> a=b, stack [a+b]
		0x21, 0x02, // set b                 -> b=a+b
		0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03, // i = i + 1
		0x0c, 0x00, // br $cont
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x01, // get a  (result)
		0x0b, // end func
	}
	runExec(t, []execCase{
		{name: "fib0", numParams: 1, body: fib, a0: 0, want: 0},
		{name: "fib1", numParams: 1, body: fib, a0: 1, want: 1},
		{name: "fib2", numParams: 1, body: fib, a0: 2, want: 1},
		{name: "fib7", numParams: 1, body: fib, a0: 7, want: 13},
		{name: "fib10", numParams: 1, body: fib, a0: 10, want: 55},
		{name: "fib20", numParams: 1, body: fib, a0: 20, want: 6765},
	})
}
