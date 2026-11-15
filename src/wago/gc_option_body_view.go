package wago

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"sort"
)

// Build a private validated Wasm view. This does not change
// public signatures or mutate the validated input. Native constructor elision
// is a separate step; each original constructor remains in this view.
type gcOptionView struct {
	Module       *wasm.Module
	Constructors []gcOptionConstructor
	Origins      map[uint32][]gcOptionOrigin
	Families     int
	Growth       int
}
type gcOptionConstructor struct {
	Function, PC, Type, Sink uint32
	BodySHA256               [32]byte
	CheckReference           bool
}
type gcOptionOrigin struct {
	NewPC, OldFunction, OldPC uint32
	Linear                    bool
}
type gcOptionCompactImmediate struct {
	Kind                     wasm.InstrKind
	Index, Index2, Subopcode uint32
	HeapType                 wasm.HeapType
}
type gcOptionInstruction struct {
	pc, end int
	op      byte
	imm     gcOptionCompactImmediate
	raw     []byte
}
type gcOptionSummary struct {
	wrapper, producer, predicate, payload, parent, child, receiver uint32
	wrapperPC, producerPC                                          []uint32
}
type gcOptionFamily struct {
	local                  uint32
	summary                gcOptionSummary
	stores, tags, payloads []int
	invalid                bool
	tag, sink              uint32
}

func gcOptionDecode(m *wasm.Module, body []byte, limit int) ([]gcOptionInstruction, []int, bool) {
	return gcOptionDecodeCapacity(m, body, limit, 0)
}
func gcOptionDecodeCapacity(m *wasm.Module, body []byte, limit, capacity int) ([]gcOptionInstruction, []int, bool) {
	r := wasm.NewReader(body)
	c := wasm.NewModuleInstructionClassifier(m, true)
	var out []gcOptionInstruction
	var ends []int
	if capacity > 0 && capacity <= limit {
		out = make([]gcOptionInstruction, 0, capacity)
		ends = make([]int, 0, capacity)
	}
	var stack []int
	for r.HasNext() {
		if len(out) == limit {
			return nil, nil, false
		}
		pc := r.Offset()
		op, e := r.Byte()
		if e != nil {
			return nil, nil, false
		}
		var im wasm.InstructionImmediate
		if c.ClassifyInto(r, op, &im) != nil {
			return nil, nil, false
		}
		i := len(out)
		out = append(out, gcOptionInstruction{pc, r.Offset(), op, gcOptionCompactImmediate{im.Kind, im.Index, im.Index2, im.Subopcode, im.HeapType}, body[pc:r.Offset()]})
		ends = append(ends, -1)
		switch op {
		case 2, 3, 4:
			if len(stack) == 32 {
				return nil, nil, false
			}
			stack = append(stack, i)
		case 0xb:
			if len(stack) > 0 {
				j := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				ends[j] = i
			}
		}
	}
	return out, ends, len(stack) == 0
}
func gcOptionULEB(v uint32) []byte {
	var b []byte
	for v >= 128 {
		b = append(b, byte(v&127)|128)
		v >>= 7
	}
	return append(b, byte(v))
}
func gcOptionHeap(v uint32) []byte {
	var b []byte
	for {
		c := byte(v & 127)
		v >>= 7
		if v != 0 || c&64 != 0 {
			b = append(b, c|128)
		} else {
			return append(b, c)
		}
	}
}
func gcOptionEncodeLocal(v wasm.ValType) ([]byte, bool) {
	if b, ok := wasm.EncodeValType(v); ok {
		return []byte{b}, true
	}
	if v.Kind() != wasm.ValRef || v.Ref().Exact() {
		return nil, false
	}
	r := v.Ref()
	prefix := byte(0x64)
	if r.Nullable() {
		prefix = 0x63
	}
	h := r.Heap()
	switch h.Kind() {
	case wasm.HeapAbs:
		return []byte{prefix, byte(h.Abs())}, true
	case wasm.HeapTypeIndex:
		if h.Type().Rec {
			return nil, false
		}
		return append([]byte{prefix}, gcOptionHeap(h.Type().Index)...), true
	}
	return nil, false
}
func gcOptionCast(i gcOptionInstruction, index uint32) bool {
	return i.op == 0xfb && i.imm.Kind == wasm.InstrRefCast && i.imm.Subopcode == 22 && bytes.Equal(i.raw[2:], gcOptionHeap(index))
}
func gcOptionLocalType(m *wasm.Module, local int, index uint32) (wasm.ValType, bool) {
	ft, ok := m.LocalFuncType(local)
	if !ok {
		return wasm.ValType{}, false
	}
	if int(index) < len(ft.Params) {
		return ft.Params[index], true
	}
	n := index - uint32(len(ft.Params))
	for _, run := range m.Code[local].Locals.Runs {
		if n < run.Count {
			return run.Type, true
		}
		n -= run.Count
	}
	return wasm.ValType{}, false
}
func gcOptionFindSummary(m *wasm.Module, wrapper uint32) (s gcOptionSummary, ok bool) {
	imported := uint32(m.ImportedFuncCount())
	if wrapper < imported || int(wrapper-imported) >= len(m.Code) {
		return
	}
	wc := m.Code[wrapper-imported]
	if len(wc.BodyBytes) > 32 || len(wc.Locals.Runs) != 0 {
		return
	}
	w, _, valid := gcOptionDecode(m, wc.BodyBytes, 16)
	if !valid || len(w) != 5 || w[0].op != 0x20 || w[0].imm.Index != 0 || w[2].op != 0x10 || w[4].op != 0x0b {
		return
	}
	producer := w[2].imm.Index
	if producer < imported || int(producer-imported) >= len(m.Code) {
		return
	}
	pc := m.Code[producer-imported]
	if len(pc.BodyBytes) > 128 || len(pc.Locals.Runs) != 1 || pc.Locals.Runs[0].Count != 1 || !wasm.EqualValType(pc.Locals.Runs[0].Type, wasm.EqRef) {
		return
	}
	p, _, valid := gcOptionDecode(m, pc.BodyBytes, 32)
	if !valid || len(p) != 17 {
		return
	}
	ops := []byte{0x20, 0xfb, 0x10, 4, 0x20, 0xfb, 0x10, 0x21, 0x41, 0x20, 0xfb, 5, 0x41, 0xfb, 0xb, 0xfb, 0xb}
	for i, op := range ops {
		if p[i].op != op {
			return
		}
	}
	if p[0].imm.Index != 0 || p[4].imm.Index != 0 || p[7].imm.Index != 1 || p[9].imm.Index != 1 || !bytes.Equal(p[3].raw, []byte{4, 0x6d}) || !bytes.Equal(p[8].raw, []byte{0x41, 1}) || !bytes.Equal(p[12].raw, []byte{0x41, 0}) || p[10].imm.Kind != wasm.InstrStructNew || p[13].imm.Kind != wasm.InstrStructNew || p[1].imm.HeapType.Kind() != wasm.HeapTypeIndex {
		return
	}
	receiver, parent, child := p[1].imm.HeapType.Type().Index, p[13].imm.Index, p[10].imm.Index
	if !gcOptionCast(w[1], receiver) || !gcOptionCast(w[3], parent) || !gcOptionCast(p[1], receiver) || !gcOptionCast(p[5], receiver) || !gcOptionCast(p[15], parent) {
		return
	}
	bt, bok := m.TypeSubType(parent)
	ct, cok := m.TypeSubType(child)
	if !bok || !cok || bt.Comp.Kind != wasm.CompStruct || ct.Comp.Kind != wasm.CompStruct || bt.Final || !ct.Final || len(bt.Comp.Fields) != 1 || len(ct.Comp.Fields) != 2 || len(ct.Supers) != 1 || ct.Supers[0].Rec || ct.Supers[0].Index != parent {
		return
	}
	for _, field := range []wasm.FieldType{bt.Comp.Fields[0], ct.Comp.Fields[0]} {
		if field.Mut() != 0 || field.Storage().Packed() || !wasm.EqualValType(field.Storage().Val(), wasm.I32) {
			return
		}
	}
	if ct.Comp.Fields[1].Mut() != 0 || ct.Comp.Fields[1].Storage().Packed() || !wasm.EqualValType(ct.Comp.Fields[1].Storage().Val(), wasm.EqRef) {
		return
	}
	wt, wok := m.FuncSignature(wrapper)
	pt, pok := m.FuncSignature(producer)
	pred, aok := m.FuncSignature(p[2].imm.Index)
	pay, zok := m.FuncSignature(p[6].imm.Index)
	if !wok || !pok || !aok || !zok || len(wt.Params) != 1 || len(pt.Params) != 1 || len(wt.Results) != 1 || len(pt.Results) != 1 || !wasm.EqualValType(wt.Params[0], pt.Params[0]) || !wasm.EqualValType(wt.Results[0], pt.Results[0]) || len(pred.Params) != 1 || len(pred.Results) != 1 || !wasm.EqualValType(pred.Params[0], pt.Params[0]) || !wasm.EqualValType(pred.Results[0], wasm.I32) || len(pay.Params) != 1 || len(pay.Results) != 1 || !wasm.EqualValType(pay.Params[0], pt.Params[0]) || !wasm.EqualValType(pay.Results[0], wasm.EqRef) {
		return
	}
	arg := pt.Params[0]
	result := pt.Results[0]
	if arg.Kind() != wasm.ValRef || arg.Ref().Nullable() || arg.Ref().Heap().Kind() != wasm.HeapTypeIndex || arg.Ref().Heap().Type().Index != receiver || result.Kind() != wasm.ValRef || result.Ref().Nullable() || result.Ref().Heap().Kind() != wasm.HeapTypeIndex || result.Ref().Heap().Type().Index != parent {
		return
	}
	s = gcOptionSummary{wrapper: wrapper, producer: producer, predicate: p[2].imm.Index, payload: p[6].imm.Index, parent: parent, child: child, receiver: receiver}
	for _, in := range w {
		s.wrapperPC = append(s.wrapperPC, uint32(in.pc)+wc.LocalDeclBytes)
	}
	for _, in := range p {
		s.producerPC = append(s.producerPC, uint32(in.pc)+pc.LocalDeclBytes)
	}
	return s, true
}
func gcOptionFamilies(m *wasm.Module, local int, summaries map[uint32]gcOptionSummary) ([]gcOptionInstruction, []gcOptionFamily) {
	code := m.Code[local]
	if len(code.BodyBytes) > 16384 {
		return nil, nil
	}
	ft, ok := m.LocalFuncType(local)
	if !ok {
		return nil, nil
	}
	nlocals := len(ft.Params)
	for _, run := range code.Locals.Runs {
		nlocals += int(run.Count)
		if nlocals > 256 {
			return nil, nil
		}
	}
	// Avoid retaining decoded instructions for functions with no candidate call.
	c := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.NewReader(code.BodyBytes)
	found := false
	steps := 0
	for r.HasNext() {
		steps++
		if steps > 4096 {
			return nil, nil
		}
		op, e := r.Byte()
		if e != nil {
			return nil, nil
		}
		var im wasm.InstructionImmediate
		if c.ClassifyInto(r, op, &im) != nil {
			return nil, nil
		}
		if op == 0x10 {
			if _, ok := summaries[im.Index]; ok {
				found = true
			}
		}
	}
	if !found {
		return nil, nil
	}
	ins, ends, ok := gcOptionDecodeCapacity(m, code.BodyBytes, 4096, steps)
	if !ok {
		return nil, nil
	}
	for _, in := range ins {
		switch in.op {
		case 0x12, 0x13, 0x15, 0x1f, 6, 7, 8, 9, 0x18, 0x19:
			return nil, nil
		}
	}
	var families []gcOptionFamily
	for i, in := range ins {
		if in.op != 0x10 || i+1 == len(ins) || ins[i+1].op != 0x21 {
			continue
		}
		summary, ok := summaries[in.imm.Index]
		if !ok {
			continue
		}
		slot := ins[i+1].imm.Index
		j := -1
		for k := range families {
			if families[k].local == slot {
				j = k
				break
			}
		}
		if j < 0 {
			if len(families) == 8 {
				return nil, nil
			}
			typ, valid := gcOptionLocalType(m, local, slot)
			if !valid || int(slot) < len(ft.Params) || !wasm.EqualValType(typ, wasm.EqRef) {
				continue
			}
			families = append(families, gcOptionFamily{local: slot, summary: summary})
			j = len(families) - 1
		}
		f := &families[j]
		if f.summary.wrapper != summary.wrapper {
			f.invalid = true
		}
		f.stores = append(f.stores, i+1)
	}
	if len(families) == 0 || nlocals+2*len(families) > 256 {
		return nil, nil
	}
	for i, in := range ins {
		for k := range families {
			f := &families[k]
			if in.imm.Index != f.local {
				continue
			}
			switch in.op {
			case 0x21:
				if i == 0 || ins[i-1].op != 0x10 || ins[i-1].imm.Index != f.summary.wrapper {
					f.invalid = true
				}
			case 0x22:
				f.invalid = true
			case 0x20:
				if i+2 >= len(ins) {
					f.invalid = true
					continue
				}
				cast, get := ins[i+1], ins[i+2]
				if gcOptionCast(cast, f.summary.parent) && get.imm.Kind == wasm.InstrStructGet && get.imm.Index == f.summary.parent && get.imm.Index2 == 0 {
					f.tags = append(f.tags, i)
				} else if gcOptionCast(cast, f.summary.child) && get.imm.Kind == wasm.InstrStructGet && get.imm.Index == f.summary.child && get.imm.Index2 == 1 {
					f.payloads = append(f.payloads, i)
				} else {
					f.invalid = true
				}
			}
		}
	}
	for k := range families {
		f := &families[k]
		if f.invalid || len(f.stores) != 2 || len(f.tags) != 2 || len(f.payloads) != 1 {
			f.invalid = true
			continue
		}
		a, b := f.stores[0], f.stores[1]
		if a+13 >= len(ins) || b+19 >= len(ins) {
			f.invalid = true
			continue
		}
		prefix := []byte{2, 3, 2, 0x20, 0xfb, 0xfb, 0x41, 0x46, 4, 0x20, 0xfb, 0xfb}
		for j, op := range prefix {
			if ins[a+1+j].op != op {
				f.invalid = true
			}
		}
		if f.tags[0] != a+4 || f.payloads[0] != a+10 || !bytes.Equal(ins[a+7].raw, []byte{0x41, 1}) || !bytes.Equal(ins[a+9].raw, []byte{4, 0x40}) {
			f.invalid = true
		}
		suffix := [][]byte{{0xc, 2}, {0xc, 1}, {0xb}}
		for j, want := range suffix {
			if !bytes.Equal(ins[b+1+j].raw, want) {
				f.invalid = true
			}
		}
		if f.tags[1] != b+4 || !bytes.Equal(ins[b+7].raw, []byte{0x41, 0}) || !bytes.Equal(ins[b+8].raw, []byte{0x46}) || !bytes.Equal(ins[b+9].raw, []byte{4, 0x40}) {
			f.invalid = true
		}
		tail := [][]byte{{0xc, 3}, {0xc, 1}, {0xb}, {0}, {0xb}, {0}, {0xb}, {0}, {0xb}}
		for j, want := range tail {
			if !bytes.Equal(ins[b+10+j].raw, want) {
				f.invalid = true
			}
		}
		if ends[a+9] != b+3 || ends[b+9] != b+12 || ends[a+3] != b+14 || ends[a+2] != b+16 || ends[a+1] != b+18 {
			f.invalid = true
		}
	}
	// The fixed preheader dominates loop entry. The only definitions and uses
	// are those checked above. Exact backwards liveness rejects a carrier needed
	// after any possible collection, including a branch back to its projections.
	roots := make([]shared.GCFrameLocal, len(families))
	for i, f := range families {
		roots[i] = shared.GCFrameLocal{Index: f.local}
	}
	masks, e := gcFrameLocalLivenessArenaWithClassifier(code.BodyBytes, roots, &c)
	if e != nil || len(masks.rootIndexes) != 0 {
		return nil, nil
	}
	out := families[:0]
	for k, f := range families {
		if f.invalid {
			continue
		}
		live := false
		for site := 0; site < masks.allocationN+masks.callN; site++ {
			if masks.site(site)[k/64]&(uint64(1)<<uint(k%64)) != 0 {
				live = true
				break
			}
		}
		if !live {
			out = append(out, f)
		}
	}
	return ins, out
}

type gcOptionReplacement struct {
	start, end   int
	body         []byte
	origins      []gcOptionOrigin
	constructors []gcOptionConstructor
}

func gcOptionExpand(f gcOptionFamily, global uint32, callPC uint32) gcOptionReplacement {
	s := f.summary
	var r gcOptionReplacement
	emit := func(op byte, immediates []byte, function, pc uint32) {
		r.origins = append(r.origins, gcOptionOrigin{uint32(len(r.body)), function, pc, false})
		r.body = append(r.body, op)
		r.body = append(r.body, immediates...)
	}
	loc := func(op byte, index uint32, fun, pc uint32) { emit(op, gcOptionULEB(index), fun, pc) }
	cast := func(fun, pc uint32) { emit(0xfb, append([]byte{22}, gcOptionHeap(s.receiver)...), fun, pc) }
	ctor := func(typ uint32, check bool, pc uint32) {
		r.constructors = append(r.constructors, gcOptionConstructor{Function: global, PC: uint32(len(r.body)), Type: typ, Sink: f.sink, CheckReference: check})
		emit(0xfb, append([]byte{0}, gcOptionULEB(typ)...), s.producer, pc)
		loc(0x21, f.sink, s.producer, pc)
	}
	// The carrier's old value is dead. Its nullable eqref home first snapshots
	// the receiver, then the payload. No new hidden reference root is needed.
	loc(0x21, f.local, global, callPC)
	loc(0x20, f.local, s.wrapper, s.wrapperPC[0])
	cast(s.wrapper, s.wrapperPC[1])
	cast(s.producer, s.producerPC[1])
	loc(0x10, s.predicate, s.producer, s.producerPC[2])
	emit(4, []byte{0x40}, s.producer, s.producerPC[3])
	loc(0x20, f.local, s.producer, s.producerPC[4])
	cast(s.producer, s.producerPC[5])
	loc(0x10, s.payload, s.producer, s.producerPC[6])
	loc(0x21, f.local, s.producer, s.producerPC[7])
	emit(0x41, []byte{1}, s.producer, s.producerPC[8])
	loc(0x21, f.tag, s.producer, s.producerPC[8])
	emit(0x41, []byte{1}, s.producer, s.producerPC[8])
	loc(0x20, f.local, s.producer, s.producerPC[9])
	ctor(s.child, true, s.producerPC[10])
	emit(5, nil, s.producer, s.producerPC[11])
	emit(0xd0, []byte{0x6d}, s.producer, s.producerPC[12])
	loc(0x21, f.local, s.producer, s.producerPC[12])
	emit(0x41, []byte{0}, s.producer, s.producerPC[12])
	loc(0x21, f.tag, s.producer, s.producerPC[12])
	emit(0x41, []byte{0}, s.producer, s.producerPC[12])
	ctor(s.parent, false, s.producerPC[13])
	emit(0xb, nil, s.producer, s.producerPC[14])
	return r
}
func buildGCOptionBodyView(m *wasm.Module) (*gcOptionView, error) {
	return buildGCOptionBodyViewWithLocalLimit(m, 0)
}

func buildGCOptionBodyViewWithLocalLimit(m *wasm.Module, localLimit uint32) (*gcOptionView, error) {
	if localLimit == 0 || localLimit > 256 {
		localLimit = 256
	}

	if m == nil {
		return nil, nil
	}
	summaries := map[uint32]gcOptionSummary{}
	for i := range m.Code {
		global := uint32(i + m.ImportedFuncCount())
		if s, ok := gcOptionFindSummary(m, global); ok {
			if len(summaries) == 32 {
				return nil, nil
			}
			summaries[global] = s
		}
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	prefixes := gcOptionCandidatePrefixes(summaries)
	view := &gcOptionView{Origins: map[uint32][]gcOptionOrigin{}}
	private := *m
	private.Code = append([]wasm.Func(nil), m.Code...)
	for local, code := range m.Code {
		if len(code.BodyBytes) > 16384 || !gcOptionMayContainCandidateCall(code.BodyBytes, &prefixes) {
			continue
		}
		ins, families := gcOptionFamilies(m, local, summaries)
		if len(families) == 0 {
			continue
		}
		global := uint32(local + m.ImportedFuncCount())
		ft, _ := m.LocalFuncType(local)
		nlocals := len(ft.Params)
		for _, run := range code.Locals.Runs {
			nlocals += int(run.Count)
		}
		// Internal tag/sink locals must fit the source compiler's bookkeeping limit.
		// Decline this function's optimization instead of rejecting valid source.
		if uint32(nlocals+2*len(families)) > localLimit {
			continue
		}

		runs := append([]wasm.LocalRun(nil), code.Locals.Runs...)
		var replacements []gcOptionReplacement
		for i := range families {
			f := &families[i]
			f.tag = uint32(nlocals)
			f.sink = uint32(nlocals + 1)
			nlocals += 2
			runs = append(runs, wasm.LocalRun{Count: 1, Type: wasm.I32}, wasm.LocalRun{Count: 1, Type: wasm.EqRef})
			for _, store := range f.stores {
				call := ins[store-1]
				r := gcOptionExpand(*f, global, uint32(call.pc)+code.LocalDeclBytes)
				r.start, r.end = call.pc, ins[store].end
				replacements = append(replacements, r)
			}
			for _, tag := range f.tags {
				replacements = append(replacements, gcOptionReplacement{start: ins[tag].pc, end: ins[tag+2].end, body: append([]byte{0x20}, gcOptionULEB(f.tag)...), origins: []gcOptionOrigin{{0, global, uint32(ins[tag].pc) + code.LocalDeclBytes, false}}})
			}
			for _, payload := range f.payloads {
				replacements = append(replacements, gcOptionReplacement{start: ins[payload].pc, end: ins[payload+2].end, body: append([]byte{0x20}, gcOptionULEB(f.local)...), origins: []gcOptionOrigin{{0, global, uint32(ins[payload].pc) + code.LocalDeclBytes, false}}})
			}
		}
		sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
		var body []byte
		var origins []gcOptionOrigin
		var certificates []gcOptionConstructor
		at := 0
		for _, r := range replacements {
			if r.start < at {
				return nil, fmt.Errorf("overlapping option rewrite")
			}
			if r.start > at {
				origins = append(origins, gcOptionOrigin{uint32(len(body)), global, uint32(at) + code.LocalDeclBytes, true})
				body = append(body, code.BodyBytes[at:r.start]...)
			}
			base := uint32(len(body))
			for _, origin := range r.origins {
				origin.NewPC += base
				origins = append(origins, origin)
			}
			for _, certificate := range r.constructors {
				certificate.PC += base
				certificates = append(certificates, certificate)
			}
			body = append(body, r.body...)
			at = r.end
		}
		if at < len(code.BodyBytes) {
			origins = append(origins, gcOptionOrigin{uint32(len(body)), global, uint32(at) + code.LocalDeclBytes, true})
			body = append(body, code.BodyBytes[at:]...)
		}
		growth := len(body) - len(code.BodyBytes)
		if growth < 0 {
			growth = 0
		}
		if view.Growth+growth > 256<<10 {
			continue
		}
		copy := code
		copy.Body = wasm.Expr{}
		copy.BodyBytes = body
		copy.Locals = wasm.Locals{Runs: runs}
		decl := gcOptionULEB(uint32(len(runs)))
		declarationOK := true
		for _, run := range runs {
			decl = append(decl, gcOptionULEB(run.Count)...)
			v, ok := gcOptionEncodeLocal(run.Type)
			if !ok {
				declarationOK = false
				break
			}
			decl = append(decl, v...)
		}
		if !declarationOK {
			continue
		}
		view.Growth += growth
		view.Families += len(families)
		copy.LocalDeclBytes = uint32(len(decl))
		private.Code[local] = copy
		hash := sha256.Sum256(body)
		for i := range certificates {
			certificates[i].BodySHA256 = hash
		}
		view.Constructors = append(view.Constructors, certificates...)
		view.Origins[global] = origins
	}
	if view.Families == 0 {
		return nil, nil
	}
	view.Module = &private
	return view, nil
}

func (v *gcOptionView) metadata() map[uint32]codegen.GCBodyView {
	out := make(map[uint32]codegen.GCBodyView, len(v.Origins))
	for index, origins := range v.Origins {
		b := codegen.GCBodyView{BodyProof: codegen.NewGCBodyProof(v.Module.Code[int(index)-v.Module.ImportedFuncCount()].BodyBytes), Constructors: map[uint32]codegen.GCDeadConstructor{}}
		for _, o := range origins {
			b.Origins = append(b.Origins, codegen.GCBodyOrigin{NewPC: o.NewPC, OldFunction: o.OldFunction, OldPC: o.OldPC, Linear: o.Linear})
		}
		out[index] = b
	}
	for _, c := range v.Constructors {
		b := out[c.Function]
		b.Constructors[c.PC] = codegen.GCDeadConstructor{Type: c.Type, Sink: c.Sink}
		out[c.Function] = b
	}
	return out
}

// Necessary byte prefixes only. False positives go to the complete decoder.
// Padded legal ULEB encodings have the same first low seven target bits.
func gcOptionCandidatePrefixes(summaries map[uint32]gcOptionSummary) (out [256]bool) {
	for target := range summaries {
		low := byte(target & 127)
		if target < 128 {
			out[low] = true
		}
		out[low|128] = true
	}
	return
}
func gcOptionMayContainCandidateCall(body []byte, prefixes *[256]bool) bool {
	for len(body) > 1 {
		index := bytes.IndexByte(body, 0x10)
		if index < 0 || index+1 >= len(body) {
			return false
		}
		if prefixes[body[index+1]] {
			return true
		}
		body = body[index+1:]
	}
	return false
}

// Reject modules without the necessary type shapes before owning and copying
// their declarations. Complete eligibility remains in gcOptionFindSummary.
func gcOptionMayContainCarrier(m *wasm.Module) bool {
	if m == nil {
		return false
	}
	var tag, payload bool
	for _, group := range m.Types {
		for _, st := range group.SubTypes {
			if st.Comp.Kind != wasm.CompStruct {
				continue
			}
			fields := st.Comp.Fields
			if len(fields) == 1 && !fields[0].Storage().Packed() && wasm.EqualValType(fields[0].Storage().Val(), wasm.I32) {
				tag = true
			}
			if len(fields) == 2 && !fields[0].Storage().Packed() && !fields[1].Storage().Packed() && wasm.EqualValType(fields[0].Storage().Val(), wasm.I32) && wasm.EqualValType(fields[1].Storage().Val(), wasm.EqRef) {
				payload = true
			}
			if tag && payload {
				return true
			}
		}
	}
	return false
}
