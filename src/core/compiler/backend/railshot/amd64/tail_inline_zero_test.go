//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestTailInlineZeroDefiniteAssignment(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, tc := range []struct {
		name string
		body []byte
		want uint8
	}{
		{"unused", []byte{0x0b}, 0},
		{"read", []byte{0x20, 1, 0x1a, 0x0b}, 2},
		{"written", []byte{0x41, 7, 0x21, 1, 0x20, 1, 0x1a, 0x0b}, 0},
		{"both-arms-write", []byte{0x20, 0, 0x04, 0x40, 0x41, 1, 0x21, 1, 0x05, 0x41, 2, 0x21, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 0},
		{"missing-else", []byte{0x20, 0, 0x04, 0x40, 0x41, 1, 0x21, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 2},
		{"early-return", []byte{0x20, 0, 0x04, 0x40, 0x0f, 0x0b, 0x41, 7, 0x21, 1, 0x20, 1, 0x1a, 0x0b}, 0},
		{"branch-skips-write", []byte{0x02, 0x40, 0x0c, 0, 0x41, 7, 0x21, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 2},
		{"written-exit", []byte{0x02, 0x40, 0x41, 7, 0x21, 1, 0x0c, 0, 0x0b, 0x20, 1, 0x1a, 0x0b}, 0},
		{"conditional-exit", []byte{0x02, 0x40, 0x20, 0, 0x0d, 0, 0x41, 7, 0x21, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 2},
		{"table-exits", []byte{0x02, 0x40, 0x20, 0, 0x0e, 1, 0, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 2},
		{"written-table-exits", []byte{0x41, 7, 0x21, 1, 0x02, 0x40, 0x20, 0, 0x0e, 1, 0, 1, 0x0b, 0x20, 1, 0x1a, 0x0b}, 0},
		{"unreachable-read", []byte{0, 0x20, 1, 0x1a, 0x0b}, 0},
		{"tail-argument-read", []byte{0x20, 1, 0x12, 0, 0x0b}, 2},
		{"source-loop", []byte{0x03, 0x40, 0x0b, 0x0b}, 2},
		{"future-opcode", []byte{0xff, 0x0b}, 2},
		{"truncated", []byte{0x20, 0x80}, 2},
		{"invalid-local", []byte{0x20, 99, 0x0b}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tailInlineZeroMask(tc.body, 1, 2, classifier); got != tc.want {
				t.Fatalf("zero mask=%d want %d", got, tc.want)
			}
		})
	}
	var deep []byte
	for range 65 {
		deep = append(deep, 2, 0x40)
	}
	for range 66 {
		deep = append(deep, 0x0b)
	}
	if got := tailInlineZeroMask(deep, 1, 2, classifier); got != 2 {
		t.Fatalf("unbounded analysis returned %d", got)
	}
}

func TestInlineTargetZeroMaskDoesNotOverlapFlags(t *testing.T) {
	flags := []uint32{inlineTailLoopFlag, inlineRecursiveFlag, inlineI32AddConstFlag, inlineI32AddParamsFlag, inlineTupleLoopFlag, inlineTupleSelectFlag}
	occupied := uint32(inlineParamMask) | uint32(255)<<inlineTailZeroShift
	for _, flag := range flags {
		if occupied&flag != 0 {
			t.Fatalf("packed target flag overlaps another field: flag=%x occupied=%x", flag, occupied)
		}
		occupied |= flag
	}
	for _, mask := range []uint8{0, 1, 31, 255} {
		target := inlineTarget{params: 7 | uint32(mask)<<inlineTailZeroShift | inlineTailLoopFlag | inlineRecursiveFlag | inlineI32AddConstFlag | inlineI32AddParamsFlag | inlineTupleLoopFlag | inlineTupleSelectFlag}
		if target.paramCount() != 7 || target.tailZeroMask() != mask || !target.tailLoop() || !target.recursive() || !target.isI32AddConst() || !target.isI32AddParams() || !target.tupleLoop() || !target.tupleSelect() {
			t.Fatalf("packed target flags overlap: mask=%x params=%x", mask, target.params)
		}
	}
}
