//go:build amd64

package amd64

import (
	"reflect"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This certificate owns complete semantic records and a structural snapshot,
// never the diagnostic Identity hash. Replay finishes before any emission.
// Plans belong to this function attempt; retries/workers cannot share them.
type gcLifetimePlan struct {
	body    []byte
	context gcLifetimeContext
	policy  CodegenPolicy
	records []gcLifetimeRecord
	typ     uint32
}
type gcLifetimeContext struct {
	typ       wasm.SubType
	signature wasm.CompType
	locals    []wasm.LocalRun
	layout    nativeGCStructAllocPlan
	function  int
	pcBase    uint32
}

func lifetimeCompCopy(c wasm.CompType) wasm.CompType {
	c.Fields = append([]wasm.FieldType(nil), c.Fields...)
	c.Params = append([]wasm.ValType(nil), c.Params...)
	c.Results = append([]wasm.ValType(nil), c.Results...)
	return c
}

type gcLifetimeRecord struct {
	pc, end    int
	kind       byte // 0: erased alias, 1: ordinary scalar, 2: forwarded scalar, 3: constructor
	op         byte
	value      storage
	defaultNew bool
}

func (f *fn) sealGCLifetime(code []byte) *gcLifetimePlan {
	if !gc.NativeDeadBatchSupported || !f.opt(optGCLifetimeAnalyze) || !f.opt(optGCNativeAlloc) || !f.gcStructHelpers || f.moduleEH || f.inlineDepth != 0 || len(f.customInstructions) != 0 || profileEnabled {
		return nil
	}
	i := f.globalIdx - f.m.ImportedFuncCount()
	if i < 0 || i >= gcLifetimeMaxFunctions {
		return nil
	}
	proof := analyzeGCLifetime(f.m, i, gcLifetimeMaxRecords)
	if !proof.Candidate || !reflect.DeepEqual(code, f.m.Code[i].BodyBytes) {
		return nil
	}
	layout, ok := f.nativeGCStructAllocLayout(proof.Type)
	if !ok || !layout.pointerFree {
		return nil
	}
	st, ok := f.stagedStructType(proof.Type)
	source, found := f.m.TypeSubType(proof.Type)
	if !ok || !found || !reflect.DeepEqual(st, *source) {
		return nil
	}
	// Include the full selected native layout, function signature/local declarations,
	// and immutable option snapshot, not a digest or producer-supplied offsets.
	st.Comp = lifetimeCompCopy(st.Comp)
	st.Supers = append([]wasm.TypeIdx(nil), st.Supers...)
	layout.fields = append(layout.fields[:0:0], layout.fields...)
	p := &gcLifetimePlan{body: append([]byte(nil), code...), policy: f.policy, typ: proof.Type,
		context: gcLifetimeContext{typ: st, signature: lifetimeCompCopy(*f.ft), locals: append([]wasm.LocalRun(nil), f.m.Code[i].Locals.Runs...), layout: layout, function: i, pcBase: f.tracePCBase}}
	type val struct {
		object bool
		scalar storage
	}
	var locals [gcLifetimeMaxSlots]val
	for j, t := range f.ft.Params {
		locals[j].scalar = storage{kind: stLocalRef, typ: lifetimeMachineType(t), idx: uint32(j)}
	}
	n := len(f.ft.Params)
	for _, run := range f.m.Code[i].Locals.Runs {
		for range run.Count {
			locals[n].scalar = zeroStorage(lifetimeMachineType(run.Type))
			n++
		}
	}
	stack := make([]val, 0, gcLifetimeMaxSlots)
	pop := func() val { v := stack[len(stack)-1]; stack = stack[:len(stack)-1]; return v }
	fields := make([]storage, len(st.Comp.Fields))
	r := wasm.ReaderFrom(code)
	for r.HasNext() {
		rec := gcLifetimeRecord{pc: r.Offset()}
		op, _ := r.Byte()
		rec.op = op
		switch op {
		case 0x0b:
			rec.kind = 1
		case 0x01:
		case 0x41, 0x42, 0x43, 0x44:
			v := storage{kind: stConst}
			switch op {
			case 0x41:
				x, _ := r.I32()
				v.typ = mtI32
				v.cval = int64(x)
			case 0x42:
				x, _ := r.I64()
				v.typ = mtI64
				v.cval = x
			case 0x43:
				x, _ := r.LEU32()
				v.typ = mtF32
				v.cval = int64(x)
			case 0x44:
				x, _ := r.LEU64()
				v.typ = mtF64
				v.cval = int64(x)
			}
			stack = append(stack, val{scalar: v})
			rec.kind = 2
			rec.value = v
		case 0x20, 0x21, 0x22:
			idx, _ := r.U32()
			if op == 0x20 {
				v := locals[idx]
				stack = append(stack, v)
				if !v.object {
					rec.kind = 2
					rec.value = v.scalar
				}
			} else {
				v := pop()
				locals[idx] = v
				if op == 0x22 {
					stack = append(stack, v)
				}
				if !v.object {
					rec.kind = 1
				}
			}
		case 0x1a:
			if !pop().object {
				rec.kind = 1
			}
		case 0xfb:
			sub, _ := r.U32()
			_, _ = r.U32()
			if sub == 2 {
				fi, _ := r.U32()
				pop()
				v := fields[fi]
				stack = append(stack, val{scalar: v})
				rec.kind = 2
				rec.value = v
			} else {
				rec.kind = 3
				rec.defaultNew = sub == 1
				for j := len(fields) - 1; j >= 0; j-- {
					if sub == 0 {
						fields[j] = pop().scalar
					} else {
						fields[j] = zeroStorage(lifetimeMachineType(st.Comp.Fields[j].Storage().Val()))
					}
				}
				stack = append(stack, val{object: true})
			}
		default:
			return nil
		}
		rec.end = r.Offset()
		p.records = append(p.records, rec)
	}
	return p
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
	return mtI64 // erased reference locals never read their scalar storage
}

func (f *fn) replayGCLifetime(p *gcLifetimePlan, code []byte) bool {
	if p == nil {
		return false
	}
	replay := f.sealGCLifetime(code)
	return replay != nil && reflect.DeepEqual(p, replay)
}

// A rejected certificate emits nothing: the caller continues ordinary lowering.
func (f *fn) consumeGCLifetime(p *gcLifetimePlan, code []byte) (bool, error) {
	if !f.replayGCLifetime(p, code) {
		return false, nil
	}
	return true, f.emitGCLifetime(p)
}

func (f *fn) emitGCLifetime(p *gcLifetimePlan) error {
	st, _ := f.stagedStructType(p.typ)
	for _, rec := range p.records {
		f.wasmPC = f.tracePCBase + uint32(rec.pc)
		switch rec.kind {
		case 1:
			if rec.op == 0x0b {
				r := wasm.ReaderFrom(p.body)
				_ = r.JumpTo(rec.pc)
				return f.bodyLoop(&r, 0)
			}
			r := wasm.ReaderFrom(p.body[rec.pc+1 : rec.end])
			if err := f.emitPlain(&r, rec.op); err != nil {
				return err
			}
		case 2:
			f.pushGCForwardedValue(rec.value)
		case 3:
			params := make([]wasm.ValType, 0, len(st.Comp.Fields)+1)
			for _, field := range st.Comp.Fields {
				t := field.Storage().Val()
				params = append(params, t)
				if rec.defaultNew {
					f.pushValue(zeroStorage(lifetimeMachineType(t)))
				}
			}
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(p.typ)})
			params = append(params, wasm.I32)
			f.nativeStructAllocType = p.typ + 1
			f.nativeStructAllocDead = true
			result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: p.typ}), false))
			if err := f.callGCStructHelper(gcStructAllocOne, params, []wasm.ValType{result}); err != nil {
				return err
			}
			f.dropValue()
			f.stats.peep("gc-lifetime-checked-scalar-replace")
		}
	}
	return nil
}
