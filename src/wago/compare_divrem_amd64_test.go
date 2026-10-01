//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func compareDivRemModule(typ wasm.ValType, left, right, compare byte, branch bool) []byte {
	body := []byte{
		0x20, 0, 0x20, 1, left, // left numerator / or % left divisor
		0x20, 2, 0x20, 3, right, // right numerator / or % right divisor
		compare,
	}
	if branch {
		body = append(body, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b) // if (result i32) 1 else 0
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(body, 0x0b)))),
	)
}

func compileCompareDivRem(t testing.TB, module []byte) (*Compiled, *Instance) {
	t.Helper()
	compiled, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	return compiled, instance
}

func TestCompareDivRemPreservesLeftAMD64(t *testing.T) {
	for _, width := range []struct {
		name    string
		typ     wasm.ValType
		div, eq byte
		mask    uint64
	}{
		{"i32", wasm.I32, 0x6d, 0x46, 0xffffffff},
		{"i64", wasm.I64, 0x7f, 0x51, ^uint64(0)},
	} {
		for _, tc := range []struct {
			name        string
			left, right byte // offset from div_s: div_s, div_u, rem_s, rem_u
			args        [4]int64
			x, y        int64
		}{
			{"div_u-div_u", 1, 1, [4]int64{6, 2, 6, 3}, 3, 2},
			{"div_u-rem_u", 1, 3, [4]int64{10, 20, 10, 3}, 0, 1},
			{"rem_u-div_u", 3, 1, [4]int64{10, 3, 10, 2}, 1, 5},
			{"rem_u-rem_u", 3, 3, [4]int64{10, 6, 10, 3}, 4, 1},
			{"div_s-div_s", 0, 0, [4]int64{-6, 2, 6, 3}, -3, 2},
			{"div_s-rem_s", 0, 2, [4]int64{-10, 20, -10, 3}, 0, -1},
			{"rem_s-div_s", 2, 0, [4]int64{-10, 3, 10, 2}, -1, 5},
			{"rem_s-rem_s", 2, 2, [4]int64{-10, 6, -10, 3}, -4, -1},
			{"equal", 1, 1, [4]int64{6, 2, 9, 3}, 3, 3},
		} {
			x, y := uint64(tc.x)&width.mask, uint64(tc.y)&width.mask
			wants := []bool{tc.x == tc.y, tc.x != tc.y, tc.x < tc.y, x < y, tc.x > tc.y, x > y, tc.x <= tc.y, x <= y, tc.x >= tc.y, x >= y}
			for cmp, name := range []string{"eq", "ne", "lt_s", "lt_u", "gt_s", "gt_u", "le_s", "le_u", "ge_s", "ge_u"} {
				for _, branch := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/branch=%v", width.name, tc.name, name, branch), func(t *testing.T) {
						_, instance := compileCompareDivRem(t, compareDivRemModule(width.typ, width.div+tc.left, width.div+tc.right, width.eq+byte(cmp), branch))
						got, err := instance.Invoke("run", uint64(tc.args[0])&width.mask, uint64(tc.args[1])&width.mask, uint64(tc.args[2])&width.mask, uint64(tc.args[3])&width.mask)
						var want uint64
						if wants[cmp] {
							want = 1
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("result = %v, %v; want %d (%d %s %d)", got, err, want, tc.x, name, tc.y)
						}
					})
				}
			}
		}
	}
}

func TestCompareDivRemTrapOrderAMD64(t *testing.T) {
	for _, width := range []struct {
		name          string
		typ           wasm.ValType
		div, eq       byte
		min, minusOne uint64
	}{
		{"i32", wasm.I32, 0x6d, 0x46, 1 << 31, 0xffffffff},
		{"i64", wasm.I64, 0x7f, 0x51, 1 << 63, ^uint64(0)},
	} {
		for _, branch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/branch=%v", width.name, branch), func(t *testing.T) {
				_, instance := compileCompareDivRem(t, compareDivRemModule(width.typ, width.div, width.div, width.eq, branch))
				for _, tc := range []struct {
					args []uint64
					want TrapCode
				}{
					{[]uint64{width.min, width.minusOne, 1, 0}, TrapDivOverflow},
					{[]uint64{1, 0, width.min, width.minusOne}, TrapDivZero},
				} {
					_, err := instance.Invoke("run", tc.args...)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != tc.want {
						t.Fatalf("Invoke(%v) = %v; want trap %v", tc.args, err, tc.want)
					}
				}
			})
		}
	}
}
