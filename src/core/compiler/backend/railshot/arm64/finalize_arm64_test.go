//go:build arm64

package arm64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestIdentityFinalizerRemapsAllArm64Metadata(t *testing.T) {
	before := nativeFinalizerEnabled
	beforeValidate := nativeFinalizerValidate
	nativeFinalizerEnabled = true
	nativeFinalizerValidate = true
	t.Cleanup(func() {
		nativeFinalizerEnabled = before
		nativeFinalizerValidate = beforeValidate
	})

	plan := &shared.GCFrameRootPlan{
		AdapterReturnOffset: 12,
		Callsites: []shared.GCFrameCallsitePlan{
			{ReturnOffset: 16},
			{ReturnOffset: 24, StackAdjust: 64},
		},
	}
	sc := &scratch{asm: &a64.Asm{B: make([]byte, 32)}, branchTargets: map[int]bool{-29: true}}
	f := fn{
		a:                sc.asm,
		sc:               sc,
		relocs:           []callReloc{{at: 4}, {at: 20}},
		adapterReturnOff: 12,
		gcFrameRoots:     plan,
		subRspAt:         0,
		addRspAt:         20,
	}

	internal, err := f.finalizeNativeCode(8)
	if err != nil {
		t.Fatal(err)
	}
	if internal != 8 || f.adapterReturnOff != 12 || plan.AdapterReturnOffset != 12 {
		t.Fatalf("entry/adapter offsets changed: internal=%d adapter=%d plan=%d", internal, f.adapterReturnOff, plan.AdapterReturnOffset)
	}
	if f.relocs[0].at != 4 || f.relocs[1].at != 20 || plan.Callsites[0].ReturnOffset != 16 || plan.Callsites[1].ReturnOffset != 24 {
		t.Fatalf("relocation/callsite offsets changed: relocs=%#v callsites=%#v", f.relocs, plan.Callsites)
	}
}

func TestCompileIdentityFinalizerByteParityArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x10, 0x01, 0x0b}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}},
	)

	before := nativeFinalizerEnabled
	beforeValidate := nativeFinalizerValidate
	nativeFinalizerValidate = true
	t.Cleanup(func() {
		nativeFinalizerEnabled = before
		nativeFinalizerValidate = beforeValidate
	})
	compile := func(enabled bool) ([]byte, []int, []int, []uint64) {
		nativeFinalizerEnabled = enabled
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 2})
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), cm.Code...), slices.Clone(cm.Entry), slices.Clone(cm.InternalEntry), slices.Clone(cm.DirectPrepared)
	}

	withoutCode, withoutEntry, withoutInternal, withoutPrepared := compile(false)
	withCode, withEntry, withInternal, withPrepared := compile(true)
	if !bytes.Equal(withCode, withoutCode) || !slices.Equal(withEntry, withoutEntry) || !slices.Equal(withInternal, withoutInternal) || !slices.Equal(withPrepared, withoutPrepared) {
		t.Fatalf("identity finalizer changed module output:\ncode equal=%v\nentry %v / %v\ninternal %v / %v\nprepared %v / %v",
			bytes.Equal(withCode, withoutCode), withEntry, withoutEntry, withInternal, withoutInternal, withPrepared, withoutPrepared)
	}
}
