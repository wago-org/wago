//go:build !tinygo && (amd64 || arm64)

package wago

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDraglineLeafInliningPreservesSemantics(t *testing.T) {
	for _, tc := range []struct {
		name         string
		typ          wasm.ValType
		leaf, caller []byte
		arg, want    uint64
		trap         TrapCode
	}{
		{
			name: "locals-reset-at-two-sites", typ: wasm.I32,
			// Return the old local value, then mutate it for the next site.
			leaf:   []byte{1, 1, 0x7f, 0x20, 1, 0x20, 0, 0x21, 1, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x20, 0, 0x10, 0, 0x6a, 0x0b},
			arg:    41, want: 0,
		},
		{
			name: "nested-loops-and-early-return", typ: wasm.I32,
			// Leaf: sum 1..n, with an explicit return nested in a loop.
			leaf: []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, 0x0f, 0x0b,
				0x20, 1, 0x20, 0, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x00, 0x0b},
			// Caller: sum leaf(n), leaf(n-1), ... leaf(1).
			caller: []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
				0x20, 1, 0x20, 0, 0x10, 0, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
				0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b},
			arg: 20, want: 1540,
		},
		{
			name: "function-label-branch-table", typ: wasm.I32,
			leaf:   []byte{0, 0x02, 0x7f, 0x41, 37, 0x20, 0, 0x0e, 1, 1, 0, 0x0b, 0x41, 5, 0x6a, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x41, 1, 0x6a, 0x0b},
			arg:    0, want: 38,
		},
		{
			name: "function-label-fallthrough", typ: wasm.I32,
			leaf:   []byte{0, 0x02, 0x7f, 0x41, 37, 0x20, 0, 0x0e, 1, 1, 0, 0x0b, 0x41, 5, 0x6a, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x41, 1, 0x6a, 0x0b},
			arg:    1, want: 43,
		},
		{
			name: "ordered-memory-effects", typ: wasm.I32,
			// Increment the value at address p; the second call observes the first.
			leaf:   []byte{1, 1, 0x7f, 0x20, 0, 0x20, 0, 0x28, 2, 0, 0x41, 1, 0x6a, 0x22, 1, 0x36, 2, 0, 0x20, 1, 0x0b},
			caller: []byte{0, 0x20, 0, 0x41, 0, 0x36, 2, 0, 0x20, 0, 0x10, 0, 0x20, 0, 0x10, 0, 0x6a, 0x0b},
			arg:    16, want: 3,
		},
		{
			name: "memory-trap", typ: wasm.I32,
			leaf:   []byte{0, 0x20, 0, 0x28, 2, 0, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x0b},
			arg:    65535, trap: TrapLinMemOutOfBounds,
		},
		{
			name: "divide-trap", typ: wasm.I32,
			leaf:   []byte{0, 0x41, 1, 0x20, 0, 0x6d, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x0b},
			arg:    0, trap: TrapDivZero,
		},
		{
			name: "i64-locals-and-result", typ: wasm.I64,
			leaf:   []byte{1, 1, 0x7e, 0x20, 0, 0x20, 1, 0x7c, 0x0f, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x0b},
			arg:    0x87654321fedcba98, want: 0x87654321fedcba98,
		},
		{
			name: "f32-locals-and-result", typ: wasm.F32,
			leaf:   []byte{1, 1, 0x7d, 0x20, 0, 0x20, 1, 0x92, 0x0f, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x0b},
			arg:    uint64(math.Float32bits(13.25)), want: uint64(math.Float32bits(13.25)),
		},
		{
			name: "f64-locals-and-result", typ: wasm.F64,
			leaf:   []byte{1, 1, 0x7c, 0x20, 0, 0x20, 1, 0xa0, 0x0f, 0x0b},
			caller: []byte{0, 0x20, 0, 0x10, 0, 0x0b},
			arg:    math.Float64bits(-17.125), want: math.Float64bits(-17.125),
		},
	} {
		modes := []BoundsCheckMode{BoundsChecksExplicit}
		if guardPageBuilt {
			modes = append(modes, BoundsChecksSignalsBased)
		}
		for _, bounds := range modes {
			for _, workers := range []int{1, 4} {
				t.Run(fmt.Sprintf("%s/bounds=%v/workers=%d", tc.name, bounds, workers), func(t *testing.T) {
					code := func(raw []byte) []byte { return append(wasmtest.ULEB(uint32(len(raw))), raw...) }
					source := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}))),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
						wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
						wasmtest.Section(10, wasmtest.Vec(code(tc.leaf), code(tc.caller))),
					)
					compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(bounds).WithFunctionWorkers(workers), source)
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					instance, err := Instantiate(compiled, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					for repeat := 0; repeat < 3; repeat++ {
						got, err := instance.Invoke("run", tc.arg)
						if tc.trap != 0 {
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != tc.trap {
								t.Fatalf("trap=%v, want %v", err, tc.trap)
							}
						} else if err != nil || len(got) != 1 || got[0] != tc.want {
							t.Fatalf("run(%d)=%v,%v; want %d", tc.arg, got, err, tc.want)
						}
					}
				})
			}
		}
	}
}
