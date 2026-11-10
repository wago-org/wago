// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// gcLifetimeRewrite is the complete persistent instruction plan. Descriptor's
// low 24 bits are the expanded opcode (0xfb0000|subopcode for GC operations);
// the high byte is the local or field index where the opcode has one.
type gcLifetimeRewrite struct {
	Offset     uint32
	Descriptor uint32
}

type gcLifetimePlan struct {
	body    []byte // immutable module-owned view; never copied into the plan
	context gcLifetimeContext
	policy  CodegenPolicy
	records []gcLifetimeRewrite // worker-owned until consumeGCLifetime returns
	typ     uint32
}

type gcLifetimeContext struct {
	fact       *gcLifetimeTypeFact
	paramTypes [gcLifetimeMaxSlots]wasm.ValType
	localTypes [gcLifetimeMaxSlots]wasm.ValType
	resultType wasm.ValType
	parameters uint8
	locals     uint8
	results    uint8
	function   int
	pcBase     uint32
	bodyHash   uint64
}

type gcLifetimeEmissionRecord struct {
	pc, end    int
	kind       byte // 0: erased alias/nop, 1: ordinary scalar, 2: forwarded scalar, 3: constructor
	op         byte
	value      storage
	defaultNew bool
}

func lifetimeBodyHash(body []byte) uint64 {
	const prime = 1099511628211
	h := uint64(14695981039346656037)
	for _, b := range body {
		h = (h ^ uint64(b)) * prime
	}
	return h
}

func (f *fn) sealGCLifetime(code []byte) *gcLifetimePlan {
	if !gc.NativeDeadBatchSupported || !f.opt(optGCLifetimeAnalyze) || !f.opt(optGCNativeAlloc) || !f.gcStructHelpers || f.moduleEH || f.inlineDepth != 0 || len(f.customInstructions) != 0 || profileEnabled {
		return nil
	}
	i := f.globalIdx - f.m.ImportedFuncCount()
	if i < 0 || i >= gcLifetimeMaxFunctions || i >= len(f.m.Code) || !bytes.Equal(code, f.m.Code[i].BodyBytes) {
		return nil
	}
	proof := analyzeGCLifetimeWithRecords(f.m, i, gcLifetimeMaxRecords, nil, false)
	if !proof.Candidate {
		return nil
	}
	sc := f.scratchState()
	if sc.gcLifetimeFacts == nil {
		sc.gcLifetimeFacts = newGCLifetimeModuleFacts()
	}
	fact, ok := sc.gcLifetimeFacts.typeFact(f, proof.Type)
	if !ok || !fact.layout.pointerFree || len(f.ft.Params) > gcLifetimeMaxSlots || len(f.ft.Results) > 1 {
		return nil
	}
	p := sc.acquireGCLifetimePlan()
	if p == nil {
		return nil
	}
	records := sc.prepareGCLifetimeRecords(proof.Rewrites)
	if records == nil && proof.Rewrites != 0 {
		sc.releaseGCLifetimePlan(p)
		return nil
	}
	filled := analyzeGCLifetimeWithRecords(f.m, i, gcLifetimeMaxRecords, records, false)
	if !filled.Candidate || filled.Rewrites != proof.Rewrites || filled.Type != proof.Type {
		sc.releaseGCLifetimePlan(p)
		return nil
	}
	p.body, p.policy, p.records, p.typ = f.m.Code[i].BodyBytes, f.policy, records, proof.Type
	ctx := &p.context
	ctx.fact, ctx.function, ctx.pcBase, ctx.bodyHash = fact, i, f.tracePCBase, lifetimeBodyHash(code)
	ctx.parameters, ctx.results = uint8(len(f.ft.Params)), uint8(len(f.ft.Results))
	copy(ctx.paramTypes[:], f.ft.Params)
	n := len(f.ft.Params)
	copy(ctx.localTypes[:], f.ft.Params)
	for _, run := range f.m.Code[i].Locals.Runs {
		if run.Count > uint32(gcLifetimeMaxSlots-n) {
			sc.releaseGCLifetimePlan(p)
			return nil
		}
		for range run.Count {
			ctx.localTypes[n] = run.Type
			n++
		}
	}
	ctx.locals = uint8(n)
	if len(f.ft.Results) != 0 {
		ctx.resultType = f.ft.Results[0]
	}
	return p
}

func (sc *scratch) acquireGCLifetimePlan() *gcLifetimePlan {
	if sc.gcLifetimePlanLive || sc.gcLifetimeRecordsLive {
		return nil
	}
	if sc.gcLifetimePlan == nil {
		sc.gcLifetimePlan = new(gcLifetimePlan)
	}
	*sc.gcLifetimePlan = gcLifetimePlan{}
	sc.gcLifetimePlanLive = true
	return sc.gcLifetimePlan
}

func (sc *scratch) prepareGCLifetimeRecords(count int) []gcLifetimeRewrite {
	if count < 0 || count > gcLifetimeMaxRecords || !sc.gcLifetimePlanLive || sc.gcLifetimeRecordsLive {
		return nil
	}
	if cap(sc.gcLifetimeRecords) < count {
		sc.gcLifetimeRecords = make([]gcLifetimeRewrite, count)
	} else {
		sc.gcLifetimeRecords = sc.gcLifetimeRecords[:count]
		clear(sc.gcLifetimeRecords)
	}
	sc.gcLifetimeRecordsLive = true
	return sc.gcLifetimeRecords
}

func (sc *scratch) releaseGCLifetimeRecords() {
	sc.gcLifetimeRecords = sc.gcLifetimeRecords[:0]
	sc.gcLifetimeRecordsLive = false
}

func (sc *scratch) releaseGCLifetimePlan(p *gcLifetimePlan) {
	if !sc.gcLifetimePlanLive {
		return
	}
	if p != sc.gcLifetimePlan {
		panic("amd64: released foreign GC lifetime plan")
	}
	if sc.gcLifetimePending == p {
		sc.gcLifetimePending = nil
	}
	*p = gcLifetimePlan{}
	sc.releaseGCLifetimeRecords()
	sc.gcLifetimePlanLive = false
}

func (f *fn) replayGCLifetime(p *gcLifetimePlan, code []byte) bool {
	return p != nil && p.contextMatches(f, code) && p.walk(nil)
}

func (p *gcLifetimePlan) contextMatches(f *fn, code []byte) bool {
	ctx := &p.context
	if ctx.fact == nil || !gc.NativeDeadBatchSupported || !f.opt(optGCLifetimeAnalyze) ||
		!f.opt(optGCNativeAlloc) || !f.gcStructHelpers || f.moduleEH || f.inlineDepth != 0 ||
		len(f.customInstructions) != 0 || profileEnabled || p.policy != f.policy || ctx.pcBase != f.tracePCBase ||
		ctx.function != f.globalIdx-f.m.ImportedFuncCount() || ctx.function < 0 || ctx.function >= len(f.m.Code) ||
		len(code) != len(p.body) || lifetimeBodyHash(code) != ctx.bodyHash ||
		!bytes.Equal(code, p.body) || !bytes.Equal(code, f.m.Code[ctx.function].BodyBytes) ||
		len(f.ft.Params) != int(ctx.parameters) || len(f.ft.Results) != int(ctx.results) {
		return false
	}
	for i, typ := range f.ft.Params {
		if typ != ctx.paramTypes[i] {
			return false
		}
	}
	if ctx.results != 0 && f.ft.Results[0] != ctx.resultType {
		return false
	}
	n := len(f.ft.Params)
	for _, run := range f.m.Code[ctx.function].Locals.Runs {
		for range run.Count {
			if n >= int(ctx.locals) || ctx.localTypes[n] != run.Type {
				return false
			}
			n++
		}
	}
	return n == int(ctx.locals) && ctx.fact.matches(f, p.typ)
}

// walk independently decodes the source and consumes compact records
// monotonically. Its fixed locals/operand/field workspaces are attempt-local;
// no second plan or pointer-bearing origin graph is retained.
func (p *gcLifetimePlan) walk(emit func(gcLifetimeEmissionRecord) bool) bool {
	type val struct {
		object bool
		scalar storage
	}
	var locals, stack [gcLifetimeMaxSlots]val
	var fields [gcLifetimeMaxFields]storage
	for i := 0; i < int(p.context.locals); i++ {
		if i < int(p.context.parameters) {
			locals[i].scalar = storage{kind: stLocalRef, typ: lifetimeMachineType(p.context.localTypes[i]), idx: uint32(i)}
		} else {
			locals[i].scalar = zeroStorage(lifetimeMachineType(p.context.localTypes[i]))
		}
	}
	sp, cursor := 0, 0
	push := func(v val) bool {
		if sp == len(stack) {
			return false
		}
		stack[sp] = v
		sp++
		return true
	}
	pop := func() (val, bool) {
		if sp == 0 {
			return val{}, false
		}
		sp--
		return stack[sp], true
	}
	consume := func(offset uint32, descriptor uint32, rewrite bool) bool {
		if cursor == len(p.records) {
			return !rewrite
		}
		rec := p.records[cursor]
		if !rewrite {
			return rec.Offset > offset
		}
		if rec.Offset != offset || rec.Descriptor != descriptor {
			return false
		}
		cursor++
		return true
	}
	r := wasm.ReaderFrom(p.body)
	for r.HasNext() {
		rec := gcLifetimeEmissionRecord{pc: r.Offset()}
		op, err := r.Byte()
		if err != nil {
			return false
		}
		rec.op = op
		rewrite, descriptor := false, uint32(op)
		switch op {
		case 0x01:
		case 0x0b:
			rec.kind = 1
			if r.HasNext() || sp != int(p.context.results) {
				return false
			}
		case 0x41, 0x42, 0x43, 0x44:
			v := storage{kind: stConst}
			switch op {
			case 0x41:
				x, e := r.I32()
				if e != nil {
					return false
				}
				v.typ, v.cval = mtI32, int64(x)
			case 0x42:
				x, e := r.I64()
				if e != nil {
					return false
				}
				v.typ, v.cval = mtI64, x
			case 0x43:
				x, e := r.LEU32()
				if e != nil {
					return false
				}
				v.typ, v.cval = mtF32, int64(x)
			case 0x44:
				x, e := r.LEU64()
				if e != nil {
					return false
				}
				v.typ, v.cval = mtF64, int64(x)
			}
			if !push(val{scalar: v}) {
				return false
			}
			rec.kind, rec.value = 2, v
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil || idx >= uint32(p.context.locals) {
				return false
			}
			descriptor |= idx << 24
			if op == 0x20 {
				v := locals[idx]
				if !push(v) {
					return false
				}
				if v.object {
					rewrite = true
				} else {
					rec.kind, rec.value = 2, v.scalar
				}
			} else {
				v, ok := pop()
				if !ok || idx < uint32(p.context.parameters) {
					return false
				}
				locals[idx] = v
				if op == 0x22 && !push(v) {
					return false
				}
				if v.object {
					rewrite = true
				} else {
					rec.kind = 1
				}
			}
		case 0x1a:
			v, ok := pop()
			if !ok {
				return false
			}
			if v.object {
				rewrite = true
			} else {
				rec.kind = 1
			}
		case 0xfb:
			sub, e := r.U32()
			if e != nil || sub > 2 {
				return false
			}
			ti, e := r.U32()
			if e != nil || ti != p.typ {
				return false
			}
			descriptor = 0xfb0000 | sub
			rewrite = true
			if sub == 2 {
				fi, e := r.U32()
				if e != nil || fi >= uint32(len(p.context.fact.typ.Comp.Fields)) {
					return false
				}
				descriptor |= fi << 24
				v, ok := pop()
				if !ok || !v.object {
					return false
				}
				value := fields[fi]
				if !push(val{scalar: value}) {
					return false
				}
				rec.kind, rec.value = 2, value
			} else {
				rec.kind, rec.defaultNew = 3, sub == 1
				for j := len(p.context.fact.typ.Comp.Fields) - 1; j >= 0; j-- {
					if sub == 0 {
						v, ok := pop()
						if !ok || v.object {
							return false
						}
						fields[j] = v.scalar
					} else {
						fields[j] = zeroStorage(lifetimeMachineType(p.context.fact.typ.Comp.Fields[j].Storage().Val()))
					}
				}
				if !push(val{object: true}) {
					return false
				}
			}
		default:
			return false
		}
		if !consume(uint32(rec.pc), descriptor, rewrite) {
			return false
		}
		rec.end = r.Offset()
		if emit != nil && !emit(rec) {
			return false
		}
	}
	return cursor == len(p.records)
}

// A rejected certificate emits nothing: the caller continues ordinary lowering.
func (f *fn) consumeGCLifetime(p *gcLifetimePlan, code []byte) (bool, error) {
	// The private candidate emitter independently decodes the complete body and
	// consumes every sealed record. A separate proof-only walk repeats that same
	// bounded decode and record validation without strengthening publication:
	// any mismatch from the emitting walk already fails this private attempt and
	// leaves compileFunc's retained baseline selected.
	valid := p != nil && p.contextMatches(f, code)
	if valid && (f.sc == nil || !f.sc.gcLifetimeCandidate) {
		valid = p.walk(nil)
	}
	if !valid {
		if f.sc != nil {
			f.sc.releaseGCLifetimePlan(p)
		}
		return false, nil
	}
	err := f.emitGCLifetime(p)
	if f.sc != nil {
		f.sc.releaseGCLifetimePlan(p)
	}
	return true, err
}

func lifetimeMachineType(t wasm.ValType) machineType {
	switch t {
	case wasm.I32:
		return mtI32
	case wasm.I64:
		return mtI64
	case wasm.F32:
		return mtF32
	case wasm.F64:
		return mtF64
	}
	return mtI64
}
