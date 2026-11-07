//go:build (linux || darwin) && arm64 && !tinygo

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func tailSlotModuleARM64(tb testing.TB, prefix, nargs int, wide bool) *wasm.Module {
	tb.Helper()
	params := make([]wasm.ValType, nargs)
	for i := range params {
		params[i] = wasm.I64
	}
	body := []byte{0}
	if wide {
		body = append(body, simdConst(i64x2Bytes(0x1234, 0x5678))...)
	}
	for i := 0; i < prefix; i++ {
		body = append(body, 0x41, 1)
	}
	for i := range params {
		body = append(body, 0x42, byte(i+1))
	}
	body = append(body, 0x12, 1, 0x0b)
	m := modFuncs(tb, funcDef{results: []wasm.ValType{wasm.I64}, body: body}, funcDef{params: params, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, byte(nargs - 1), 0x0b}})
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func TestTailArgumentSlotsMixedPrefixARM64(t *testing.T) {
	for _, regABI := range []bool{false, true} {
		for _, nargs := range []int{1, 8, 16} {
			t.Run(fmt.Sprintf("regABI=%t/args=%d", regABI, nargs), func(t *testing.T) {
				m := tailSlotModuleARM64(t, 8, nargs, true)
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Workers: 1, Optimizations: map[string]bool{"reg-abi": regABI, "inline": false}})
				if err != nil {
					t.Fatal(err)
				}
				if got != uint64(nargs) {
					t.Fatalf("result=%d, want %d", got, nargs)
				}
			})
		}
	}
}

func BenchmarkCompileTailArgumentPrefixARM64(b *testing.B) {
	for _, prefix := range []int{0, 32, 8192} {
		for _, regABI := range []bool{false, true} {
			b.Run(fmt.Sprintf("prefix=%d/regABI=%t", prefix, regABI), func(b *testing.B) {
				m := tailSlotModuleARM64(b, prefix, 8, false)
				opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"reg-abi": regABI, "inline": false}}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					if cm.CodeImage != nil {
						if err := cm.CodeImage.Close(); err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
				}
			})
		}
	}
}

var tailSlotSinkARM64 int

func BenchmarkTailArgumentSlotPlanningARM64(b *testing.B) {
	for _, prefix := range []int{0, 32, 8192} {
		b.Run(fmt.Sprintf("prefix=%d", prefix), func(b *testing.B) {
			nodes := make([]elem, prefix+16)
			roots := make([]*elem, len(nodes))
			for i := range nodes {
				typ := mtI64
				if i < prefix && i%3 == 0 {
					typ = mtV128
				}
				nodes[i].st = storage{kind: stConst, typ: typ}
				roots[i] = &nodes[i]
			}
			b.ReportAllocs()
			for _, legacy := range []bool{true, false} {
				b.Run(fmt.Sprintf("legacy=%t", legacy), func(b *testing.B) {
					b.ReportAllocs()
					for range b.N {
						sum := 0
						if legacy {
							types := make([]machineType, len(roots))
							for i, root := range roots {
								types[i] = rootMachineType(root)
							}
							for i := 0; i < 16; i++ {
								sum += slotOfLogicalTypes(types, prefix+i)
							}
						} else {
							slot := tailArgumentStartSlot(roots, 16)
							for i := 0; i < 16; i++ {
								sum += slot
								slot += mtI64.stackSlots()
							}
						}
						tailSlotSinkARM64 = sum
					}
				})
			}
		})
	}
}

func TestTailArgumentStartSlotMixedWidthsARM64(t *testing.T) {
	types := []machineType{mtV128, mtI32, mtF64, mtV128, mtF32, mtI64}
	nodes := make([]elem, len(types))
	roots := make([]*elem, len(types))
	for i, typ := range types {
		nodes[i].st = storage{kind: stConst, typ: typ}
		roots[i] = &nodes[i]
	}
	for params := 0; params <= len(types); params++ {
		want := slotOfLogicalTypes(types, len(types)-params)
		if got := tailArgumentStartSlot(roots, params); got != want {
			t.Fatalf("params=%d: start=%d, want %d", params, got, want)
		}
	}
	if got := tailArgumentStartSlot(nil, 0); got != 0 {
		t.Fatalf("empty start=%d", got)
	}
}

func TestTailArgumentSlotsMixedArgumentsARM64(t *testing.T) {
	// Check both halves of each v128 and the scalar between them.
	body := append([]byte{0}, simdConst(i64x2Bytes(99, 100))...)
	body = append(body, 0x41, 1, 0x42, 1)
	body = append(body, simdConst(i64x2Bytes(2, 3))...)
	body = append(body, 0x42, 4)
	body = append(body, simdConst(i64x2Bytes(5, 6))...)
	body = append(body, 0x12, 1, 0x0b)
	callee := []byte{0, 0x20, 0}
	for _, local := range []byte{1, 2, 3} {
		if local == 2 {
			callee = append(callee, 0x20, local, 0x7c)
			continue
		}
		for lane := byte(0); lane < 2; lane++ {
			callee = append(callee, 0x20, local, 0xfd, 0x1d, lane, 0x7c)
		}
	}
	callee = append(callee, 0x0b)
	m := modFuncs(t, funcDef{results: []wasm.ValType{wasm.I64}, body: body}, funcDef{params: []wasm.ValType{wasm.I64, wasm.V128, wasm.I64, wasm.V128}, results: []wasm.ValType{wasm.I64}, body: callee})
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, regABI := range []bool{false, true} {
		t.Run(fmt.Sprintf("regABI=%t", regABI), func(t *testing.T) {
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Workers: 1, Optimizations: map[string]bool{"reg-abi": regABI, "inline": false}})
			if err != nil {
				t.Fatal(err)
			}
			if got != 21 {
				t.Fatalf("result=%d, want 21", got)
			}
		})
	}
}
