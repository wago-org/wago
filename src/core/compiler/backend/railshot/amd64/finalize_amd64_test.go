//go:build amd64

package amd64

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	amd64enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestIdentityFinalizerPreservesBytesAndMetadata(t *testing.T) {
	oldEnabled := nativeFinalizerEnabled
	nativeFinalizerEnabled = true
	t.Cleanup(func() { nativeFinalizerEnabled = oldEnabled })

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
	t.Cleanup(func() { nativeFinalizerEnabled = oldEnabled })

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
