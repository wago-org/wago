//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The pending expression sits below the tail arguments and is discarded by the
// return. Its trap must run before target checks or the callee's global write.
func tailPendingTrapModule(kind string, wide bool, op byte, load bool) []byte {
	typ := wasm.I32
	if wide {
		typ = wasm.I64
	}
	body := []byte{0x20, 0}
	if load {
		body = append(body, 0x28, 2, 0)
	} else {
		body = append(body, 0x20, 1, op)
	}
	body = append(body, 0x41, 42)
	switch kind {
	case "direct":
		body = append(body, 0x12, 0)
	case "indirect":
		body = append(body, 0x20, 2, 0x13, 0, 0)
	case "ref":
		body = append(body, 0xd2, 0, 0x15, 0)
	case "null_ref":
		body = append(body, 0xd0, 0, 0x15, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("g", 3, 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x20, 0, 0x0b}), wasmtest.Code(body))),
	)
}

func TestTailCallsPreservePendingTraps(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, kind := range []string{"direct", "indirect", "ref", "null_ref"} {
			for _, tc := range []struct {
				name string
				wide bool
				op   byte
				a, b uint64
				want TrapCode
				load bool
			}{
				{"i32_div_s_zero", false, 0x6d, 1, 0, TrapDivZero, false},
				{"i32_div_u_zero", false, 0x6e, 1, 0, TrapDivZero, false},
				{"i32_rem_s_zero", false, 0x6f, 1, 0, TrapDivZero, false},
				{"i32_rem_u_zero", false, 0x70, 1, 0, TrapDivZero, false},
				{"i32_overflow", false, 0x6d, 1 << 31, 0xffffffff, TrapDivOverflow, false},
				{"i64_div_s_zero", true, 0x7f, 1, 0, TrapDivZero, false},
				{"i64_div_u_zero", true, 0x80, 1, 0, TrapDivZero, false},
				{"i64_rem_s_zero", true, 0x81, 1, 0, TrapDivZero, false},
				{"i64_rem_u_zero", true, 0x82, 1, 0, TrapDivZero, false},
				{"i64_overflow", true, 0x7f, 1 << 63, ^uint64(0), TrapDivOverflow, false},
				{"load_oob", false, 0, 65536, 0, TrapLinMemOutOfBounds, true},
			} {
				t.Run(fmt.Sprintf("%v/%s/%s", mode, kind, tc.name), func(t *testing.T) {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(mode).WithOptimization("inline", false), tailPendingTrapModule(kind, tc.wide, tc.op, tc.load))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					indexes := []uint64{0}
					if kind == "indirect" {
						indexes = append(indexes, 2)
					}
					for _, index := range indexes {
						_, err = in.Invoke("run", tc.a, tc.b, index)
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != tc.want {
							t.Errorf("index=%d: got %v, want %v", index, err, tc.want)
						}
						if got, err := in.Global("g"); err != nil || got != 0 {
							t.Fatalf("callee ran before trap: g=%d, %v", got, err)
						}
					}
					if kind != "null_ref" {
						got, err := in.Invoke("run", 8, 2, 0)
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatalf("nontrapping prefix: got %v, %v; want 42", got, err)
						}
						if got, err := in.Global("g"); err != nil || got != 42 {
							t.Fatalf("nontrapping callee: g=%d, %v", got, err)
						}
					}
				})
			}
		}
	}
}
