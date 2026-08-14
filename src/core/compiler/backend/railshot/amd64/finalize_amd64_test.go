//go:build amd64

package amd64

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	amd64enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestIdentityFinalizerPreservesBytesAndMetadata(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = false
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})

	code := []byte{0x48, 0x81, 0xec, 0, 0, 0, 0, 0xe8, 0, 0, 0, 0, 0xc3}
	original := append([]byte(nil), code...)
	plan := &shared.GCFrameRootPlan{
		AdapterReturnOffset: 12,
		Callsites:           []shared.GCFrameCallsitePlan{{ReturnOffset: 12}},
	}
	f := fn{
		a:                &amd64enc.Asm{B: code},
		relocs:           []callReloc{{at: 8}},
		adapterReturnOff: 12,
		gcFrameRoots:     plan,
	}

	internal, err := f.finalizeNativeCode(7)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.a.B, original) {
		t.Fatalf("identity finalizer changed bytes: %x != %x", f.a.B, original)
	}
	if internal != 7 || f.relocs[0].at != 8 || f.adapterReturnOff != 12 ||
		plan.AdapterReturnOffset != 12 || plan.Callsites[0].ReturnOffset != 12 {
		t.Fatalf("metadata changed: internal=%d reloc=%d adapter=%d gc-adapter=%d gc-call=%d",
			internal, f.relocs[0].at, f.adapterReturnOff,
			plan.AdapterReturnOffset, plan.Callsites[0].ReturnOffset)
	}
}

func TestIdentityFinalizerCompileParity(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x01, 0x0b})
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeCompactionEnabled = false

	nativeFinalizerEnabled = false
	without, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	defer without.CodeImage.Close()

	nativeFinalizerEnabled = true
	with, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	defer with.CodeImage.Close()

	if !bytes.Equal(without.Code, with.Code) {
		t.Fatalf("identity finalizer changed module bytes")
	}
	if !reflect.DeepEqual(without.Entry, with.Entry) ||
		!reflect.DeepEqual(without.InternalEntry, with.InternalEntry) {
		t.Fatalf("identity finalizer changed entries: entry %v/%v internal %v/%v",
			without.Entry, with.Entry, without.InternalEntry, with.InternalEntry)
	}
}

func TestFinalizerCompactsSmallFrameAdjustments(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeFinalizerEnabled = true
	nativeCompactionEnabled = true

	a := &amd64enc.Asm{}
	subSite := a.Len() + 3
	a.SubRsp(24)
	a.B = append(a.B, 0x90)
	addSite := a.Len() + 3
	a.AddRsp(24)
	sc := &scratch{}
	f := fn{a: a, sc: sc, subRspAt: subSite, addRspAt: addSite}

	if _, err := f.finalizeNativeCode(0); err != nil {
		t.Fatal(err)
	}
	if got, want := len(f.a.B), 9; got != want {
		t.Fatalf("compacted code length = %d, want %d", got, want)
	}
	want := []byte{0x48, 0x83, 0xec, 24, 0x90, 0x48, 0x83, 0xc4, 24}
	if !bytes.Equal(f.a.B, want) {
		t.Fatalf("compacted frame bytes = %x, want %x", f.a.B, want)
	}
}

func TestFinalizerDeletesBranchFoldHole(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, []byte{
		0x00,       // no locals
		0x02, 0x40, // block void
		0x20, 0x00, 0x41, 0x05, 0x48, // local.get x; i32.const 5; i32.lt_s
		0x0d, 0x00, // br_if 0
		0x41, 0x0a, 0x0f, // return 10
		0x0b,
		0x41, 0x14, // return 20 through function end
		0x0b,
	})
	oldEnabled := nativeFinalizerEnabled
	oldCompact := nativeCompactionEnabled
	t.Cleanup(func() {
		nativeFinalizerEnabled = oldEnabled
		nativeCompactionEnabled = oldCompact
	})
	nativeFinalizerEnabled = true

	compile := func(compact bool) (*amd64enc.CompiledModule, *ModuleStats) {
		nativeCompactionEnabled = compact
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		return cm, stats
	}
	without, withoutStats := compile(false)
	defer without.CodeImage.Close()
	with, withStats := compile(true)
	defer with.CodeImage.Close()

	if got := withoutStats.Funcs[0].NativeSize.BranchFoldHoleBytes; got != 5 {
		t.Fatalf("uncompacted branch hole = %d, want 5", got)
	}
	if got := withStats.Funcs[0].NativeSize.BranchFoldHoleBytes; got != 0 {
		t.Fatalf("compacted branch hole = %d, want 0", got)
	}
	if got, wantMax := len(with.Code), len(without.Code)-5; got > wantMax {
		t.Fatalf("compacted length = %d, want <= %d", got, wantMax)
	}
}
