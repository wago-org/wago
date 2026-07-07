package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// divInvoker compiles a one-arg one-result function once and returns a closure to
// invoke it repeatedly (avoiding a recompile+mmap per test vector) plus cleanup.
func divInvoker(t *testing.T, m *wasm.Module) (call func(uint64) uint64, done func()) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	mem, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	serArgs := ar.Alloc(256)
	results := ar.Alloc(256)
	trap := ar.Alloc(8)
	call = func(arg uint64) uint64 {
		binary.LittleEndian.PutUint64(serArgs, arg)
		if err := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results); err != nil {
			t.Fatalf("call(%#x): %v", arg, err)
		}
		return binary.LittleEndian.Uint64(results)
	}
	done = func() {
		runtime.Unmap(mem)
		ar.Close()
		jm.Close()
		eng.Close()
	}
	return
}

// divOp is one wasm division/remainder opcode for a given width.
type divOp struct {
	name   string
	opcode byte
	signed bool
	rem    bool
}

func divOps(w64 bool) []divOp {
	if w64 {
		return []divOp{
			{"i64.div_s", 0x7f, true, false},
			{"i64.div_u", 0x80, false, false},
			{"i64.rem_s", 0x81, true, true},
			{"i64.rem_u", 0x82, false, true},
		}
	}
	return []divOp{
		{"i32.div_s", 0x6d, true, false},
		{"i32.div_u", 0x6e, false, false},
		{"i32.rem_s", 0x6f, true, true},
		{"i32.rem_u", 0x70, false, true},
	}
}

// divConstMod builds (param T)(result T){ local.get 0; T.const d; op } for the
// given width and divisor bit pattern.
func divConstMod(t *testing.T, w64 bool, op divOp, d uint64) *wasm.Module {
	body := []byte{0x00, 0x20, 0x00} // 0 locals; local.get 0
	if w64 {
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(int64(d))...)
	} else {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(uint32(d)))...)
	}
	body = append(body, op.opcode, 0x0b)
	vt := wasm.I32
	if w64 {
		vt = wasm.I64
	}
	return mod1(t, []wasm.ValType{vt}, []wasm.ValType{vt}, body)
}

// refDivRem is the reference (wasm-semantics) result of op(n, d).
func refDivRem(op divOp, w64 bool, n, d uint64) uint64 {
	if w64 {
		if op.signed {
			a, b := int64(n), int64(d)
			if op.rem {
				return uint64(a % b)
			}
			return uint64(a / b)
		}
		if op.rem {
			return n % d
		}
		return n / d
	}
	if op.signed {
		a, b := int32(uint32(n)), int32(uint32(d))
		if op.rem {
			return uint64(uint32(a % b))
		}
		return uint64(uint32(a / b))
	}
	a, b := uint32(n), uint32(d)
	if op.rem {
		return uint64(a % b)
	}
	return uint64(a / b)
}

// TestDivByConstFallback covers the cases that stay on the idiv path: a constant
// zero divisor (must trap) and signed ±1 (x/-1 == -x with an INT_MIN/-1 trap,
// x%±1 == 0). Strength reduction must not swallow these.
func TestDivByConstFallback(t *testing.T) {
	// div by constant 0 traps.
	for _, w64 := range []bool{false, true} {
		for _, op := range divOps(w64) {
			if _, _, err := runMemAmd64(t, modMem(t, 1, funcParams(w64), funcParams(w64), divBody(w64, op, 0)), nil, 5); err == nil {
				t.Errorf("%s by const 0 did not trap", op.name)
			}
		}
	}
	// signed div by -1: x/-1 == -x, and INT_MIN/-1 traps; rem by ±1 == 0.
	i32 := wasm.I32
	m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, divBody(false, divOp{"", 0x6d, true, false}, uint64(^uint32(0))))
	call, done := divInvoker(t, m)
	if got := int32(uint32(call(7))); got != -7 {
		t.Errorf("i32.div_s 7/-1 = %d, want -7", got)
	}
	done()
	// INT_MIN / -1 must trap.
	if _, _, err := runMemAmd64(t, modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, divBody(false, divOp{"", 0x6d, true, false}, uint64(^uint32(0)))), nil, 0x80000000); err == nil {
		t.Errorf("i32.div_s INT_MIN/-1 did not trap")
	}
}

func funcParams(w64 bool) []wasm.ValType {
	if w64 {
		return []wasm.ValType{wasm.I64}
	}
	return []wasm.ValType{wasm.I32}
}

// divBody is divConstMod's body without wrapping it in a module.
func divBody(w64 bool, op divOp, d uint64) []byte {
	body := []byte{0x00, 0x20, 0x00}
	if w64 {
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(int64(d))...)
	} else {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(uint32(d)))...)
	}
	return append(body, op.opcode, 0x0b)
}

// TestDivByConstExhaustive checks constant-divisor strength reduction (power-of-2
// and magic multiply, signed and unsigned, i32 and i64, div and rem) against the
// idiv reference for a broad set of divisor × dividend pairs.
func TestDivByConstExhaustive(t *testing.T) {
	// Dividends: sign boundaries, small magnitudes, and assorted bit patterns.
	dividends := []uint64{
		0, 1, 2, 3, 6, 7, 10, 99, 100, 127, 128, 255, 256, 1000, 65535, 65536,
		0x7fffffff, 0x80000000, 0x80000001, 0xfffffffe, 0xffffffff,
		0x100000000, 0x123456789a, 0x7fffffffffffffff, 0x8000000000000000,
		0x8000000000000001, 0xfffffffffffffffe, 0xffffffffffffffff,
		0xdeadbeef, 0xcafebabecafebabe, 12345678901234567,
	}
	// Divisors covering: powers of two (+/-), the add-variant triggers (3,7,...),
	// small odds/evens, large values, and high-bit unsigned divisors.
	posDivs := []uint64{
		2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 15, 16, 25, 100, 1000, 1024,
		0x10000, 0x7fffffff, 0x80000000, 0xffffffff, 3000000000,
		0x100000000, 0x123456789, 0x7fffffffffffffff, 0x8000000000000000,
		0xffffffffffffffff, 0xaaaaaaaaaaaaaaab,
	}
	// Signed divisors: |d| >= 2 (±1 use the idiv path), both signs.
	signedDivs := []int64{
		2, -2, 3, -3, 4, -4, 5, -5, 7, -7, 8, -8, 10, -10, 100, -100,
		1024, -1024, 0x7fffffff, -0x7fffffff, 1000000, -1000000,
		0x100000000, -0x100000000, 0x7fffffffffffffff, -0x7fffffffffffffff,
		-0x8000000000000000,
	}

	for _, w64 := range []bool{false, true} {
		for _, op := range divOps(w64) {
			divs := posDivs
			if op.signed {
				divs = nil
				for _, d := range signedDivs {
					divs = append(divs, uint64(d))
				}
			}
			for _, d := range divs {
				// Skip divisors out of range for the narrower width.
				if !w64 {
					if op.signed {
						sd := int64(int32(uint32(d)))
						if uint64(sd) != d {
							continue
						}
					} else if d > 0xffffffff {
						continue
					}
				}
				call, done := divInvoker(t, divConstMod(t, w64, op, d))
				for _, n := range dividends {
					want := refDivRem(op, w64, n, d)
					got := call(n)
					if !w64 {
						want &= 0xffffffff
						got &= 0xffffffff
					}
					if got != want {
						t.Errorf("%s n=%#x d=%#x: got %#x want %#x", op.name, n, d, got, want)
					}
				}
				done()
			}
		}
	}
}
