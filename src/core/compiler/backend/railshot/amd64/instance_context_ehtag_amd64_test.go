//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestCopyInstanceContextRestoresEHTagDirectory(t *testing.T) {
	f := &fn{a: &encoder.Asm{}}
	f.copyInstanceContext(R11, R10)

	var wantLoad encoder.Asm
	wantLoad.Load64(RAX, R10, 14*8)
	var want encoder.Asm
	want.Store64(R11, -int32(abi.EHTagDirPtrOffset), RAX)
	if !bytes.Contains(f.a.B, wantLoad.B) || !bytes.Contains(f.a.B, want.B) {
		t.Fatalf("copy instance context code %x does not load/store the EH tag directory (%x/%x)", f.a.B, wantLoad.B, want.B)
	}
}

func TestEHHandlerRestoresSavedInstanceContext(t *testing.T) {
	a := &encoder.Asm{}
	targetSite := a.LeaRipPlaceholder(RAX)
	f := &fn{a: a, sc: &scratch{}, ehTryCap: 1}
	fr := ctrlFrame{mergeIndex: 1}
	f.sc.ctrlMerges = []ctrlFrameMerge{{eh: &ctrlFrameEH{targetSite: uint32(targetSite)}}}
	recordOff := f.ehRecordOff(0)
	f.emitEHHandler(&fr)

	const contextOffset = 24 // existing handler context word
	var wantLinMem encoder.Asm
	wantLinMem.Load64(RBX, RDX, coreruntime.TableEntryHomeLinMemOffset)
	if !bytes.Contains(f.a.B, wantLinMem.B) {
		t.Fatalf("exception handler code %x does not restore its saved linear-memory base (%x)", f.a.B, wantLinMem.B)
	}
	var wantNativeContext encoder.Asm
	wantNativeContext.Load64(RDX, RSP, recordOff+contextOffset)
	wantNativeContext.Load64(RBX, RDX, coreruntime.TableEntryHomeLinMemOffset)
	wantNativeContext.Load64(RDX, RDX, coreruntime.FuncRefContextOffset)
	if !bytes.Contains(f.a.B, wantNativeContext.B) {
		t.Fatalf("exception handler code %x does not load the saved native context (%x)", f.a.B, wantNativeContext.B)
	}
	for i, basedataOff := range instanceContextOffsets[1:] {
		var want encoder.Asm
		want.Load64(RAX, RDX, int32((i+1)*8))
		want.Store64(RBX, -basedataOff, RAX)
		if !bytes.Contains(f.a.B, want.B) {
			t.Fatalf("exception handler code %x does not restore basedata offset %d (%x)", f.a.B, basedataOff, want.B)
		}
	}
	for _, field := range []struct {
		contextOff, basedataOff int32
	}{
		{13 * 8, int32(abi.GCNativeViewPtrOffset)},
		{14 * 8, int32(abi.EHTagDirPtrOffset)},
	} {
		var want encoder.Asm
		want.Load64(RAX, RDX, field.contextOff)
		want.Store64(RBX, -field.basedataOff, RAX)
		if !bytes.Contains(f.a.B, want.B) {
			t.Fatalf("exception handler code %x does not restore basedata offset %d (%x)", f.a.B, field.basedataOff, want.B)
		}
	}
	var wantCustom encoder.Asm
	wantCustom.Load64(RAX, RSP, 0)
	wantCustom.Store64(RBX, -offCustomCtx, RAX)
	if !bytes.Contains(f.a.B, wantCustom.B) {
		t.Fatalf("exception handler code %x does not restore the dynamically saved custom context (%x)", f.a.B, wantCustom.B)
	}
}

func TestEHTryCapturesDynamicCustomAndStableNativeContexts(t *testing.T) {
	f := &fn{a: &encoder.Asm{}, s: newStack(), sc: &scratch{}, ehTryCap: 1, m: &wasm.Module{}, moduleEH: true, ctrl: []ctrlFrame{{kind: cfBlock}}}
	if err := f.opTryTable(wasm.NewReader([]byte{0x40, 0x00})); err != nil {
		t.Fatal(err)
	}
	contextOff := f.ehRecordOff(0) + 24

	var wantCustom encoder.Asm
	wantCustom.Load64(RAX, RBX, -offCustomCtx)
	wantCustom.Store64(RSP, 0, RAX)
	if !bytes.Contains(f.a.B, wantCustom.B) {
		t.Fatalf("try entry code %x does not save the current custom context (%x)", f.a.B, wantCustom.B)
	}
	var wantNative encoder.Asm
	wantNative.Load64(RAX, RBX, -offFuncRefDescPtr)
	wantNative.Store64(RSP, contextOff, RAX)
	if !bytes.Contains(f.a.B, wantNative.B) {
		t.Fatalf("try entry code %x does not capture the stable native context through descriptor zero (%x)", f.a.B, wantNative.B)
	}
}

// Context restoration must not grow the variable-sized EH layout or shift a
// previously encodable spill beyond ARM64's scaled-displacement boundary.
func TestEHContextPreservesPerFunctionFrameLayout(t *testing.T) {
	for _, shape := range [][2]int{{0, 0}, {1, 0}, {1, 5}, {4, 4}, {8, 9}} {
		f := &fn{moduleEH: true, ehTryCap: shape[0], ehRootCap: shape[1], nLocalSlots: 3}
		wantBytes := (shape[0]*21 + shape[1]*17) * 8
		if got := f.ehFrameBytes(); got != wantBytes {
			t.Fatalf("EH shape %v uses %d bytes, want unchanged %d", shape, got, wantBytes)
		}
		wantRoot := int32(f.frameHeaderBytes() + 3*8 + shape[0]*21*8)
		if got := f.ehRootOff(0); got != wantRoot {
			t.Fatalf("EH shape %v root offset %d, want unchanged %d", shape, got, wantRoot)
		}
	}
}
