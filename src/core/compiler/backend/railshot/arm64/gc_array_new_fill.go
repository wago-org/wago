//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// gcArrayNewFillPlan describes a fixed adjacent constructor/fill sequence. It
// is compiler-local lookahead state and is never retained in fn or scratch.
type gcArrayNewFillPlan struct {
	objectLocal uint32
	sourceLocal uint32
	sourcePos   int
	afterSet    wasm.Reader
	afterFill   wasm.Reader
	afterRead   wasm.Reader
	forwardRead bool
	deadObject  bool
	forward     storage
}

func (f *fn) matchGCArrayNewFill(r *wasm.Reader, typeIndex uint32, field wasm.FieldType) (gcArrayNewFillPlan, bool) {
	if f.localBase != 0 || field.Mut() != wasm.Var || field.Storage().Packed() ||
		field.Storage().Val().Kind() != wasm.ValNum || field.Storage().Val().Num() != wasm.NumI32 {
		return gcArrayNewFillPlan{}, false
	}
	typ, ok := f.stagedArraySubtype(typeIndex)
	if !ok || !typ.Final {
		return gcArrayNewFillPlan{}, false
	}
	countRoot := f.s.back()
	if countRoot == nil || countRoot.elemKind() != ekValue || countRoot.st.kind != stConst || countRoot.st.typ != mtI32 {
		return gcArrayNewFillPlan{}, false
	}
	count := uint32(countRoot.st.cval)
	if count == 0 || count > 64 {
		return gcArrayNewFillPlan{}, false
	}
	valueRoot := f.s.prev(f.s.baseOfValentBlock(countRoot))
	if valueRoot == nil || valueRoot.elemKind() != ekValue || valueRoot.st.kind != stConst || valueRoot.st.typ != mtI32 || valueRoot.st.cval != 0 {
		return gcArrayNewFillPlan{}, false
	}

	look := *r
	if op, err := look.Byte(); err != nil || op != 0x21 { // local.set object
		return gcArrayNewFillPlan{}, false
	}
	objectLocal, err := look.U32()
	if err != nil || int(objectLocal) >= len(f.localType) || f.localType[objectLocal] != mtI64 {
		return gcArrayNewFillPlan{}, false
	}
	afterSet := look
	if op, err := look.Byte(); err != nil || op != 0x20 { // local.get object
		return gcArrayNewFillPlan{}, false
	}
	gotObject, err := look.U32()
	if err != nil || gotObject != objectLocal {
		return gcArrayNewFillPlan{}, false
	}
	if op, err := look.Byte(); err != nil || op != 0x41 { // i32.const 0 start
		return gcArrayNewFillPlan{}, false
	}
	start, err := look.I32()
	if err != nil || start != 0 {
		return gcArrayNewFillPlan{}, false
	}
	if op, err := look.Byte(); err != nil || op != 0x20 { // local.get fill value
		return gcArrayNewFillPlan{}, false
	}
	sourceLocal, err := look.U32()
	if err != nil || sourceLocal == objectLocal || int(sourceLocal) >= len(f.localType) || f.localType[sourceLocal] != mtI32 {
		return gcArrayNewFillPlan{}, false
	}
	sourcePos := look.Offset()
	if op, err := look.Byte(); err != nil || op != 0x41 { // i32.const count
		return gcArrayNewFillPlan{}, false
	}
	fillCount, err := look.I32()
	if err != nil || uint32(fillCount) != count {
		return gcArrayNewFillPlan{}, false
	}
	if op, err := look.Byte(); err != nil || op != 0xfb {
		return gcArrayNewFillPlan{}, false
	}
	sub, err := look.U32()
	if err != nil || sub != 16 { // array.fill
		return gcArrayNewFillPlan{}, false
	}
	fillType, err := look.U32()
	if err != nil || fillType != typeIndex {
		return gcArrayNewFillPlan{}, false
	}
	plan := gcArrayNewFillPlan{objectLocal: objectLocal, sourceLocal: sourceLocal, sourcePos: sourcePos, afterSet: afterSet, afterFill: look}
	read := look
	if op, err := read.Byte(); err == nil && op == 0x20 {
		if object, err := read.U32(); err == nil && object == objectLocal {
			plan.afterRead, plan.forwardRead = matchGCBoundedArrayGet(&read, typeIndex, count)
		}
	}
	plan.deadObject = plan.forwardRead && f.gcFillLocalConfined(objectLocal)
	return plan, true
}

func (p *gcArrayNewFillPlan) replaceInitializer(f *fn) {
	count := f.popValue()
	f.popValue() // constructor's constant-zero initializer
	f.pushI32LocalValue(int(p.sourceLocal), p.sourcePos)
	if p.forwardRead {
		value := f.s.back()
		p.forward = value.st
		switch p.forward.kind {
		case stConst:
		case stLocalRef, stLocalReg:
			p.forward.kind, p.forward.reg = stLocalRef, regNone
		default:
			p.forwardRead = false
		}
	}

	f.pushValue(count.st)
}

// pushI32LocalValue mirrors local.get's state-aware storage selection. The
// fusion moves this read before array.new, so it must also preserve interval
// ownership, pinned dirty values, and lazy-zero locals.
func (f *fn) pushI32LocalValue(x, pos int) {
	f.activateIntervalLocal(x, pos, true)
	if reg, ok := f.takeFinalIntervalGet(x, pos); ok {
		f.pushReg(reg, mtI32)
		return
	}
	if f.localConstZero(x) {
		if pr, _, ok := f.pinReg(x); ok {
			f.recoverLocal(x)
			f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: pr, idx: uint32(x)})
		} else {
			f.pushValue(zeroStorage(mtI32))
		}
		return
	}
	if pr, _, ok := f.pinReg(x); ok {
		f.recoverLocal(x)
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: pr, idx: uint32(x)})
		return
	}
	f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: uint32(x)})
}

func (p gcArrayNewFillPlan) finish(f *fn, r *wasm.Reader) {
	if p.deadObject && p.forwardRead {
		f.dropValue()
		f.stats.peep("gc-dead-fill-local")
	} else {
		f.prepareStoreForward(0x21)
		if p.objectLocal < 64 {
			f.localWritten |= 1 << p.objectLocal
		}
		f.setLocal(&p.afterSet, int(p.objectLocal), false)
	}
	*r = p.afterFill
	if p.forwardRead {
		f.pushValue(p.forward)
		*r = p.afterRead
		f.stats.peep("gc-fresh-array-get-forward")
	}

	f.stats.peep("gc-array-new-fill-fusion")
}

// gcFillLocalConfined proves that the constructor/fill/read sequence owns every
// use of this default-null local. Its only write and two reads are adjacent in
// the matched sequence, so neither branches nor loop backedges can expose an
// earlier identity. Removing the write keeps the local null at safepoints.
// Limit lookahead work independently of module size.
func (f *fn) gcFillLocalConfined(local uint32) bool {
	if f.m == nil || int(local) < f.nParams || len(f.customInstructions) != 0 {
		return false
	}
	index := int(f.traceFuncIdx) - f.m.ImportedFuncCount()
	if index < 0 || index >= len(f.m.Code) {
		return false
	}
	code := f.m.Code[index].BodyBytes
	if len(code) == 0 || len(code) > 4096 {
		return false
	}
	r := wasm.ReaderFrom(code)
	gets, sets := 0, 0
	for {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x20 || op == 0x21 || op == 0x22 {
			x, err := r.U32()
			if err != nil {
				return false
			}
			if x == local {
				switch op {
				case 0x20:
					gets++
				case 0x21:
					sets++
				default:
					return false
				}
				if gets > 2 || sets > 1 {
					return false
				}
			}
		} else if err := skipImmediatesWithMemory64(&r, op, f.memoryAddr64(0)); err != nil {
			return false
		}
		if _, more := r.Peek(); !more {
			break
		}
	}
	return gets == 2 && sets == 1
}
