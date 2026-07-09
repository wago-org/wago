//go:build linux && arm64

package arm64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/testutil/wasmtest"
)

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

func divConstMod(t *testing.T, w64 bool, op divOp, d uint64) *wasm.Module {
	t.Helper()
	body := []byte{0x00, 0x20, 0x00}
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

func runArm64ConstDiv(t *testing.T, m *wasm.Module, arg uint64) uint64 {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	mem, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&mem[cm.InternalEntry[0]]))
	return uint64(arm64spike.Call2(entry, uintptr(arg), 0))
}

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

func TestDivByConstArm64(t *testing.T) {
	dividends := []uint64{
		0, 1, 2, 3, 7, 10, 127, 128, 255, 256, 1000,
		0x7fffffff, 0x80000000, 0xffffffff,
		0x100000000, 0x7fffffffffffffff, 0x8000000000000000,
		0xffffffffffffffff, 0xcafebabecafebabe,
	}
	posDivs := []uint64{
		2, 3, 4, 5, 7, 8, 10, 11, 16, 25, 100, 1024,
		0x7fffffff, 0x80000000, 0xffffffff,
		0x100000000, 0x7fffffffffffffff, 0xaaaaaaaaaaaaaaab,
	}
	signedDivs := []int64{
		2, -2, 3, -3, 5, -5, 7, -7, 10, -10, 100, -100,
		0x7fffffff, -0x7fffffff, 0x100000000, -0x100000000,
		0x7fffffffffffffff, -0x7fffffffffffffff,
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
				m := divConstMod(t, w64, op, d)
				for _, n := range dividends {
					want := refDivRem(op, w64, n, d)
					got := runArm64ConstDiv(t, m, n)
					if !w64 {
						want &= 0xffffffff
						got &= 0xffffffff
					}
					if got != want {
						t.Fatalf("%s n=%#x d=%#x: got %#x want %#x", op.name, n, d, got, want)
					}
				}
			}
		}
	}
}
