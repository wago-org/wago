//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/big"
	"testing"
)

func TestIntegerSumLoopLowering(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		typ, c, add, lt := wasm.I32, byte(0x41), byte(0x6a), byte(0x49)
		if wide {
			typ, c, add, lt = wasm.I64, 0x42, 0x7c, 0x54
		}
		for _, fenced := range []bool{false, true} {
			valueType := byte(0x7f)
			if wide {
				valueType = 0x7e
			}
			body := []byte{1, 1, valueType, 0x03, 0x40, 0x20, 1}
			if fenced {
				body = append(body, 0x02, valueType, 0xfe, 3, 0)
			}
			body = append(body, 0x20, 2)
			if fenced {
				body = append(body, 0x0b)
			}
			body = append(body, add, 0x21, 1, 0x20, 2, c, 1, add, 0x21, 2, 0x20, 2, 0x20, 0, lt, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b)
			m := modFuncs(t, funcDef{params: []wasm.ValType{typ, typ}, results: []wasm.ValType{typ, typ}, body: body})
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			if fenced {
				fence := []byte{0xbf, 0x3b, 0x03, 0xd5}
				if count := bytes.Count(cm.Code, fence); count != 1 {
					t.Fatalf("retained fences=%d, want 1", count)
				}
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["integer-sum-loop"]; got != 1 {
				t.Fatalf("wide=%t fenced=%t: reductions=%d, want 1", wide, fenced, got)
			}
			bounds := []uint64{0, 1, 2, 3, 65535, 65536, 1 << 31, (1 << 32) - 1}
			if wide {
				bounds = append(bounds, 1<<63, (1<<63)+1, ^uint64(0))
			}
			for _, n := range bounds {
				acc := uint64(0xfedcba9876543210)
				a := new(big.Int).SetUint64(n)
				b := new(big.Int).Sub(new(big.Int).Set(a), big.NewInt(1))
				want := new(big.Int).Rsh(new(big.Int).Mul(a, b), 1).Uint64() + acc
				got := func() uint64 {
					v, err := runArm64Wrapper(t, m, n, acc)
					if err != nil {
						t.Fatal(err)
					}
					return v
				}()
				if !wide {
					want = uint64(uint32(want))
				}
				if got != want {
					t.Fatalf("wide=%t fenced=%t n=%x: sum=%x want=%x", wide, fenced, n, got, want)
				}
			}
			var disabled ModuleStats
			off, err := CompileModuleWith(m, CompileOptions{Stats: &disabled, Optimizations: map[string]bool{"integer-sum-loop": false}})
			if err != nil {
				t.Fatal(err)
			}
			off.CodeImage.Close()
			if disabled.Funcs[0].Peephole["integer-sum-loop"] != 0 {
				t.Fatal("disabled reduction emitted")
			}
			var polls ModuleStats
			interruptible, err := CompileModuleWith(m, CompileOptions{Stats: &polls, Interruptible: true})
			if err != nil {
				t.Fatal(err)
			}
			interruptible.CodeImage.Close()
			if polls.Funcs[0].Peephole["integer-sum-loop"] != 0 {
				t.Fatal("interruptible reduction emitted")
			}

		}
	}
}

func TestIntegerSumLoopRequiresUntouchedDeclaredCounter(t *testing.T) {
	requireCompilerDiagnostics(t)
	loop := []byte{0x03, 0x40, 0x20, 1, 0x20, 2, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x20, 2, 0x0b}
	for _, parameter := range []bool{false, true} {
		body := []byte{1, 1, 0x7f, 0x41, 7, 0x21, 2}
		params := []wasm.ValType{wasm.I32, wasm.I32}
		if parameter {
			body = []byte{0}
			params = append(params, wasm.I32)
		}
		body = append(body, loop...)
		m := modFuncs(t, funcDef{params: params, results: []wasm.ValType{wasm.I32, wasm.I32}, body: body})
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if stats.Funcs[0].Peephole["integer-sum-loop"] != 0 {
			t.Fatalf("parameter=%t: assumed nonzero counter starts at zero", parameter)
		}
	}
}
