//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/nativeabi"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Records live after locals and before operand spills. The invocation's handler
// cell is the existing padding word at the top of the native wrapper save area.
// Child invocation views retain the root top, so cross-instance calls share this
// cell without sharing mutable state between native invocations.
const (
	exceptionPrevious      = 0
	exceptionSP            = 8
	exceptionFP            = 16
	exceptionPC            = 24
	exceptionHome          = 32
	exceptionContext       = 40
	exceptionView          = 48
	exceptionControlOffset = 56
	exceptionHost          = 64
	exceptionTag           = 72
	exceptionPayload       = 80
	exceptionRecordBytes   = exceptionPayload + shared.EHPayloadLanes*8
	exceptionRootBytes     = (1 + shared.EHPayloadLanes) * 8
	trapExceptionUnhandled = 17
)

type exceptionCatch struct {
	kind        wasm.CatchKind
	tag         uint32
	target      int
	root        int
	payload     []wasm.ValType
	hiddenRefs  []bool
	collectorPC uint32
}

type exceptionControl struct {
	index   int
	site    int
	clauses []exceptionCatch
	active  bool
}

type exceptionActive struct{ depth, index int }

type exceptionFrame struct {
	module   *wasm.Module
	shape    shared.EHFrameShape
	base     int32
	nextRoot int
	active   []exceptionActive
}

func exceptionTagSignature(m *wasm.Module, index uint32) (*wasm.CompType, error) {
	tag, ok := shared.TagType(m, index)
	if !ok {
		return nil, unsupported("exception tag index")
	}
	sig, ok := m.TypeFunc(tag.Type.Index)
	if !ok || len(sig.Results) != 0 || len(sig.Params) > shared.MaxEHTagPayloadWords {
		return nil, unsupported("exception tag payload bound")
	}
	numericWords := len(sig.Params)
	for _, typ := range sig.Params {
		if wasm.EqualValType(typ, wasm.V128) {
			numericWords++
		}
		if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) &&
			!wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !wasm.EqualValType(typ, wasm.V128) && typ.Kind() != wasm.ValRef {
			return nil, unsupported("exception scalar/reference payload")
		}
	}
	if numericWords > shared.MaxEHTagPayloadWords {
		return nil, unsupported("exception numeric payload word bound")
	}
	return sig, nil
}

// GC lanes use logical parameter indexes. Other carriers use physical words,
// so a vector occupies two identity lanes without moving the fixed GC area.

func exceptionPayloadRootKind(m *wasm.Module, typ wasm.ValType) (nativeabi.RootKind, bool) {
	if exceptionReferenceType(typ) {
		return nativeabi.RootGCRef, true
	}
	return shared.EHPayloadRootKind(m, typ)
}

func exceptionPayloadLane(m *wasm.Module, params []wasm.ValType, i int) int {
	if kind, root := exceptionPayloadRootKind(m, params[i]); root && kind == nativeabi.RootGCRef {
		return shared.EHGCLaneBase + i
	}
	lane := i
	for _, typ := range params[:i] {
		if wasm.EqualValType(typ, wasm.V128) {
			lane++
		}
	}

	return lane
}

func (e *exceptionFrame) record(index int) int32 { return e.base + int32(index*exceptionRecordBytes) }
func (e *exceptionFrame) root(index int) int32 {
	return e.base + int32(e.shape.TryRecords*exceptionRecordBytes+index*exceptionRootBytes)
}
func (e *exceptionFrame) slots() int {
	return (e.shape.TryRecords*exceptionRecordBytes + e.shape.RootRecords*exceptionRootBytes) / 8
}

// Only X5/X6/X7 and the ordinary reserved scratches are used. Scalar lowering
// has no additional reserved register and retains its complete register pool.
func (f *integerEmitter) exceptionCell(reg enc.Reg) {
	f.a.Ld(reg, enc.X25, -abi.RV64InvocationViewOffset)
	f.a.Ld(reg, reg, abi.RV64InvocationTopOffset)
	f.a.Addi(reg, reg, -8)
}

func (f *integerEmitter) exceptionAddress(reg enc.Reg, offset int32) {
	f.a.Constant64(reg, int64(offset))
	f.a.Add(reg, enc.SP, reg)
}

func (f *integerEmitter) initializeExceptionRoots() {
	e := f.exception
	if e == nil {
		return
	}
	for record := 0; record < e.shape.TryRecords; record++ {
		f.exceptionAddress(enc.X6, e.record(record))
		for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
			f.a.Sd(enc.Zero, enc.X6, exceptionPayload+int32(lane*8))
		}
	}
	for root := 0; root < e.shape.RootRecords; root++ {
		f.exceptionAddress(enc.X6, e.root(root))
		for lane := 0; lane < 1+shared.EHPayloadLanes; lane++ {
			f.a.Sd(enc.Zero, enc.X6, int32(lane*8))
		}
	}
}

func (f *integerEmitter) beginExceptionHandler(control *exceptionControl, depth int) {
	e := f.exception
	control.index = len(e.active)
	control.active = true
	f.exceptionAddress(enc.X6, e.record(control.index))
	f.exceptionCell(enc.X7)
	f.a.Ld(enc.X5, enc.X7, 0)
	f.a.Sd(enc.X5, enc.X6, exceptionPrevious)
	f.a.Sd(enc.SP, enc.X6, exceptionSP)
	f.a.Sd(enc.X8, enc.X6, exceptionFP)
	control.site = f.a.JumpLong(false)
	// The AUIPC/JALR packet is patched as an address producer: preserve the
	// high instruction and replace the low instruction with ADDI at finalization.
	f.a.Sd(enc.Scratch1, enc.X6, exceptionPC)
	f.a.Sd(enc.X25, enc.X6, exceptionHome)
	f.a.Ld(enc.X5, enc.X25, -abi.RV64NativeContextOffset)
	liveContext := f.a.BranchLong(enc.NE, enc.X5, enc.Zero)
	f.a.Ld(enc.X5, enc.X25, -abi.FuncRefDescPtrOffset)
	f.a.Ld(enc.X5, enc.X5, coreruntime.FuncRefContextOffset)
	if err := f.Patch(liveContext, f.Position()); err != nil {
		f.err = err
	}
	f.a.Sd(enc.X5, enc.X6, exceptionContext)
	f.a.Ld(enc.X5, enc.X25, -abi.RV64InvocationViewOffset)
	f.a.Sd(enc.X5, enc.X6, exceptionView)
	f.a.Ld(enc.X5, enc.X25, -40)
	f.a.Sd(enc.X5, enc.X6, exceptionControlOffset)
	f.a.Sd(enc.X24, enc.X6, exceptionHost)
	f.a.Sd(enc.X6, enc.X7, 0)
	e.active = append(e.active, exceptionActive{depth: depth, index: control.index})
}

func (f *integerEmitter) restoreExceptionHandlers(targetDepth int) {
	if f.exception == nil {
		return
	}
	for _, active := range f.exception.active {
		if active.depth >= targetDepth {
			f.exceptionAddress(enc.X6, f.exception.record(active.index))
			f.a.Ld(enc.X5, enc.X6, exceptionPrevious)
			f.exceptionCell(enc.X7)
			f.a.Sd(enc.X5, enc.X7, 0)
			return
		}
	}
}

func (f *integerEmitter) throwException(tag uint32, types []wasm.ValType, base, stride int) {
	f.throwExceptionAt(tag, types, base, func(index int) int32 { return f.SpillOffset(index * stride) })
}

func (f *integerEmitter) throwExceptionAt(tag uint32, types []wasm.ValType, base int, spill func(int) int32) {
	f.exceptionCell(enc.X7)
	f.a.Ld(enc.X6, enc.X7, 0)
	f.trap(trapExceptionUnhandled, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	f.a.Ld(enc.X7, enc.X25, -abi.EHTagDirPtrOffset)
	f.a.Constant64(enc.X5, int64(tag*8))
	f.a.Add(enc.X7, enc.X7, enc.X5)
	f.a.Ld(enc.X5, enc.X7, 0)
	f.a.Sd(enc.X5, enc.X6, exceptionTag)
	var written uint32
	for i, typ := range types {
		lane := exceptionPayloadLane(f.exception.module, types, i)
		written |= 1 << lane
		f.Load(5, spill(base+i), scalarWide(typ) || typ.Kind() == wasm.ValRef || wasm.EqualValType(typ, wasm.V128))
		f.a.Sd(enc.X5, enc.X6, exceptionPayload+int32(lane*8))
		if wasm.EqualValType(typ, wasm.V128) {
			f.Load(5, spill(base+i)+8, true)
			f.a.Sd(enc.X5, enc.X6, exceptionPayload+int32((lane+1)*8))
		}
	}
	for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
		if written&(1<<lane) == 0 {
			f.a.Sd(enc.Zero, enc.X6, exceptionPayload+int32(lane*8))
		}
	}
	f.jumpExceptionRecord(enc.X6)
}

func (f *integerEmitter) throwExceptionReference(slot int) {
	f.Load(5, f.SpillOffset(slot), true)
	f.trap(trapNullReference, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	if f.mode.ownedExceptionReferences {
		f.restoreCallTransport()
		f.a.Sd(enc.X5, enc.X10, 0)
		f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcExceptionPayload})
		f.restoreCallTransport()
		f.a.Ld(enc.X5, enc.X13, 0)
	}
	f.exceptionCell(enc.X7)
	f.a.Ld(enc.X6, enc.X7, 0)
	f.trap(trapExceptionUnhandled, f.a.BranchLong(enc.EQ, enc.X6, enc.Zero))
	for word := 0; word < 1+shared.EHPayloadLanes; word++ {
		f.a.Ld(enc.X7, enc.X5, int32(word*8))
		f.a.Sd(enc.X7, enc.X6, exceptionTag+int32(word*8))
	}
	f.jumpExceptionRecord(enc.X6)
}

func (f *integerEmitter) jumpExceptionRecord(record enc.Reg) {
	f.a.Ld(enc.Scratch0, record, exceptionSP)
	f.a.Ld(enc.X7, record, exceptionPC)
	f.a.Addi(enc.SP, enc.Scratch0, 0)
	f.a.Jalr(enc.Zero, enc.X7, 0)
}

func (f *integerEmitter) restoreExceptionContext(control *exceptionControl) {
	f.exceptionAddress(enc.X6, f.exception.record(control.index))
	f.a.Ld(enc.X8, enc.X6, exceptionFP)
	f.a.Ld(enc.X25, enc.X6, exceptionHome)
	f.a.Ld(enc.X28, enc.X6, exceptionContext)
	for _, field := range nativeImportContextFields {
		f.a.Ld(enc.X5, enc.X28, field.source)
		f.a.Sd(enc.X5, enc.X25, -field.destination)
	}
	f.a.Sd(enc.X28, enc.X25, -abi.RV64NativeContextOffset)
	f.a.Ld(enc.X5, enc.X6, exceptionView)
	f.a.Sd(enc.X5, enc.X25, -abi.RV64InvocationViewOffset)
	f.a.Ld(enc.X5, enc.X6, exceptionControlOffset)
	f.a.Sd(enc.X5, enc.X25, -40)
	f.a.Ld(enc.X24, enc.X6, exceptionHost)
}

func (f *integerEmitter) forwardException() {
	f.a.Ld(enc.X7, enc.X6, exceptionPrevious)
	f.trap(trapExceptionUnhandled, f.a.BranchLong(enc.EQ, enc.X7, enc.Zero))
	for word := 0; word < 1+shared.EHPayloadLanes; word++ {
		f.a.Ld(enc.X5, enc.X6, exceptionTag+int32(word*8))
		f.a.Sd(enc.X5, enc.X7, exceptionTag+int32(word*8))
	}
	f.exceptionCell(enc.X5)
	f.a.Sd(enc.X7, enc.X5, 0)
	f.jumpExceptionRecord(enc.X7)
}

func (f *integerEmitter) patchExceptionHandler(control *exceptionControl, position int) error {
	if err := f.Patch(control.site, position); err != nil {
		return err
	}
	word := f.a.B[control.site+4 : control.site+8]
	// JALR rd=x0 becomes ADDI rd=Scratch1 with the same immediate/base.
	value := binary.LittleEndian.Uint32(word)
	value = value&0xfff00000 | uint32(enc.Scratch1)<<15 | uint32(enc.Scratch1)<<7 | 0x13
	binary.LittleEndian.PutUint32(word, value)
	if f.exception == nil {
		return fmt.Errorf("rv64: exception handler without frame")
	}
	return nil
}

func parseExceptionCatches(r *wasm.Reader, m *wasm.Module, depth int, nextRoot *int) (*exceptionControl, error) {
	n, err := r.U32()
	if err != nil {
		return nil, err
	}
	if n > shared.MaxEHFrameRecords {
		return nil, unsupported("exception catch count")
	}
	control := &exceptionControl{index: -1, site: -1, clauses: make([]exceptionCatch, 0, n)}
	for i := uint32(0); i < n; i++ {
		kind, err := r.Byte()
		if err != nil {
			return nil, err
		}
		clause := exceptionCatch{kind: wasm.CatchKind(kind), root: -1}
		switch clause.kind {
		case wasm.CatchTag, wasm.CatchRef:
			clause.tag, err = r.U32()
			if err != nil {
				return nil, err
			}
			sig, err := exceptionTagSignature(m, clause.tag)
			if err != nil {
				return nil, err
			}
			clause.payload = sig.Params
		case wasm.CatchAll, wasm.CatchAllRef:
		default:
			return nil, unsupported("exception catch kind")
		}
		label, err := r.U32()
		if err != nil {
			return nil, err
		}
		if uint64(label) >= uint64(depth) {
			return nil, unsupported("exception catch label")
		}
		clause.target = depth - 1 - int(label)
		if clause.kind == wasm.CatchRef || clause.kind == wasm.CatchAllRef {
			clause.root = *nextRoot
			*nextRoot++
		}
		control.clauses = append(control.clauses, clause)
	}
	return control, nil
}

func (f *integerEmitter) emitExceptionHandler(control *exceptionControl, route func(*exceptionCatch) error) error {
	if err := f.patchExceptionHandler(control, f.Position()); err != nil {
		return err
	}
	f.restoreExceptionContext(control)
	for i := range control.clauses {
		clause := &control.clauses[i]
		if clause.kind == wasm.CatchAll || clause.kind == wasm.CatchAllRef {
			if err := route(clause); err != nil {
				return err
			}
			continue
		}
		f.a.Ld(enc.X5, enc.X6, exceptionTag)
		f.a.Ld(enc.X7, enc.X25, -abi.EHTagDirPtrOffset)
		f.a.Constant64(enc.Scratch0, int64(clause.tag*8))
		f.a.Add(enc.X7, enc.X7, enc.Scratch0)
		f.a.Ld(enc.X7, enc.X7, 0)
		unmatched := f.a.BranchLong(enc.NE, enc.X5, enc.X7)
		if err := route(clause); err != nil {
			return err
		}
		if err := f.Patch(unmatched, f.Position()); err != nil {
			return err
		}
	}
	f.forwardException()
	return nil
}

func (f *integerEmitter) copyExceptionPayload(clause *exceptionCatch, targetBase, stride int) {
	f.copyExceptionPayloadAt(clause, targetBase, func(index int) int32 { return f.SpillOffset(index * stride) })
}

func (f *integerEmitter) copyExceptionPayloadAt(clause *exceptionCatch, targetBase int, spill func(int) int32) {
	if clause.root >= 0 {
		f.exceptionAddress(enc.X28, f.exception.root(clause.root))
		for word := 0; word < 1+shared.EHPayloadLanes; word++ {
			f.a.Ld(enc.X5, enc.X6, exceptionTag+int32(word*8))
			f.a.Sd(enc.X5, enc.X28, int32(word*8))
		}
		if f.mode.ownedExceptionReferences {
			f.restoreCallTransport()
			f.a.Sd(enc.X28, enc.X10, 0)
			f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcExceptionAlloc, safepoint: f.recordGCFrameSafepointAt(clause.hiddenRefs, clause.collectorPC)})
			f.restoreCallTransport()
			f.a.Lwu(enc.X28, enc.X13, 0)
			f.exceptionCell(enc.X7)
			f.a.Ld(enc.X6, enc.X7, 0)
		}
	}
	// Allocation sees the handler-entry stack. Write the branch payload only
	// after it returns, so old operand roots still have their original homes.
	for i, typ := range clause.payload {
		lane := exceptionPayloadLane(f.exception.module, clause.payload, i)
		f.a.Ld(enc.X5, enc.X6, exceptionPayload+int32(lane*8))
		f.Store(spill(targetBase+i), 5, true)
		if wasm.EqualValType(typ, wasm.V128) {
			f.a.Ld(enc.X5, enc.X6, exceptionPayload+int32((lane+1)*8))
			f.Store(spill(targetBase+i)+8, 5, true)
		}
	}
	if clause.root >= 0 {
		f.Store(spill(targetBase+len(clause.payload)), 28, true)
		if f.mode.ownedExceptionReferences {
			f.exceptionAddress(enc.X28, f.exception.root(clause.root))
			for lane := 0; lane < shared.EHPayloadLanes; lane++ {
				f.a.Sd(enc.Zero, enc.X28, int32(8+lane*8))
			}
		}
	}
	f.a.Ld(enc.X5, enc.X6, exceptionPrevious)
	f.exceptionCell(enc.X7)
	f.a.Sd(enc.X5, enc.X7, 0)
}

func newExceptionFrame(m *wasm.Module, fn *wasm.Func, base int32) (*exceptionFrame, error) {
	if m == nil {
		return nil, nil
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	shape, err := shared.ScanEHFrameShape(&classifier, fn.BodyBytes)
	if err != nil {
		return nil, err
	}
	return &exceptionFrame{module: m, shape: shape, base: base}, nil
}

// Dead catch routes still own static allocation maps. Their helper sites sit
// behind an unconditional skip and cannot run or allocate.
func (f *integerEmitter) emitInactiveExceptionCopies(control *exceptionControl) {
	if !f.mode.ownedExceptionReferences {
		return
	}
	skip := f.Jump()
	for _, clause := range control.clauses {
		if clause.root >= 0 {
			f.restoreCallTransport()
			f.exceptionAddress(enc.X28, f.exception.root(clause.root))
			f.a.Sd(enc.X28, enc.X10, 0)
			f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), helper: gcExceptionAlloc, safepoint: f.recordGCFrameSafepoint(nil)})
		}
	}
	if err := f.Patch(skip, f.Position()); err != nil {
		f.err = err
	}
}
