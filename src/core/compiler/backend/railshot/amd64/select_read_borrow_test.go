//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSelectReadBorrowPreservesSource(t *testing.T) {
	saved := selectReadBorrowEnabled
	defer func() { selectReadBorrowEnabled = saved }()
	for _, wide := range []bool{false, true} {
		ty, div, shift, less, xor := wasm.I32, byte(0x6e), byte(0x74), byte(0x49), byte(0x73)
		mask := uint64(0xffffffff)
		shiftMask := uint64(31)
		if wide {
			ty, div, shift, less, xor = wasm.I64, 0x80, 0x86, 0x54, 0x85
			mask, shiftMask = ^uint64(0), 63
		}
		for _, predicate := range []string{"plain", "div", "shift"} {
			t.Run(fmt.Sprintf("wide=%v/%s", wide, predicate), func(t *testing.T) {
				body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2}
				if predicate != "plain" {
					body = append(body, 0x20, 3)
					if predicate == "div" {
						body = append(body, div)
					} else {
						body = append(body, shift)
					}
				}
				// The predicate also reads the borrowed alternative. Read it again
				// after CMOV to check that lowering preserves its original value.
				body = append(body, 0x20, 1, less, 0x1b, 0x20, 1, xor, 0x0b)
				m := mod1(t, []wasm.ValType{ty, ty, ty, ty}, []wasm.ValType{ty}, body)
				for _, on := range []bool{false, true} {
					selectReadBorrowEnabled = on
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-abi": true}})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if diagnosticsEnabled {
						if (stats.Funcs[0].Peephole["select-read-borrow"] > 0) != on {
							t.Fatalf("on=%v admission=%v", on, stats.Funcs[0].Peephole)
						}
					}
					for _, args := range [][4]uint64{{123, 17, 2, 3}, {123, 17, 100, 3}, {0, 0, 0, 1}, {0xfedcba9876543210, 0x80000000, 0xffffffff, 33}, {17, 0xffffffffffffffff, 0xfffffffffffffffe, 63}} {
						for i := range args {
							args[i] &= mask
						}
						value := args[2]
						if predicate == "div" {
							value /= args[3]
						} else if predicate == "shift" {
							value = (value << (args[3] & shiftMask)) & mask
						}
						want := uint64(0)
						if value < args[1] {
							want = args[0] ^ args[1]
						}
						if got := runCompiledAmd64u(t, cm, args[:]...); got != want {
							t.Fatalf("on=%v args=%x got=%x want=%x", on, args, got, want)
						}
					}
				}
			})
		}
	}
}

func TestSelectReadBorrowSurvivesPredicatePressure(t *testing.T) {
	saved := selectReadBorrowEnabled
	defer func() { selectReadBorrowEnabled = saved }()
	const depth = 24
	for _, width := range []int{32, 64} {
		ty, constant, xor, div, shift, less := wasm.I32, byte(0x41), byte(0x73), byte(0x6e), byte(0x74), byte(0x49)
		if width == 64 {
			ty, constant, xor, div, shift, less = wasm.I64, 0x42, 0x85, 0x80, 0x86, 0x54
		}
		body := []byte{0, 0x20, 0, 0x20, 1}
		for level := 0; level < depth; level++ {
			body = append(body, 0x20, 2, constant, byte(level+1), xor, 0x20, 3, div)
		}
		body = append(body, 0x20, 4)
		for level := depth - 1; level >= 0; level-- {
			body = append(body, shift+byte(level%5), 0x20, 5, xor)
		}
		body = append(body, 0x20, 1, less, 0x1b, 0x20, 1, xor, 0x0b)
		m := mod1(t, []wasm.ValType{ty, ty, ty, ty, ty, ty}, []wasm.ValType{ty}, body)
		for _, on := range []bool{false, true} {
			selectReadBorrowEnabled = on
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled {
				if stats.Funcs[0].Spills == 0 || stats.Funcs[0].Reloads == 0 {
					t.Fatal("fixture did not exercise spilling")
				}
			}
			if diagnosticsEnabled {
				if (stats.Funcs[0].Peephole["select-read-borrow"] > 0) != on {
					t.Fatalf("width=%d on=%v admission=%v", width, on, stats.Funcs[0].Peephole)
				}
			}
			for _, b := range []uint64{0, ^uint64(0)} {
				args := []uint64{123, b, 0x8123456789abcdef, 3, 31, 0x63d27a19}
				if width == 32 {
					for i := range args {
						args[i] = uint64(uint32(args[i]))
					}
				}
				value := args[4]
				for level := depth - 1; level >= 0; level-- {
					left := divideResult(width, 1, args[2]^uint64(level+1), args[3])
					value = shiftResult(width, level%5, left, value) ^ args[5]
				}
				want := uint64(0)
				if value < args[1] {
					want = args[0] ^ args[1]
				}
				if got := runCompiledAmd64u(t, cm, args...); got != want {
					t.Fatalf("width=%d on=%v got=%x want=%x", width, on, got, want)
				}
			}
		}
	}
}
