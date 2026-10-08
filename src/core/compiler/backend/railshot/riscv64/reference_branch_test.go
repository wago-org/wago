//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The null branch returns a mixed numeric payload through the root label,
// discarding a lower extra operand. The non-null branch carries an inline
// reference result past extra operands while preserving numeric values below
// the block. Neither fixture needs reference-valued external/indexed types.
func referenceBranchSource(vector, nonNull bool, depth int) []byte {
	typ := wasm.I64
	if vector {
		typ = wasm.V128
	}
	sig := []wasm.ValType{typ, wasm.I32}
	var body []byte
	if nonNull {
		body = append(body, 0x20, 0, 0x20, 1, 0x02, 0x70)
	}
	body = append(body, bytes.Repeat([]byte{0x02, 0x40}, depth)...)
	body = append(body, 0x42, 1, 0x42, 2)
	if !nonNull {
		body = append(body, 0x20, 0, 0x20, 1)
	}
	body = append(body, 0x23, 0)
	op := byte(0xd5)
	if nonNull {
		op = 0xd6
	}
	body = append(body, op)
	body = append(body, wasmtest.ULEB(uint32(depth))...)
	if nonNull {
		body = append(body, 0x1a, 0x1a, 0xd0, 0x70, 0x0c)
		body = append(body, wasmtest.ULEB(uint32(depth))...)
	} else {
		body = append(body, 0x24, 1, 0x0f)
	}
	body = append(body, bytes.Repeat([]byte{0x0b}, depth)...)
	if depth != 0 {
		body = append(body, 0x00)
	} // the nested fallthrough is unreachable
	if nonNull {
		body = append(body, 0x0b, 0x24, 1)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b}), wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestReferenceBranchPrivateGates(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"null", []byte{2, 0x40, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x0b, 0x0b}, true},
		{"non-null", []byte{2, 0x70, 0xd0, 0x70, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, true},
		{"dead-null", []byte{0, 0xd5, 0, 0x1a, 0x0b}, true},
		{"dead-non-null", []byte{2, 0x70, 0, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, true},
		{"inherited-dead-null-underflow", []byte{0, 2, 0x40, 0xd5, 0, 0x1a, 0x0b, 0x0b}, false},
		{"inherited-dead-null-payload", []byte{0, 2, 0x7f, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x0b}, false},
		{"inherited-dead-non-null-underflow", []byte{0, 2, 0x70, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false},
		{"explicit-dead-nested-payload", []byte{0, 2, 0x7f, 0, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x0b}, true},
		{"null-underflow", []byte{0xd5, 0, 0x1a, 0x0b}, false},
		{"null-numeric", []byte{0x42, 0, 0xd5, 0, 0x1a, 0x0b}, false},
		{"null-dead-numeric", []byte{0, 0x42, 0, 0xd5, 0, 0x1a, 0x0b}, false},
		{"null-bad-label", []byte{0, 0xd5, 1, 0x1a, 0x0b}, false},
		{"null-payload-class", []byte{2, 0x7f, 0x42, 0, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x0b}, false},
		{"null-dead-payload-class", []byte{2, 0x7f, 0, 0x42, 0, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x0b}, false},
		{"null-payload-floor", []byte{2, 0x7f, 0x41, 1, 2, 0x40, 0xd0, 0x70, 0xd5, 1, 0x1a, 0x0b, 0x0b, 0x1a, 0x0b}, false},
		{"non-null-empty-label", []byte{0xd0, 0x70, 0xd6, 0, 0x0b}, false},
		{"non-null-numeric-label", []byte{2, 0x7e, 0xd0, 0x70, 0xd6, 0, 0x42, 0, 0x0b, 0x1a, 0x0b}, false},
		{"non-null-numeric", []byte{2, 0x70, 0x42, 0, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false},
		{"non-null-dead-numeric", []byte{2, 0x70, 0, 0x42, 0, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false},
		{"non-null-bad-label", []byte{2, 0x70, 0, 0xd6, 2, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}, false},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-enabled%t", tc.name, enabled), func(t *testing.T) {
				fn := &wasm.Func{BodyBytes: tc.body}
				state := moduleState{production: true, importReferences: true, referenceCalls: enabled}
				_, a := walkInteger(fn, ft, &state, nil)
				_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, enabled, nil)
				if (a == nil) != (tc.ok && enabled) || (b == nil) != (tc.ok && enabled) {
					t.Fatal("reference branch gate", a, b)
				}
			})
		}
	}
}

func TestReferenceBranchDeadInstructionBound(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, count := range []int{(integerBudget - 2) / 2, (integerBudget-2)/2 + 1} {
		fn := &wasm.Func{BodyBytes: append(append([]byte{0}, bytes.Repeat([]byte{0xd5, 0, 0x1a}, count)...), 0x0b)}
		state := moduleState{production: true, importReferences: true, referenceCalls: true}
		_, a := walkInteger(fn, ft, &state, nil)
		_, b := walkSWARPairsWithTypedCallMode(fn, ft, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
		want := count == (integerBudget-2)/2
		if (a == nil) != want || (b == nil) != want {
			t.Fatal("dead branch count bound", count, a, b)
		}
	}
}

func TestProductionReferenceBranchRemainsClosed(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, nonNull := range []bool{false, true} {
			for _, dead := range []bool{false, true} {
				for _, witness := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-nonnull%t-dead%t-witness%t", vector, nonNull, dead, witness), func(t *testing.T) {
						m, err := wasm.DecodeModule(referenceBranchSource(vector, nonNull, 3))
						if err != nil {
							t.Fatal(err)
						}
						if dead {
							m.Code[0].BodyBytes = append([]byte{0}, m.Code[0].BodyBytes...)
						}
						if witness {
							m.Code[0].BodyBytes = append([]byte{0xd0, 0x70, 0xd4, 0x1a}, m.Code[0].BodyBytes...)
						}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
							t.Fatal("fixture", err)
						}
						pressure := 0
						image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
						if image != nil || !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "reference branch artifact contract") || pressure != 0 {
							t.Fatal("reference branch reached production staging", image, err, pressure)
						}
					})
				}
			}
		}
	}
}
