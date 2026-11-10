//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func maskedBranchModule(t *testing.T, mask uint32, lhs bool) *wasm.Module {
	body := []byte{0}
	for range 9 {
		body = append(body, 0x02, 0x40)
	}
	constant := append([]byte{0x41}, wasmtest.SLEB32(int32(mask))...)
	if lhs {
		body = append(body, constant...)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x6e)
	if !lhs {
		body = append(body, constant...)
	}
	body = append(body, 0x71, 0x0e, 8, 0, 1, 2, 3, 4, 5, 6, 7, 8)
	for i := range 9 {
		body = append(body, 0x0b, 0x41)
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f)
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestBoundedBranchIndexNative(t *testing.T) {
	for _, mask := range []uint32{0, 3, 7, 8, 15, 0x80000007, 0xffffffff} {
		for _, lhs := range []bool{false, true} {
			m := maskedBranchModule(t, mask, lhs)
			for _, enabled := range []bool{false, true} {
				for _, guard := range []bool{false, true} {
					for _, pair := range [][2]uint64{{0, 1}, {7, 1}, {8, 1}, {15, 1}, {63, 8}, {0xffffffff, 1}, {0xaabbccdd00000009, 1}, {7, 0}} {
						var stats ModuleStats
						opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"bounded-branch-index": enabled, "br-table-branch-vector": true}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1])
						if uint32(pair[1]) == 0 {
							var trap *rt.TrapError
							if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
								t.Fatalf("mask=%x lhs=%v enabled=%v pair=%x trap=%v", mask, lhs, enabled, pair, err)
							}
						} else {
							index := (uint32(pair[0]) / uint32(pair[1])) & mask
							if index >= 8 {
								index = 8
							}
							if err != nil || got != uint64(1000+index) {
								t.Fatalf("mask=%x lhs=%v enabled=%v pair=%x got=%d err=%v", mask, lhs, enabled, pair, got, err)
							}
						}
						if diagnosticsEnabled {
							admitted := enabled && mask < 8 && !nativeCompactionEnabled
							if (stats.Funcs[0].Peephole["bounded-branch-index"] != 0) != admitted {
								t.Fatalf("mask=%x lhs=%v enabled=%v stats=%v", mask, lhs, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestBoundedBranchIndexExclusions(t *testing.T) {
	for _, shape := range []string{"constant", "variable-mask", "stored-mask", "owned-expression", "duplicate-labels", "vector-off", "compact"} {
		body := []byte{0}
		if shape == "stored-mask" {
			body = []byte{1, 1, 0x7f, 0x20, 0, 0x41, 7, 0x71, 0x21, 3}
		}
		for range 9 {
			body = append(body, 0x02, 0x40)
		}
		switch shape {
		case "constant":
			body = append(body, 0x41, 3)
		case "variable-mask":
			body = append(body, 0x20, 0, 0x20, 2, 0x71)
		case "stored-mask":
			body = append(body, 0x20, 3)
		case "owned-expression":
			body = append(body, 0x20, 0, 0x41, 1, 0x6a, 0x41, 7, 0x71)
		default:
			body = append(body, 0x20, 0, 0x41, 7, 0x71)
		}
		body = append(body, 0x0e, 8)
		for i := byte(0); i < 8; i++ {
			label := i
			if shape == "duplicate-labels" {
				label = 0
			}
			body = append(body, label)
		}
		body = append(body, 8)
		for i := range 9 {
			body = append(body, 0x0b, 0x41)
			body = append(body, wasmtest.SLEB32(int32(1000+i))...)
			body = append(body, 0x0f)
		}
		body = append(body, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 7, 8, 17, 0xffffffff, 0xfeedface00000011} {
					index := uint32(x) & 7
					if shape == "constant" {
						index = 3
					} else if shape == "owned-expression" {
						index = (uint32(x) + 1) & 7
					} else if shape == "duplicate-labels" {
						index = 0
					}
					var stats ModuleStats
					opts := CompileOptions{ElideBoundsChecks: guard, CompactNative: shape == "compact", Optimizations: map[string]bool{"bounded-branch-index": enabled, "br-table-branch-vector": shape != "vector-off"}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, x, 1, 7)
					if err != nil || got != uint64(1000+index) {
						t.Fatalf("shape=%s enabled=%v bounds=%v x=%x got=%d err=%v", shape, enabled, guard, x, got, err)
					}
					if diagnosticsEnabled {
						admitted := enabled && !nativeCompactionEnabled && (shape == "constant" || shape == "owned-expression")
						if (stats.Funcs[0].Peephole["bounded-branch-index"] != 0) != admitted {
							t.Fatalf("shape=%s enabled=%v stats=%v", shape, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestBoundedBranchIndexOlderTrap(t *testing.T) {
	// A trapping value below the consumed index is discarded by every edge,
	// but it must execute before dispatch. The range proof must not replace
	// normal stack canonicalization with a direct jump.
	body := []byte{0}
	for range 9 {
		body = append(body, 0x02, 0x40)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x6e, 0x20, 2, 0x41, 7, 0x71, 0x0e, 8, 0, 1, 2, 3, 4, 5, 6, 7, 8)
	for i := range 9 {
		body = append(body, 0x0b, 0x41)
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		body = append(body, 0x0f)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			for _, divisor := range []uint64{0, 1} {
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"bounded-branch-index": enabled, "br-table-branch-vector": true}}, 8, divisor, 7)
				if divisor == 0 {
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
						t.Fatalf("enabled=%v bounds=%v older trap=%v", enabled, guard, err)
					}
				} else if err != nil || got != 1007 {
					t.Fatalf("enabled=%v bounds=%v got=%d err=%v", enabled, guard, got, err)
				}
			}
		}
	}
}
