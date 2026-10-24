//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Ordinary GP registers/spills keep an explicit negative value ID in otherwise
// unused cval; positive vector-tee local hints and constant payloads are distinct.
// Borrowed locals use declared Wasm reference types, not liveness/root flags:
// call-free functions often need no source GC frame plan at all.
// Facts are compiler scratch, not a runtime lookup table. Each proved use
// directly loads its address home. Homes contain raw addresses and are never roots.
const gcValueCheckCapacity = 8
const gcValuePlanWorkingSet = 256
const gcValuePlanProducerLimit = 4096
const gcValueChecksAvailable = true

type gcValueCheck struct {
	value, typ, extent uint32
	used               bool
}
type gcFieldValue struct{ parent, typ, field, value uint32 }
type gcValueChecks struct {
	locals          []uint32
	touched         []uint32
	localRefs       []uint64
	serial          uint32
	entries         [gcValueCheckCapacity]gcValueCheck
	fields          [16]gcFieldValue
	fieldNext, next int
	home            int
	producers       map[uint32]bool
}

func (s *gcValueChecks) nextID() uint32 {
	s.serial++
	if s.serial == 0 {
		panic("amd64: GC value identity overflow")
	}
	return s.serial
}
func (s *gcValueChecks) clear() {
	clear(s.entries[:])
	clear(s.fields[:])
	// Reset work follows used locals, not declared locals times control edges.
	for _, i := range s.touched {
		s.locals[i] = 0
	}
	s.touched = s.touched[:0]
	s.next = 0
	s.fieldNext = 0
}
func (s *gcValueChecks) setLocalValue(i int, value uint32) {
	if value == 0 {
		panic("amd64: cannot assign zero GC value identity")
	}
	if s.locals[i] == 0 {
		s.touched = append(s.touched, uint32(i))
	}
	s.locals[i] = value
}
func (s *gcValueChecks) find(value, typ uint32) *gcValueCheck {
	if value == 0 {
		return nil
	}
	for i := range s.entries {
		e := &s.entries[i]
		if e.used && e.value == value && e.typ == typ {
			return e
		}
	}
	return nil
}
func (s *gcValueChecks) fieldValue(parent, typ, field uint32) uint32 {
	if parent == 0 {
		return 0
	}
	for _, v := range s.fields {
		if v.value != 0 && v.parent == parent && v.typ == typ && v.field == field {
			return v.value
		}
	}
	v := s.nextID()
	s.fields[s.fieldNext%len(s.fields)] = gcFieldValue{parent, typ, field, v}
	s.fieldNext++
	return v
}
func (f *fn) gcLocalValueID(i int) uint32 {
	s := f.gcValues
	if s == nil || i < 0 || i >= len(s.locals) || i/64 >= len(s.localRefs) || s.localRefs[i/64]&(uint64(1)<<uint(i%64)) == 0 {
		return 0
	}
	if s.locals[i] == 0 {
		s.setLocalValue(i, s.nextID())
	}
	return s.locals[i]
}
func (f *fn) gcValueID(e *elem) uint32 {
	s := f.gcValues
	if s == nil || e == nil || !e.isValue() || e.st.typ != mtI64 {
		return 0
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return f.gcLocalValueID(e.st.index())
	case stReg, stSlot:
		if e.st.cval < 0 && e.st.cval >= -int64(math.MaxUint32) {
			return uint32(-e.st.cval)
		}
		return 0
	}
	return 0
}
func (f *fn) setGCValueID(e *elem, id uint32) {
	if f.gcValues != nil && e != nil && e.isValue() && e.st.typ == mtI64 && (e.st.kind == stReg || e.st.kind == stSlot) {
		if id != 0 {
			e.st.cval = -int64(id)
		}
	}
}
func (f *fn) clearGCValueChecks() {
	if f.gcValues != nil {
		f.gcValues.clear()
	}
}
func (f *fn) prepareGCValueChecks(op byte) {
	if f.gcValues == nil {
		return
	}
	if f.inlineDepth != 0 {
		f.clearGCValueChecks()
		return
	}
	switch {
	case op == 0x01 || op == 0x1a || op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4 || op == 0xfb:
		return
	default:
		f.clearGCValueChecks()
	}
}
func (f *fn) prepareGCValueFB(sub uint32) {
	if sub != 2 && sub != 3 && sub != 4 && sub != 20 && sub != 21 && sub != 22 && sub != 23 {
		f.clearGCValueChecks()
	}
}
func (f *fn) gcValueHome(entry int, limit bool) int32 {
	i := f.gcValues.home + 2*entry
	if limit {
		i++
	}
	// These compiler-only homes never take part in source-local slot packing.
	// Marking them in its source-local recorder would overflow that recorder
	// and disable packing for the whole function. Their offsets remain fixed.
	off := f.localOff(i)
	if diagnosticsEnabled && f.stats != nil {
		f.stats.Encoding.RecordLocalFrameAddress(off)
	}
	return off
}
func (f *fn) recordGCValueCheck(value, typ, extent uint32, address, limit Reg) {
	s := f.gcValues
	if s == nil || value == 0 || f.inlineDepth != 0 || !s.producers[f.wasmPC] {
		return
	}
	index := s.next % len(s.entries)
	s.next++
	for i, e := range s.entries {
		if e.used && e.value == value && e.typ == typ {
			index = i
			break
		}
	}
	// Publish the compiler fact only after both runtime stores are emitted.
	f.a.Store64(RSP, f.gcValueHome(index, false), address)
	f.a.Store32(RSP, f.gcValueHome(index, true), limit)
	s.entries[index] = gcValueCheck{value, typ, extent, true}
	f.stats.peep("gc-value-check-save")
}
func (f *fn) reuseGCValueCheck(value, typ, extent uint32) bool {
	s := f.gcValues
	if s == nil || f.inlineDepth != 0 {
		return false
	}
	e := s.find(value, typ)
	if e == nil {
		return false
	}
	index := -1
	for i := range s.entries {
		if &s.entries[i] == e {
			index = i
			break
		}
	}
	if index < 0 {
		panic("amd64: GC value fact has no private home")
	}
	if extent > e.extent {
		f.a.Load32(RAX, RSP, f.gcValueHome(index, true))
		f.a.AluRI(cmpDigit, RAX, int32(extent), false)
		f.trapIf(condB, trapCastFailure)
		e.extent = extent
		f.stats.peep("gc-value-check-extend")
	}
	f.a.Load64(RAX, RSP, f.gcValueHome(index, false))
	f.stats.peep("gc-value-check-reuse")
	f.stats.addGCHandleResolutionReuse()
	return true
}

// This linear source pass decides which producers merit a home store. It is
// only a profitability filter: the emitter's actual value/effect facts authorize
// each reuse. Unknown operations end the source region. No function names or
// numeric declaration identities select an optimization.
func (f *fn) planGCValueChecks(code []byte, declBytes uint32) map[uint32]bool {
	type key struct{ value, typ uint32 }
	type producer struct{ pc uint32 }
	seen := make(map[key]producer)
	fields := make(map[gcFieldValue]uint32)
	locals := make([]uint32, f.nLocals)
	var touched []uint32
	setLocal := func(i int, value uint32) {
		if locals[i] == 0 {
			touched = append(touched, uint32(i))
		}
		locals[i] = value
	}
	next := uint32(0)
	fresh := func() uint32 { next++; return next }
	clearRegion := func() {
		clear(seen)
		clear(fields)
		for _, i := range touched {
			locals[i] = 0
		}
		touched = touched[:0]
	}
	planned := make(map[uint32]bool)
	r := wasm.ReaderFrom(code)
	var last uint32
	var cast key
	var castPC uint32
	for r.BytesLeft() > 0 {
		pc := uint32(r.Offset()) + declBytes
		op, err := r.Byte()
		if err != nil {
			return nil
		}
		rr := r
		var imm wasm.InstructionImmediate
		if err = f.classifier.ClassifyInto(&r, op, &imm); err != nil {
			return nil
		}
		if op == 0x20 {
			i := int(imm.Index)
			if i >= len(locals) {
				return nil
			}
			if locals[i] == 0 {
				setLocal(i, fresh())
			}
			last = locals[i]
			cast = key{}
			continue
		}
		if op == 0x21 || op == 0x22 {
			i := int(imm.Index)
			if i >= len(locals) {
				return nil
			}
			if last == 0 {
				last = fresh()
			}
			setLocal(i, last)
			if op == 0x21 {
				last = 0
			}
			cast = key{}
			continue
		}
		if op == 0xfb && (imm.Subopcode == 22 || imm.Subopcode == 23) {
			_, _ = rr.U32()
			heap, exact, e := readRefHeapTypeImmediate(&rr)
			if e == nil && !exact && heap >= 0 && uint64(heap) <= math.MaxUint32 && last != 0 {
				st, ok := f.stagedStructType(uint32(heap))
				if ok && st.Final {
					cast = key{last, uint32(heap)}
					castPC = pc
					continue
				}
			}
			last = 0
			cast = key{}
			continue
		}
		if op == 0xfb && imm.Subopcode >= 2 && imm.Subopcode <= 4 {
			parent := last
			if cast.value != 0 && cast.typ == imm.Index {
				if previous, ok := seen[cast]; ok {
					if len(planned) < gcValuePlanProducerLimit {
						planned[previous.pc] = true
					}
				}
				if len(seen) >= gcValuePlanWorkingSet {
					clear(seen)
				}
				seen[cast] = producer{castPC}
			}
			last = 0
			if layout, _, ok := f.gcStructFieldLayout(imm.Index, imm.Index2); ok && layout.CollectorRef && parent != 0 {
				k := gcFieldValue{parent: parent, typ: imm.Index, field: imm.Index2}
				last = fields[k]
				if last == 0 {
					last = fresh()
					if len(fields) >= gcValuePlanWorkingSet {
						clear(fields)
					}
					fields[k] = last
				}
			}
			cast = key{}
			continue
		}
		cast = key{}
		last = 0
		if op == 0xfb && (imm.Subopcode == 20 || imm.Subopcode == 21) || op == 0x01 || op == 0x1a || op >= 0x41 && op <= 0xc4 {
			continue
		}
		clearRegion()
	}
	if len(planned) == 0 {
		return nil
	}
	return planned
}
func (f *fn) reserveGCValueChecks(code []byte, declBytes uint32) error {
	if !f.gcStructHelpers || f.moduleEH || len(f.customInstructions) != 0 {
		return nil
	}
	producers := f.planGCValueChecks(code, declBytes)
	if len(producers) == 0 {
		return nil
	}
	s := &f.sc.gcValueScratch
	oldLocals, oldRefs, oldTouched := s.locals, s.localRefs, s.touched
	*s = gcValueChecks{home: len(f.localType), producers: producers}
	if cap(oldTouched) <= 4096 {
		s.touched = oldTouched[:0]
	}
	count := len(f.localType)
	if cap(oldLocals) >= count && cap(oldLocals) <= 4096 {
		s.locals = oldLocals[:count]
		clear(s.locals)
	} else {
		s.locals = make([]uint32, count)
	}
	words := (count + 63) / 64
	if cap(oldRefs) >= words && cap(oldRefs) <= 64 {
		s.localRefs = oldRefs[:words]
		clear(s.localRefs)
	} else {
		s.localRefs = make([]uint64, words)
	}
	addRef := func(i int, t wasm.ValType) {
		if gcFrameRefType(f.m, t) {
			s.localRefs[i/64] |= uint64(1) << uint(i%64)
		}
	}
	i := 0
	for _, t := range f.ft.Params {
		addRef(i, t)
		i++
	}
	for _, run := range f.m.Code[f.globalIdx-f.importedFunctionCount()].Locals.Runs {
		for j := uint32(0); j < run.Count; j++ {
			addRef(i, run.Type)
			i++
		}
	}
	f.gcValues = s
	f.stats.peep("gc-value-check-plan")
	for i := 0; i < 2*gcValueCheckCapacity; i++ {
		home := len(f.localType)
		for len(f.gcLocalTypes) <= home/64 {
			f.gcLocalTypes = append(f.gcLocalTypes, 0)
		}
		f.gcLocalTypes[home/64] &^= uint64(1) << uint(home%64)
		if uint64(f.nLocalSlots)*8 > uint64(localSlotOffsetMask) {
			return fmt.Errorf("amd64: GC value homes exceed local frame range")
		}
		f.localType = append(f.localType, mtI64)
		f.localSlot = append(f.localSlot, uint32(8*f.nLocalSlots))
		f.locals = append(f.locals, localDef{reg: regNone, typ: mtI64, state: lsMem})
		f.nLocalSlots++
	}
	f.skipFence = false
	return nil
}

// The saved limit must include both independently checked extent sources.
func emitGCValueLimit(a *fn, handle, header, out Reg) {
	a.a.MovRegReg32(out, handle)
	a.a.Cmp32(header, out)
	skip := a.a.JccPlaceholder(condAE)
	a.a.MovRegReg32(out, header)
	a.a.PatchRel32(skip, a.a.Len())
}
