//go:build linux && amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestLocalSlotPackedDomain(t *testing.T) {
	refs := encamd64.LocalRefRecorder{Locals: 1}
	f := fn{a: &encamd64.Asm{LocalRefs: &refs}, localSlot: []uint32{0x12340}}
	if got, want := unsafe.Sizeof(f.localSlot[0]), uintptr(4); got != want {
		t.Fatalf("local slot width = %d, want %d", got, want)
	}
	wantOff := int32(f.frameHeaderBytes() + 0x12340)
	for range 255 {
		if got := f.localAddr(0); got != wantOff {
			t.Fatalf("local offset = %#x, want %#x", got, wantOff)
		}
		refs.Pending = false // stand in for the encoder consuming this mark.
	}
	if got := f.localRefCount(0); got != 255 || refs.Overflow {
		t.Fatalf("reference count/overflow = %d/%v, want 255/false", got, refs.Overflow)
	}
	f.localAddr(0)
	if !refs.Overflow || f.localRefCount(0) != 255 {
		t.Fatalf("saturated reference count/overflow = %d/%v, want 255/true", f.localRefCount(0), refs.Overflow)
	}
}

func localSlotOrderModule(t *testing.T) *wasm.Module {
	// Locals 8..27 are equally hot. The eight lowest indexes take the available
	// whole-function pins, leaving locals 16..27 as hot frame residents. In
	// declaration order those homes need disp32; exact post-codegen ordering moves
	// them into zero-reference low homes. Repeated sums make the encoded-length
	// effect unambiguous. A zero-length memory.fill disables the regional cache so
	// the low hot locals use whole-function pins and leave their homes untouched.
	body := []byte{0x01, 0x1c, 0x7e} // 28 x i64 locals
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, // dst, value, len
		0xfc, 0x0b, 0x00, // memory.fill 0
	)
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
	m := localSlotOrderModule(t)
	off := compileLocalSlotOrder(t, m, false).Funcs[0]
	on := compileLocalSlotOrder(t, m, true).Funcs[0]

	got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{
		CompactNative: true,
		Optimizations: map[string]bool{"local-slot-order": true},
	}, nil)
	if err != nil || got != 160 {
		t.Fatalf("ordered result = %d, err=%v, want 160", got, err)
	}
	if on.FrameBytes != off.FrameBytes {
		t.Fatalf("ordered frame = %d bytes, declaration frame = %d", on.FrameBytes, off.FrameBytes)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("ordered code = %d bytes, declaration code = %d; encoding=%#v peepholes=%v", on.CodeBytes, off.CodeBytes, on.Encoding, on.Peephole)
	}
	if on.Peephole["local-slot-order"] != 1 {
		t.Fatalf("local-slot-order hits = %d, want 1", on.Peephole["local-slot-order"])
	}
}

func TestLocalSlotOrderDefaultsOnForCompaction(t *testing.T) {
	m := localSlotOrderModule(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["local-slot-order"]; got != 1 {
		t.Fatalf("default Size local-slot-order hits = %d, want 1", got)
	}
}

func TestLocalSlotOrderDoesNotGrowMixedCompactFrame(t *testing.T) {
	// Declaration order packs the two i32s after one i64 into 16 bytes. Putting
	// hot local 1 first would introduce an alignment gap and grow it to 24 bytes.
	body := []byte{0x02, 0x01, 0x7e, 0x02, 0x7f,
		0x41, 0x01, 0x21, 0x01,
		0x20, 0x01, 0x0b}
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)

	off := compileLocalSlotOrder(t, m, false).Funcs[0]
	on := compileLocalSlotOrder(t, m, true).Funcs[0]
	if on.FrameBytes != off.FrameBytes || on.CodeBytes != off.CodeBytes {
		t.Fatalf("ordered mixed frame/code = %d/%d, declaration = %d/%d", on.FrameBytes, on.CodeBytes, off.FrameBytes, off.CodeBytes)
	}
	if on.Peephole["local-slot-order"] != 0 {
		t.Fatalf("local-slot-order hits = %d, want rollback", on.Peephole["local-slot-order"])
	}
}

func TestLocalSlotOrderSkipsGCFrameRootFunctions(t *testing.T) {
	refs := encamd64.LocalRefRecorder{
		Sites:  []encamd64.LocalRefSite{{Local: 1}},
		Limit:  1,
		Locals: 2,
	}
	plan := &shared.GCFrameRootPlan{Candidate: true, Locals: []shared.GCFrameLocal{{Index: 1, Offset: 128}}}
	f := fn{
		a:                  &encamd64.Asm{LocalRefs: &refs},
		nLocals:            2,
		localType:          []machineType{mtI64, mtI64},
		localSlot:          []uint32{0, uint32(1)<<localSlotRefShift | 128},
		compactFrameHeader: true,
		gcFrameRoots:       plan,
		stats:              &CodegenStats{},
	}
	if got := f.packLocalSlots(1); got != 0 {
		t.Fatalf("GC frame-root local slot swaps = %d, want 0", got)
	}
	if got := f.localOff(1); got != 128 || plan.Locals[0].Offset != 128 {
		t.Fatalf("GC frame-root local home changed: frame=%d metadata=%d", got, plan.Locals[0].Offset)
	}
}

func TestLocalSlotOrderExcludesMultiSlotHomes(t *testing.T) {
	refs := encamd64.LocalRefRecorder{
		Sites:  []encamd64.LocalRefSite{{Local: 1}},
		Limit:  1,
		Locals: 2,
	}
	a := encamd64.Asm{LocalRefs: &refs}
	f := fn{
		a:                  &a,
		nLocals:            2,
		localType:          []machineType{mtV128, mtV128},
		localSlot:          []uint32{0, uint32(1)<<localSlotRefShift | 128},
		compactFrameHeader: true,
	}
	if got := f.packLocalSlots(1); got != 0 {
		t.Fatalf("multi-slot swaps = %d, want 0", got)
	}
	if got := f.localOff(1); got != 128 {
		t.Fatalf("multi-slot target offset = %d, want 128", got)
	}
}

func compileLocalSlotOrder(t *testing.T, m *wasm.Module, enabled bool) *ModuleStats {
	t.Helper()
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		CompactNative: true,
		Optimizations: map[string]bool{"local-slot-order": enabled},
		Stats:         &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { cm.CodeImage.Close() })
	}
	return &stats
}
