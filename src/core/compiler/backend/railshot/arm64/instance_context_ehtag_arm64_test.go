//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const testMaxScaled64FrameDisp = 0xfff * 8

func TestCopyInstanceContextRestoresEHTagDirectory(t *testing.T) {
	f := &fn{a: &a64.Asm{}}
	f.copyInstanceContext(X1, X10)

	wantLoad := &fn{a: &a64.Asm{}}
	wantLoad.ld64(X9, X10, 14*8)
	want := &fn{a: &a64.Asm{}}
	want.st64(X1, -int32(abi.EHTagDirPtrOffset), X9)
	if !bytes.Contains(f.a.B, wantLoad.a.B) || !bytes.Contains(f.a.B, want.a.B) {
		t.Fatalf("copy instance context code %x does not load/store the EH tag directory (%x/%x)", f.a.B, wantLoad.a.B, want.a.B)
	}
}

func TestEHHandlerRestoresSavedInstanceContext(t *testing.T) {
	a := &a64.Asm{}
	targetSite := a.Adr(X17)
	f := &fn{
		a: a, sc: &scratch{}, memSizeReg: X27,
		m: &wasm.Module{Globals: []wasm.Global{
			{Type: wasm.GlobalType{Type: wasm.I64}},
			{Type: wasm.GlobalType{Type: wasm.I64}},
		}},
		moduleGlobals: []moduleGlobalPin{{global: 0, reg: X25}},
		globalReg:     []Reg{X25, X24},
	}
	fr := ctrlFrame{mergeIndex: 1}
	f.sc.ctrlMerges = []ctrlFrameMerge{{eh: &ctrlFrameEH{targetSite: uint32(targetSite)}}}
	f.emitEHHandler(&fr)

	wantLinMem := &fn{a: &a64.Asm{}}
	wantLinMem.ld64(linMemReg, X16, coreruntime.TableEntryHomeLinMemOffset)
	if !bytes.Contains(f.a.B, wantLinMem.a.B) {
		t.Fatalf("exception handler code %x does not restore its saved linear-memory base (%x)", f.a.B, wantLinMem.a.B)
	}
	wantNativeContext := &fn{a: &a64.Asm{}}
	wantNativeContext.ld64(X16, ehReg, 24)
	wantNativeContext.ld64(linMemReg, X16, coreruntime.TableEntryHomeLinMemOffset)
	wantNativeContext.ld64(X16, X16, coreruntime.FuncRefContextOffset)
	if !bytes.Contains(f.a.B, wantNativeContext.a.B) {
		t.Fatalf("exception handler code %x does not load the saved native context (%x)", f.a.B, wantNativeContext.a.B)
	}
	contextFields := [...]int32{
		offTablePtr,
		offFuncRefDescPtr,
		offPassiveElemPtr,
		offGlobalsPtr,
		offPassiveDataPtr,
		offTableDirPtr,
		offMemoryDirPtr,
		offImportDispatchPtr,
	}
	for i, basedataOff := range contextFields {
		want := &fn{a: &a64.Asm{}}
		want.ld64(X17, X16, int32((i+1)*8))
		want.st64(linMemReg, -basedataOff, X17)
		if !bytes.Contains(f.a.B, want.a.B) {
			t.Fatalf("exception handler code %x does not restore basedata offset %d (%x)", f.a.B, basedataOff, want.a.B)
		}
	}
	badScratch := &fn{a: &a64.Asm{}}
	badScratch.ld64(X9, X16, 8)
	badScratch.st64(linMemReg, -offCustomCtx, X9)
	if bytes.Contains(f.a.B, badScratch.a.B) {
		t.Fatalf("exception handler code %x clobbers live allocatable pin X9 (%x)", f.a.B, badScratch.a.B)
	}
	for _, field := range []struct {
		contextOff, basedataOff int32
	}{
		{14 * 8, int32(abi.EHTagDirPtrOffset)},
		{13 * 8, int32(abi.GCNativeViewPtrOffset)},
	} {
		want := &fn{a: &a64.Asm{}}
		want.ld64(X17, X16, field.contextOff)
		if field.basedataOff == int32(abi.GCNativeViewPtrOffset) {
			// The GC view lies beyond STUR's signed 9-bit displacement. This exact
			// sequence keeps the regression executable on native ARM64 instead of
			// panicking while the compiler emits the handler.
			want.leaDisp(X16, linMemReg, -field.basedataOff, true)
			want.st64(X16, 0, X17)
		} else {
			want.st64(linMemReg, -field.basedataOff, X17)
		}
		if !bytes.Contains(f.a.B, want.a.B) {
			t.Fatalf("exception handler code %x does not restore basedata offset %d (%x)", f.a.B, field.basedataOff, want.a.B)
		}
	}
	wantCustom := &fn{a: &a64.Asm{}}
	wantCustom.ld64(X17, SP, 0)
	wantCustom.st64(linMemReg, -offCustomCtx, X17)
	if !bytes.Contains(f.a.B, wantCustom.a.B) {
		t.Fatalf("exception handler code %x does not restore the dynamically saved custom context (%x)", f.a.B, wantCustom.a.B)
	}

	// Restoring basedata is not sufficient: the native ABI caches the current
	// memory bound and selected globals in registers, so the exceptional edge must
	// refresh them exactly like an ordinary cross-instance call continuation.
	wantMemSize := &fn{a: &a64.Asm{}}
	wantMemSize.ld64(X27, linMemReg, -bdCurBytes)
	if !bytes.Contains(f.a.B, wantMemSize.a.B) {
		t.Fatalf("exception handler code %x does not refresh its cached memory bound (%x)", f.a.B, wantMemSize.a.B)
	}
	for global, reg := range []Reg{X25, X24} {
		want := &fn{a: &a64.Asm{}}
		want.ld64(reg, linMemReg, -int32(abi.GlobalsPtrOffset))
		want.ld64(reg, reg, int32(global*8))
		want.ld64(reg, reg, 0)
		if !bytes.Contains(f.a.B, want.a.B) {
			t.Fatalf("exception handler code %x does not refresh global %d in %v (%x)", f.a.B, global, reg, want.a.B)
		}
	}

	// An unmatched inner handler forwards into the previous record before that
	// outer catch converges local state. Keep allocatable X9 live across the copy.
	wantForward := &fn{a: &a64.Asm{}}
	wantForward.ld64(ehReg, X16, ehTagOff)
	wantForward.st64(X17, ehTagOff, ehReg)
	if !bytes.Contains(f.a.B, wantForward.a.B) {
		t.Fatalf("exception handler code %x does not forward with reserved scratches (%x)", f.a.B, wantForward.a.B)
	}
	badForward := &fn{a: &a64.Asm{}}
	badForward.ld64(X9, X16, ehTagOff)
	badForward.st64(X17, ehTagOff, X9)
	if bytes.Contains(f.a.B, badForward.a.B) {
		t.Fatalf("exception handler code %x clobbers live X9 while forwarding (%x)", f.a.B, badForward.a.B)
	}
}

func TestEHHandlerRestoresInstanceContextBeyondScaledDisplacement(t *testing.T) {
	a := &a64.Asm{}
	targetSite := a.Adr(X17)
	eh := &ctrlFrameEH{
		targetSite: uint32(targetSite),
		// Exercise tag dispatch and both matched catch routes. A handler without
		// catches would miss the other fixed-record accesses after restoration.
		catches: []ehCatchClause{
			{kind: wasm.CatchTag, frame: 0},
			{kind: wasm.CatchAll, frame: 0},
		},
	}
	f := &fn{
		a: a, sc: &scratch{}, moduleEH: true, ehTryCap: 1, ehRootCap: 1,
		// Put the handler beyond LDR/STR's 32 KiB scaled displacement.
		// Restore through the record pointer without clobbering a live local pin.
		nLocalSlots: 4096,
		ctrl:        []ctrlFrame{{kind: cfBlock}},
	}
	fr := ctrlFrame{mergeIndex: 1}
	f.sc.ctrlMerges = []ctrlFrameMerge{{eh: eh}}
	f.emitEHHandler(&fr)

	wantBase := &fn{a: &a64.Asm{}}
	wantBase.leaDisp(ehReg, SP, f.ehRecordOff(0), true)
	if !bytes.Contains(f.a.B, wantBase.a.B) {
		t.Fatalf("large-frame handler code %x does not rebase its context snapshot (%x)", f.a.B, wantBase.a.B)
	}
}

func TestEHTryInitializesReferenceRootBeyondScaledDisplacement(t *testing.T) {
	f := &fn{
		a: &a64.Asm{}, s: newStack(), sc: &scratch{}, m: &wasm.Module{}, moduleEH: true, ehTryCap: 1, ehRootCap: 1,
		// Keep a synthetically live X9 pin while forcing all EH records past the
		// architectural scaled-offset limit; neither property may consume X16/X17.
		nLocals: 1, nLocalSlots: 4096, localSlot: []uint32{0},
		localType: []machineType{mtI64}, locals: []localDef{{reg: X9}},
		pinnedLocalMask: maskOf(X9),
		ctrl:            []ctrlFrame{{kind: cfBlock, resultN: 1, branchN: 1, res0: mtI64}},
	}
	// catch_all_ref targets the outer one-result block and therefore allocates and
	// zeroes a rooted exception record during try entry.
	try := []byte{0x40, 0x01, byte(wasm.CatchAllRef), 0x00}
	if err := f.opTryTable(wasm.NewReader(try)); err != nil {
		t.Fatal(err)
	}

	rootOff := f.ehRootOff(0)
	if rootOff <= 0xfff*8 {
		t.Fatalf("root offset %d does not exceed the scaled LDR/STR range", rootOff)
	}
	want := &fn{a: &a64.Asm{}}
	want.leaDisp(X16, SP, rootOff, true)
	want.st64(X16, 0, ZR)
	if !bytes.Contains(f.a.B, want.a.B) {
		t.Fatalf("large-frame try entry code %x does not rebase its root record (%x)", f.a.B, want.a.B)
	}
}

func TestEHTailDiscardRestoresOuterHandlerBeyondScaledDisplacement(t *testing.T) {
	f := &fn{a: &a64.Asm{}, moduleEH: true, ehTryCap: 1, ehRootCap: 1, ehTryDepth: 1, nLocalSlots: 4096}
	f.discardEHHandlersForTail()

	recordOff := f.ehRecordOff(0)
	want := &fn{a: &a64.Asm{}}
	want.leaDisp(X16, SP, recordOff, true)
	want.ld64(ehReg, X16, ehPrevOff)
	if !bytes.Equal(f.a.B, want.a.B) {
		t.Fatalf("large-frame tail discard code = %x, want rebased record load %x", f.a.B, want.a.B)
	}
}

func TestEHTryContextCaptureUsesReservedScratch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		localSlots int
	}{
		{name: "small-frame", localSlots: 1},
		{name: "large-frame", localSlots: 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fn{
				a: &a64.Asm{}, s: newStack(), sc: &scratch{}, moduleEH: true, ehTryCap: 1, ehRootCap: 1,
				nLocals: 1, nLocalSlots: tc.localSlots, localSlot: []uint32{0},
				localType: []machineType{mtI64}, locals: []localDef{{reg: X9}},
				pinnedLocalMask: maskOf(X9), ctrl: []ctrlFrame{{kind: cfBlock}},
			}
			if err := f.opTryTable(wasm.NewReader([]byte{0x40, 0x00})); err != nil {
				t.Fatal(err)
			}
			want := &fn{a: &a64.Asm{}}
			want.ld64(X17, linMemReg, -offCustomCtx)
			want.st64(SP, 0, X17)
			if !bytes.Contains(f.a.B, want.a.B) {
				t.Fatalf("try entry code %x does not save CustomCtx with reserved X17 (%x)", f.a.B, want.a.B)
			}
			wantBase := &fn{a: &a64.Asm{}}
			wantBase.leaDisp(X16, SP, f.ehRecordOff(0), true)
			if !bytes.Contains(f.a.B, wantBase.a.B) {
				t.Fatalf("try entry code %x does not address its handler record (%x)", f.a.B, wantBase.a.B)
			}
			bad := &fn{a: &a64.Asm{}}
			bad.ld64(X9, linMemReg, -offCustomCtx)
			if bytes.Contains(f.a.B, bad.a.B) {
				t.Fatalf("try entry code %x clobbers live allocatable pin X9 (%x)", f.a.B, bad.a.B)
			}
			wantNative := &fn{a: &a64.Asm{}}
			wantNative.ld64(X17, linMemReg, -offFuncRefDescPtr)
			wantNative.st64(X16, 24, X17)
			if !bytes.Contains(f.a.B, wantNative.a.B) {
				t.Fatalf("try entry code %x does not save descriptor zero in the existing context word (%x)", f.a.B, wantNative.a.B)
			}
		})
	}
}

func TestEHCompactContextKeepsSpillWithinScaledDisplacement(t *testing.T) {
	f := &fn{a: &a64.Asm{}, moduleEH: true, ehTryCap: 1, nLocalSlots: 4072}
	if got, want := f.ehFrameBytes(), 168; got != want {
		t.Fatalf("fixed EH frame bytes = %d, want the unchanged single-handler size %d", got, want)
	}
	off := f.spillOff(0)
	if off != testMaxScaled64FrameDisp {
		t.Fatalf("spill offset = %d, want scaled 64-bit boundary %d", off, testMaxScaled64FrameDisp)
	}
	f.st64(SP, off, X9)
	if got := f.a.Len(); got != 4 {
		t.Fatalf("boundary spill store emitted %d bytes, want one direct instruction", got)
	}
}

func TestCompileEHFrameWithSpillAtScaledDisplacementBoundary(t *testing.T) {
	body := []byte{0x01}
	body = append(body, wasmtest.ULEB(4072)...)
	body = append(body,
		0x7e,       // 4,072 i64 locals
		0x02, 0x40, // block
		0x1f, 0x40, 0x01, // try_table void, one catch
		byte(wasm.CatchAll), 0x00, // catch_all targets the outer block
		0x41, 0x07, // i32.const 7 (tag payload)
		0x08, 0x00, // throw tag 0
		0x0b, 0x0b, 0x0b, // end try_table, block, function
	)
	// wasmtest.Code always prepends a zero local-declaration count. This fixture
	// already carries one explicit 4,072-local run, so frame the body directly;
	// otherwise the compiler interprets the local count as an opcode and never
	// reaches the intended spill boundary.
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode boundary module: %v", err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatalf("compile EH frame with spill 0 at scaled displacement boundary: %v", err)
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
