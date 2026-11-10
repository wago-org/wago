//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestFoldedNegativeI32Compare(t *testing.T) {
	for op := byte(0x46); op <= 0x4f; op++ {
		// Constant folding produces the unsigned representation of i32 -1.
		// memory.size/drop bypasses the shared scalar instruction selector.
		body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x41, 0, 0x41, 1, 0x6b, op, 0x0b}
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		cm, err := CompileModuleWith(m, CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i := 0; i+4 <= len(cm.Code); i += 4 {
			word := binary.LittleEndian.Uint32(cm.Code[i:])
			if word&0xffc0001f == 0x3100001f && (word>>10)&0xfff == 1 {
				found = true
			}
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if !found {
			t.Fatalf("op=%x: no CMN immediate for folded -1", op)
		}
		for _, x := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff} {
			want := false
			switch op {
			case 0x46:
				want = x == 0xffffffff
			case 0x47:
				want = x != 0xffffffff
			case 0x48:
				want = int32(x) < -1
			case 0x49:
				want = x < 0xffffffff
			case 0x4a:
				want = int32(x) > -1
			case 0x4b:
				want = x > 0xffffffff
			case 0x4c:
				want = int32(x) <= -1
			case 0x4d:
				want = x <= 0xffffffff
			case 0x4e:
				want = int32(x) >= -1
			case 0x4f:
				want = x >= 0xffffffff
			}
			got := runArm64u(t, m, uint64(x))
			if (got != 0) != want {
				t.Fatalf("op=%x x=%x got=%d want=%v", op, x, got, want)
			}
		}
	}
}

func TestI32ConstantLocalNeedsNoCanonicalization(t *testing.T) {
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x20, 0, 0x04, 0x40,
		0x41, 0x7f, 0x21, 1, 0x05, 0x41, 0, 0x21, 1, 0x0b, 0x20, 1, 0xad, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if diagnosticsEnabled {
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if stats.Funcs[0].Peephole["local-i32-canonicalize"] != 0 {
			t.Fatalf("constant assignments required redundant canonicalization: %v", stats.Funcs[0].Peephole)
		}
	}
	for _, condition := range []uint64{0, 1, 0x80000000} {
		want := uint64(0)
		if condition != 0 {
			want = 0xffffffff
		}
		if got := runArm64u(t, m, condition); got != want {
			t.Fatalf("condition=%x got=%x want=%x", condition, got, want)
		}
	}
}

func TestConstantBranchComparisons(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, firstCompare := byte(0x41), byte(0x46)
		if typ == wasm.I64 {
			constant, firstCompare = 0x42, 0x51
		}
		for _, c := range []int64{-1, -4095, -4096, -4097, 4096, 0x123000} {
			for index := byte(0); index < 10; index++ {
				body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, constant}
				if typ == wasm.I32 {
					body = append(body, wasmtest.SLEB32(int32(c))...)
				} else {
					body = append(body, wasmtest.SLEB64(c)...)
				}
				body = append(body, firstCompare+index, 0x04, 0x7f, 0x41, 11, 0x05, 0x41, 23, 0x0b, 0x0b)
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
				for _, x := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0x7fffffffffffffff, 0x8000000000000000, 0xfffff000, ^uint64(0)} {
					a, b := x, uint64(c)
					sa, sb := int64(a), int64(b)
					if typ == wasm.I32 {
						a, b = uint64(uint32(a)), uint64(uint32(b))
						sa, sb = int64(int32(a)), int64(int32(b))
					}
					condition := false
					switch index {
					case 0:
						condition = a == b
					case 1:
						condition = a != b
					case 2:
						condition = sa < sb
					case 3:
						condition = a < b
					case 4:
						condition = sa > sb
					case 5:
						condition = a > b
					case 6:
						condition = sa <= sb
					case 7:
						condition = a <= b
					case 8:
						condition = sa >= sb
					case 9:
						condition = a >= b
					}
					want := uint64(23)
					if condition {
						want = 11
					}
					if got := runArm64u(t, m, x); got != want {
						t.Fatalf("type=%v index=%d x=%x c=%d got=%d want=%d", typ, index, x, c, got, want)
					}
				}
			}
		}
	}
}
