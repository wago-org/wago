//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func selfTailPressureModule() []byte {
	body := []byte{1, 4, 0x7f}
	for i := byte(4); i < 8; i++ {
		body = append(body, 0x20, 1, 0x21, i)
	}
	body = append(body, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x20, 2, 0x41, 3, 0x6c, 0x6a, 0x20, 3, 0x41, 5, 0x6c, 0x6a, 0x05, 0x20, 0, 0x41, 1, 0x6b)
	var tree func(int, int)
	tree = func(depth, leaf int) {
		if depth == 0 {
			body = append(body, 0x20, byte(4+leaf%4))
			return
		}
		tree(depth-1, leaf*2)
		tree(depth-1, leaf*2+1)
		body = append(body, 0x6b)
	}
	tree(6, 0)
	body = append(body, 0x41, 13, 0x41, 17, 0x12, 0, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestPublicSelfTailLoopPreservesParameterUnderPressure(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(workers).WithOptimization("tail-self-loop", enabled).Compile(selfTailPressureModule())
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 31} {
				got, err := in.Invoke("run", n|0xdeadbeef00000000, 7, 11, 19)
				want := uint64(124)
				if n == 0 {
					want = 135
				}
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("enabled=%t workers=%d n=%d got=%v err=%v want=%d", enabled, workers, n, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func nonReturningTailPrefixModule(kind string) []byte {
	helper := []byte{}
	switch kind {
	case "countdown":
		helper = append(helper, 0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b, 0x20, 0, 0x41, 1, 0x6b)
	case "immediate-trap":
		helper = append(helper, 0x00, 0x20, 0)
	case "self-tail":
		helper = append(helper, 0x20, 0)
	}
	helper = append(helper, 0x12, 1, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 5, 0x20, 0, 0x10, 1, 0x6a, 0x0b}), wasmtest.Code(helper))))
}

func TestPublicTailInlineNonReturningHelperWithPrefix(t *testing.T) {
	for _, kind := range []string{"countdown", "immediate-trap", "self-tail"} {
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(workers).WithOptimization("tail-loop-inline", enabled).Compile(nonReturningTailPrefixModule(kind))
				if err != nil {
					t.Fatalf("kind=%s enabled=%t workers=%d compile: %v", kind, enabled, workers, err)
				}
				if kind != "self-tail" {
					in, err := Instantiate(c)
					if err != nil {
						c.Close()
						t.Fatal(err)
					}
					_, err = in.Invoke("run", 2)
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 1 {
						t.Fatalf("kind=%s enabled=%t err=%v want helper unreachable trap", kind, enabled, err)
					}
					in.Close()
				}
				c.Close()
			}
		}
	}
}
