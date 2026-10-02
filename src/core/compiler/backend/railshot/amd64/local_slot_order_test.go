//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func localSlotOrderModule(t *testing.T) *wasm.Module {
	// Locals 8..27 are equally hot. The eight lowest indexes take the available
	// whole-function pins, leaving locals 16..27 as hot frame residents. In
	// declaration order those homes need disp32; score ordering moves them into
	// disp8 range. Repeated sums make the encoded-length effect unambiguous.
	body := []byte{0x01, 0x1c, 0x7e} // 28 x i64 locals
	for i := byte(8); i < 28; i++ {
		body = append(body, 0x42, 0x01, 0x21, i) // i64.const 1; local.set i
	}
	body = append(body, 0x42, 0x00) // accumulator
	for repeat := 0; repeat < 8; repeat++ {
		for i := byte(8); i < 28; i++ {
			body = append(body, 0x20, i, 0x7c) // local.get i; i64.add
		}
	}
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I64}, body)
}

func TestLocalSlotOrderShrinksHotUnpinnedFrameRefs(t *testing.T) {
	saved := localSlotOrderEnabled
	defer func() { localSlotOrderEnabled = saved }()

	m := localSlotOrderModule(t)
	localSlotOrderEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	localSlotOrderEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]

	got, _, err := runMemAmd64(t, m, nil)
	if err != nil || got != 160 {
		t.Fatalf("ordered result = %d, err=%v, want 160", got, err)
	}
	if on.FrameBytes != off.FrameBytes {
		t.Fatalf("ordered frame = %d bytes, declaration frame = %d", on.FrameBytes, off.FrameBytes)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("ordered code = %d bytes, declaration code = %d", on.CodeBytes, off.CodeBytes)
	}
	if on.Peephole["local-slot-order"] != 1 {
		t.Fatalf("local-slot-order hits = %d, want 1", on.Peephole["local-slot-order"])
	}
}

func TestLocalSlotOrderDoesNotGrowMixedCompactFrame(t *testing.T) {
	saved := localSlotOrderEnabled
	defer func() { localSlotOrderEnabled = saved }()

	// Declaration order packs the two i32s after one i64 into 16 bytes. Putting
	// hot local 1 first would introduce an alignment gap and grow it to 24 bytes.
	body := []byte{0x02, 0x01, 0x7e, 0x02, 0x7f,
		0x41, 0x01, 0x21, 0x01,
		0x20, 0x01, 0x0b}
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)

	localSlotOrderEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	localSlotOrderEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if on.FrameBytes != off.FrameBytes || on.CodeBytes != off.CodeBytes {
		t.Fatalf("ordered mixed frame/code = %d/%d, declaration = %d/%d", on.FrameBytes, on.CodeBytes, off.FrameBytes, off.CodeBytes)
	}
	if on.Peephole["local-slot-order"] != 0 {
		t.Fatalf("local-slot-order hits = %d, want rollback", on.Peephole["local-slot-order"])
	}
}
