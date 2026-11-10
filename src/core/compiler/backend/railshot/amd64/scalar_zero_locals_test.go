//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSharedScalarZeroLocalCodeAndExecution(t *testing.T) {
	if !sharedScalarEnabled {
		t.Skip("shared scalar disabled")
	}
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		for _, locals := range []int{1, 8, 64, 256} {
			body := append([]byte{1}, wasmtest.ULEB(uint32(locals))...)
			body = append(body, wasm.MustEncodeValType(typ), 0x20)
			body = append(body, wasmtest.ULEB(uint32(locals-1))...)
			body = append(body, 0x0b)
			m := modFuncs(t, funcDef{results: []wasm.ValType{typ}, body: body})
			for _, compact := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, Workers: 1, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-abi": regABI}})
					if err != nil {
						t.Fatal(err)
					}
					got := runCompiledAmd64u(t, cm)
					size := len(cm.Code)
					if err := cm.CodeImage.Close(); err != nil {
						t.Fatal(err)
					}
					if got != 0 {
						t.Fatalf("type=%v locals=%d compact=%v regABI=%v got=%x", typ, locals, compact, regABI, got)
					}
					if size > 80 {
						t.Fatalf("type=%v locals=%d compact=%v regABI=%v: zero-local leaf emits %d bytes, budget 80", typ, locals, compact, regABI, size)
					}
					if diagnosticsEnabled && !stats.Funcs[0].SharedScalar {
						t.Fatal("zero-local fixture did not use shared path")
					}
				}
			}
		}
	}
}

func TestSharedScalarZeroLocalAgreements(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, result := wasm.I32, byte(0x41), byte(0x6a), byte(0x7f)
		if wide {
			typ, constant, add, result = wasm.I64, 0x42, 0x7c, 0x7e
		}
		cases := []struct {
			body          []byte
			zero, nonzero uint64
		}{
			{[]byte{0x20, 0, 0x04, result, 0x20, 1, 0x05, constant, 5, 0x21, 1, 0x20, 1, 0x0b, 0x20, 1, add, 0x0b}, 10, 0},
			{[]byte{0x20, 1, 0x20, 0, 0x04, result, constant, 7, 0x22, 1, 0x05, 0x20, 1, 0x0b, add, 0x20, 1, add, 0x0b}, 0, 14},
		}
		for _, tc := range cases {
			body := append([]byte{1, 1, wasm.MustEncodeValType(typ)}, tc.body...)
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{typ}, body: body})
			for _, compact := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					cm, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, Workers: 1, Optimizations: map[string]bool{"reg-abi": regABI}})
					if err != nil {
						t.Fatal(err)
					}
					for _, condition := range []uint64{0, 1, 0xffffffff} {
						want := tc.nonzero
						if condition == 0 {
							want = tc.zero
						}
						if got := runCompiledAmd64u(t, cm, condition); got != want {
							t.Fatalf("wide=%v compact=%v regABI=%v cond=%x got=%x want=%x", wide, compact, regABI, condition, got, want)
						}
					}
					if err := cm.CodeImage.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}
