package ir

import (
	"fmt"
	"slices"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type Builder struct {
	m   *wasm.Module
	out *Module
	fn  *Func
	r   *wasm.Reader

	cur            BlockID
	reachable      bool
	stack          []ValueID
	labels         []label
	ctrlH          []int
	localRunStarts []uint64

	preds       []uint32
	returnBlock BlockID
}

type labelKind uint8

const (
	labelFunc labelKind = iota
	labelBlock
	labelLoop
)

type label struct {
	kind   labelKind
	target BlockID
	types  []wasm.ValType
}

func funcTypeFromComp(ft *wasm.CompType) wasm.FuncType {
	if ft == nil {
		return wasm.FuncType{}
	}
	return wasm.FuncType{Params: ft.Params, Results: ft.Results}
}

func moduleTypeMeta(m *wasm.Module) ([]wasm.FuncType, []bool, []uint32) {
	n := 0
	for i := range m.Types {
		n += len(m.Types[i].SubTypes)
	}
	types := make([]wasm.FuncType, 0, n)
	isFunc := make([]bool, 0, n)
	canonical := make([]uint32, 0, n)
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			ct := &m.Types[i].SubTypes[j].Comp
			idx := uint32(len(types))
			if ct.Kind == wasm.CompFunc {
				types = append(types, funcTypeFromComp(ct))
				isFunc = append(isFunc, true)
				canonical = append(canonical, m.CanonicalTypeID(idx))
			} else {
				// Preserve flattened type indexes even when a rec group also contains
				// non-function subtypes. Kind metadata keeps placeholders from becoming
				// indistinguishable empty function signatures to verifier/codegen users.
				types = append(types, wasm.FuncType{})
				isFunc = append(isFunc, false)
				canonical = append(canonical, idx)
			}
		}
	}
	return types, isFunc, canonical
}

func elemLen(e wasm.Elem) uint32 {
	switch e.Kind.Kind {
	case wasm.ElemFuncs:
		return uint32(len(e.Kind.Funcs))
	case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
		return uint32(len(e.Kind.Exprs))
	default:
		return 0
	}
}

func elemType(e wasm.Elem) wasm.ValType {
	if e.Kind.Kind == wasm.ElemFuncs || e.Kind.Kind == wasm.ElemFuncExprs {
		return wasm.FuncRef
	}
	return wasm.RefVal(e.Kind.Ref)
}

func globalTypeValue(gt wasm.GlobalType) wasm.ValType { return wasm.GlobalValueType(gt) }
func tableRefType(tt wasm.TableType) wasm.RefType     { return wasm.TableRefType(tt) }
func tableAddrType(tt wasm.TableType) wasm.ValType    { return wasm.TableAddrType(tt) }
func memoryAddrType(mt wasm.MemType) wasm.ValType     { return wasm.MemoryAddrType(mt) }

func minAddrType(a, b wasm.ValType) wasm.ValType {
	if a == wasm.I32 || b == wasm.I32 {
		return wasm.I32
	}
	return wasm.I64
}

func isFuncRefTableType(m *wasm.Module, rt wasm.RefType) bool {
	switch rt.Heap.Kind {
	case wasm.HeapAbs:
		return rt.Heap.Abs == wasm.HeapFunc || rt.Heap.Abs == wasm.HeapNoFunc
	case wasm.HeapTypeIndex:
		if m == nil {
			return true
		}
		_, ok := m.TypeFunc(rt.Heap.Type.Index)
		return ok
	case wasm.HeapDefType:
		if rt.Heap.Def == nil || int(rt.Heap.Def.Index) >= len(rt.Heap.Def.Rec.SubTypes) {
			return false
		}
		return rt.Heap.Def.Rec.SubTypes[int(rt.Heap.Def.Index)].Comp.Kind == wasm.CompFunc
	default:
		return false
	}
}

func buildModuleMeta(m *wasm.Module) *Module {
	types, isFunc, canonical := moduleTypeMeta(m)
	out := &Module{Types: types, TypeIsFunc: isFunc, CanonicalTypeIDs: canonical, ImportedFuncCount: uint32(m.ImportedFuncCount())}
	out.FuncTypes = make([]uint32, 0, int(out.ImportedFuncCount)+len(m.FuncTypes))
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternFunc {
			out.FuncTypes = append(out.FuncTypes, m.Imports[i].Type.Type.Index)
		}
	}
	for i := range m.FuncTypes {
		out.FuncTypes = append(out.FuncTypes, m.FuncTypes[i].Index)
	}
	for i := range m.Imports {
		switch m.Imports[i].Type.Kind {
		case wasm.ExternGlobal:
			gt := m.Imports[i].Type.Global
			gt.Type = globalTypeValue(gt)
			out.Globals = append(out.Globals, gt)
		case wasm.ExternMem:
			out.Memories = append(out.Memories, m.Imports[i].Type.Mem)
		case wasm.ExternTable:
			out.Tables = append(out.Tables, m.Imports[i].Type.Table)
		}
	}
	for i := range m.Globals {
		gt := m.Globals[i].Type
		gt.Type = globalTypeValue(gt)
		out.Globals = append(out.Globals, gt)
	}
	out.Memories = append(out.Memories, m.Memories...)
	for i := range m.Tables {
		out.Tables = append(out.Tables, m.Tables[i].Type)
	}
	for i := range m.Elements {
		e := m.Elements[i]
		out.Elements = append(out.Elements, ElementMeta{TableIdx: uint32(e.Mode.Table), ElemType: elemType(e), Passive: e.Mode.Kind == wasm.ElemPassive, Declared: e.Mode.Kind == wasm.ElemDeclarative, Len: elemLen(e)})
	}
	for i := range m.Data {
		d := m.Data[i]
		out.Data = append(out.Data, DataMeta{MemIdx: uint32(d.Mode.Mem), Passive: d.Mode.Kind == wasm.DataPassive, Len: uint32(len(d.Init))})
	}
	return out
}

func BuildModule(m *wasm.Module) (*Module, error) {
	if m == nil {
		return nil, fmt.Errorf("ir: nil wasm module")
	}
	if err := rejectMultiMemory(m); err != nil {
		return nil, err
	}
	b := &Builder{m: m}
	out := buildModuleMeta(m)
	out.Funcs = make([]Func, len(m.Code))
	b.out = out
	for i := range m.Code {
		f, err := b.buildFunc(uint32(i))
		if err != nil {
			return nil, err
		}
		out.Funcs[i] = *f
	}
	return out, nil
}

func BuildFunc(m *wasm.Module, localFuncIdx int) (*Func, error) {
	if m == nil {
		return nil, fmt.Errorf("ir: nil wasm module")
	}
	if err := rejectMultiMemory(m); err != nil {
		return nil, err
	}
	if localFuncIdx < 0 || localFuncIdx >= len(m.Code) {
		return nil, fmt.Errorf("ir: local function index %d out of range", localFuncIdx)
	}
	out := buildModuleMeta(m)
	b := &Builder{m: m, out: out}
	return b.buildFunc(uint32(localFuncIdx))
}

func (b *Builder) buildFunc(localIdx uint32) (*Func, error) {
	if int(localIdx) >= len(b.m.FuncTypes) || int(localIdx) >= len(b.m.Code) {
		return nil, fmt.Errorf("ir: local function index %d out of range", localIdx)
	}
	typeIdx := b.m.FuncTypes[localIdx].Index
	ftc, ok := b.m.LocalFuncType(int(localIdx))
	if !ok {
		return nil, fmt.Errorf("ir: function %d has unknown type %d", localIdx, typeIdx)
	}
	ft := funcTypeFromComp(ftc)
	if err := checkIRValTypes(fmt.Sprintf("function %d param", localIdx), ft.Params); err != nil {
		return nil, err
	}
	if err := checkIRValTypes(fmt.Sprintf("function %d result", localIdx), ft.Results); err != nil {
		return nil, err
	}
	code := &b.m.Code[localIdx]
	for i, run := range code.Locals.Runs {
		if !supportsIRValType(run.Type) {
			return nil, fmt.Errorf("function %d local run %d has unsupported IR value type %s", localIdx, i, run.Type)
		}
	}
	body := code.BodyBytes
	if body == nil {
		var err error
		body, err = wasm.EncodeExpr(code.Body)
		if err != nil {
			return nil, err
		}
	}
	fn := &Func{Index: uint32(b.m.ImportedFuncCount()) + localIdx, LocalIndex: localIdx, TypeIndex: typeIdx, Sig: ft, Entry: InvalidBlock}
	// Keep locals compact: parameters need O(1) indexing, while declared locals
	// retain the wasm run-length encoding to avoid allocating one byte per local
	// in functions that declare large zero-initialized local ranges.
	fn.Locals = append(fn.Locals, ft.Params...)
	fn.LocalRuns = append(fn.LocalRuns, code.Locals.Runs...)
	b.fn = fn
	b.prepareLocalLookup()
	// Byte length is a useful upper bound, but retaining capacity proportional to
	// bytecode size can dwarf the actual IR on small devices. Start with capped
	// guesses and trim any large slack once the function is complete.
	blockCap := min(4+len(body)/4, 1024)
	instCap := min(len(body)/2, 2048)
	valueCap := min(len(body)/2+len(ft.Params), 4096)
	idCap := min(len(body), 8192)
	fn.Blocks = make([]Block, 0, blockCap)
	fn.Insts = make([]Inst, 0, instCap)
	fn.Values = make([]Value, 0, valueCap)
	fn.ValueIDs = make([]ValueID, 0, idCap)
	fn.Edges = make([]Edge, 0, 8)
	b.r = wasm.NewReader(body)
	b.returnBlock = InvalidBlock
	b.stack = b.stack[:0]
	b.labels = b.labels[:0]
	b.ctrlH = b.ctrlH[:0]
	b.preds = b.preds[:0]
	// Function parameters are explicit local state at this IR stage, not operand
	// stack values. Keeping the entry block parameterless avoids allocating dead
	// SSA values for params that can only be observed through local.get.
	entry := b.newBlock(nil)
	fn.Entry = entry
	b.cur = entry
	b.reachable = true
	b.labels = append(b.labels, label{kind: labelFunc, types: ft.Results})
	b.ctrlH = append(b.ctrlH, 0)
	stop, err := b.parseSeq(true)
	if err != nil {
		return nil, fmt.Errorf("ir: function %d: %w", fn.Index, err)
	}
	if stop != 0x0b {
		return nil, fmt.Errorf("ir: function %d missing end", fn.Index)
	}
	if b.r.HasNext() {
		return nil, fmt.Errorf("ir: function %d has trailing bytes", fn.Index)
	}
	if b.reachable {
		args, err := b.popValues(ft.Results)
		if err != nil {
			return nil, err
		}
		if len(b.stack) != 0 {
			return nil, fmt.Errorf("ir: function %d has %d leftover stack values", fn.Index, len(b.stack))
		}
		b.setReturn(args)
	}
	b.terminateDeadBlocks()
	trimFuncStorage(fn)
	return fn, nil
}

func (b *Builder) parseSeq(stopAtEnd bool) (byte, error) {
	for b.r.HasNext() {
		op, err := b.r.Byte()
		if err != nil {
			return 0, err
		}
		switch op {
		case 0x05, 0x0b:
			return op, nil
		case 0x00: // unreachable
			if b.reachable {
				b.setTrap()
			}
			b.setUnreachable()
		case 0x01: // nop
		case 0x02:
			in, out, err := b.readBlockType()
			if err != nil {
				return 0, err
			}
			if err := b.lowerBlock(in, out); err != nil {
				return 0, err
			}
		case 0x03:
			in, out, err := b.readBlockType()
			if err != nil {
				return 0, err
			}
			if err := b.lowerLoop(in, out); err != nil {
				return 0, err
			}
		case 0x04:
			in, out, err := b.readBlockType()
			if err != nil {
				return 0, err
			}
			if err := b.lowerIf(in, out); err != nil {
				return 0, err
			}
		case 0x0c:
			depth, err := b.r.U32()
			if err != nil {
				return 0, err
			}
			if err := b.lowerBr(depth); err != nil {
				return 0, err
			}
		case 0x0d:
			depth, err := b.r.U32()
			if err != nil {
				return 0, err
			}
			if err := b.lowerBrIf(depth); err != nil {
				return 0, err
			}
		case 0x0e:
			if err := b.lowerBrTable(); err != nil {
				return 0, err
			}
		case 0x0f:
			args, err := b.popValues(b.fn.Sig.Results)
			if err != nil {
				return 0, err
			}
			if b.reachable {
				b.setReturn(args)
			}
			b.setUnreachable()
		default:
			if err := b.lowerSimple(op); err != nil {
				return 0, err
			}
		}
	}
	if stopAtEnd {
		return 0, nil
	}
	return 0, fmt.Errorf("unexpected end of bytecode")
}

func (b *Builder) readBlockType() (in, out []wasm.ValType, err error) {
	first, ok := b.r.Peek()
	if !ok {
		return nil, nil, fmt.Errorf("invalid block type")
	}
	if first == 0x40 {
		_, _ = b.r.Byte()
		return nil, nil, nil
	}
	if vt, ok := valTypeByte(first); ok {
		_, _ = b.r.Byte()
		if !supportsIRValType(vt) {
			return nil, nil, fmt.Errorf("unsupported IR value type %s in block result", vt)
		}
		return nil, []wasm.ValType{vt}, nil
	}
	x, err := b.r.I64()
	if err != nil {
		return nil, nil, err
	}
	// Non-inline block types are typeidx values encoded as signed LEBs by the
	// core spec. Keep the lowering boundary strict: values outside the uint32
	// index space must not wrap and accidentally resolve to a valid type.
	if x < 0 || x > int64(^uint32(0)) {
		return nil, nil, fmt.Errorf("invalid block type index %d", x)
	}
	ft, ok := b.m.TypeFunc(uint32(x))
	if !ok {
		return nil, nil, fmt.Errorf("invalid block type index %d", x)
	}
	if err := checkIRFuncType(fmt.Sprintf("block type %d", x), funcTypeFromComp(ft)); err != nil {
		return nil, nil, err
	}
	return ft.Params, ft.Results, nil
}

func supportsIRValType(t wasm.ValType) bool {
	// The current IR opcode set and amd64 backend cover only scalar numeric
	// values. Reference, GC, and vector types remain at the wasm validation
	// boundary until the IR has explicit operations and codegen contracts for them.
	return t == wasm.I32 || t == wasm.I64 || t == wasm.F32 || t == wasm.F64
}

func validValType(t wasm.ValType) bool { return supportsIRValType(t) }

func checkIRValTypes(what string, ts []wasm.ValType) error {
	for i, t := range ts {
		if !supportsIRValType(t) {
			return fmt.Errorf("%s %d has unsupported IR value type %s", what, i, t)
		}
	}
	return nil
}

func checkIRFuncType(what string, ft wasm.FuncType) error {
	if err := checkIRValTypes(what+" param", ft.Params); err != nil {
		return err
	}
	return checkIRValTypes(what+" result", ft.Results)
}

func valTypeByte(b byte) (wasm.ValType, bool) {
	switch b {
	case 0x7f:
		return wasm.I32, true
	case 0x7e:
		return wasm.I64, true
	case 0x7d:
		return wasm.F32, true
	case 0x7c:
		return wasm.F64, true
	case 0x7b:
		return wasm.V128, true
	case 0x70:
		return wasm.FuncRef, true
	case 0x6f:
		return wasm.ExternRef, true
	default:
		return wasm.ValType{}, false
	}
}

func (b *Builder) lowerBlock(in, out []wasm.ValType) error {
	params, err := b.popValues(in)
	if err != nil {
		return err
	}
	height := len(b.stack)
	body := b.newBlock(in)
	merge := b.newBlock(out)
	if b.reachable {
		b.setBr(body, params)
	}
	b.cur = body
	b.reachable = b.preds[body] > 0
	b.stack = append(b.stack[:height], b.blockParams(body)...)
	b.labels = append(b.labels, label{kind: labelBlock, target: merge, types: out})
	b.ctrlH = append(b.ctrlH, height)
	stop, err := b.parseSeq(false)
	if err != nil {
		return err
	}
	if stop != 0x0b {
		return fmt.Errorf("block ended by else")
	}
	if err := b.branchFallthrough(merge, out, height, "block"); err != nil {
		return err
	}
	b.labels = b.labels[:len(b.labels)-1]
	b.ctrlH = b.ctrlH[:len(b.ctrlH)-1]
	b.cur = merge
	b.reachable = b.preds[merge] > 0
	b.stack = append(b.stack[:height], b.blockParams(merge)...)
	return nil
}

func (b *Builder) lowerLoop(in, out []wasm.ValType) error {
	params, err := b.popValues(in)
	if err != nil {
		return err
	}
	height := len(b.stack)
	header := b.newBlock(in)
	after := b.newBlock(out)
	if b.reachable {
		b.setBr(header, params)
	}
	b.cur = header
	b.reachable = b.preds[header] > 0
	b.stack = append(b.stack[:height], b.blockParams(header)...)
	b.labels = append(b.labels, label{kind: labelLoop, target: header, types: in})
	b.ctrlH = append(b.ctrlH, height)
	stop, err := b.parseSeq(false)
	if err != nil {
		return err
	}
	if stop != 0x0b {
		return fmt.Errorf("loop ended by else")
	}
	if err := b.branchFallthrough(after, out, height, "loop"); err != nil {
		return err
	}
	b.labels = b.labels[:len(b.labels)-1]
	b.ctrlH = b.ctrlH[:len(b.ctrlH)-1]
	b.cur = after
	b.reachable = b.preds[after] > 0
	b.stack = append(b.stack[:height], b.blockParams(after)...)
	return nil
}

func (b *Builder) lowerIf(in, out []wasm.ValType) error {
	cond, err := b.popTyped(wasm.I32)
	if err != nil {
		return err
	}
	params, err := b.popValues(in)
	if err != nil {
		return err
	}
	height := len(b.stack)
	thenB := b.newBlock(in)
	elseB := b.newBlock(in)
	merge := b.newBlock(out)
	if b.reachable {
		b.setCondBr(cond, thenB, params, elseB, params)
	}
	b.labels = append(b.labels, label{kind: labelBlock, target: merge, types: out})
	b.ctrlH = append(b.ctrlH, height)
	b.cur = thenB
	b.reachable = b.preds[thenB] > 0
	b.stack = append(b.stack[:height], b.blockParams(thenB)...)
	stop, err := b.parseSeq(false)
	if err != nil {
		return err
	}
	if stop != 0x05 && stop != 0x0b {
		return fmt.Errorf("if missing end")
	}
	// Wasm requires an if-without-else to preserve its block parameters
	// unchanged. Enforce this even in unreachable code so BuildFunc remains a
	// defensive lowering boundary when called without a prior validator pass.
	if stop == 0x0b && !sameTypes(in, out) {
		return fmt.Errorf("if without else type mismatch")
	}
	if err := b.branchFallthrough(merge, out, height, "then"); err != nil {
		return err
	}
	b.cur = elseB
	b.reachable = b.preds[elseB] > 0
	b.stack = append(b.stack[:height], b.blockParams(elseB)...)
	if stop == 0x05 {
		stop, err = b.parseSeq(false)
		if err != nil {
			return err
		}
		if stop != 0x0b {
			return fmt.Errorf("else missing end")
		}
	}
	if err := b.branchFallthrough(merge, out, height, "else"); err != nil {
		return err
	}
	b.labels = b.labels[:len(b.labels)-1]
	b.ctrlH = b.ctrlH[:len(b.ctrlH)-1]
	b.cur = merge
	b.reachable = b.preds[merge] > 0
	b.stack = append(b.stack[:height], b.blockParams(merge)...)
	return nil
}

func (b *Builder) lowerBr(depth uint32) error {
	l, err := b.labelAt(depth)
	if err != nil {
		return err
	}
	args, err := b.popValues(l.types)
	if err != nil {
		return err
	}
	if b.reachable {
		if err := b.branchTo(l, args); err != nil {
			return err
		}
	}
	b.setUnreachable()
	return nil
}

func (b *Builder) lowerBrIf(depth uint32) error {
	cond, err := b.popTyped(wasm.I32)
	if err != nil {
		return err
	}
	l, err := b.labelAt(depth)
	if err != nil {
		return err
	}
	args, err := b.popValues(l.types)
	if err != nil {
		return err
	}
	height := len(b.stack)
	cont := b.newBlock(l.types)
	if b.reachable {
		if l.kind == labelFunc {
			ret := b.makeReturnBlock(l.types)
			b.setCondBr(cond, ret, args, cont, args)
		} else {
			b.setCondBr(cond, l.target, args, cont, args)
		}
	}
	b.cur = cont
	b.reachable = b.preds[cont] > 0
	b.stack = append(b.stack[:height], b.blockParams(cont)...)
	return nil
}

func (b *Builder) lowerBrTable() error {
	n, err := b.r.U32()
	if err != nil {
		return err
	}
	// A br_table target vector is untrusted until the builder has consumed it.
	// Each label depth, including the default, needs at least one byte of LEB128
	// encoding, so bound the count before reserving any edge storage.
	if uint64(n)+1 > uint64(b.r.BytesLeft()) {
		return fmt.Errorf("br_table label count %d exceeds remaining bytecode", n)
	}
	// Store label depths directly in the final edge buffer as temporary
	// placeholders. Reachable switches rewrite this range in place after the
	// default target establishes the required branch type; unreachable switches
	// truncate it. This avoids a large depth slice plus a second temporary edge
	// slice for br_table-heavy modules.
	edgeStart := len(b.fn.Edges)
	for i := uint32(0); i < n; i++ {
		d, err := b.r.U32()
		if err != nil {
			b.fn.Edges = b.fn.Edges[:edgeStart]
			return err
		}
		b.fn.Edges = append(b.fn.Edges, Edge{To: BlockID(d)})
	}
	def, err := b.r.U32()
	if err != nil {
		b.fn.Edges = b.fn.Edges[:edgeStart]
		return err
	}
	idx, err := b.popTyped(wasm.I32)
	if err != nil {
		b.fn.Edges = b.fn.Edges[:edgeStart]
		return err
	}
	dl, err := b.labelAt(def)
	if err != nil {
		b.fn.Edges = b.fn.Edges[:edgeStart]
		return err
	}
	args, err := b.popValues(dl.types)
	if err != nil {
		b.fn.Edges = b.fn.Edges[:edgeStart]
		return err
	}
	for i := uint32(0); i < n; i++ {
		d := uint32(b.fn.Edges[edgeStart+int(i)].To)
		l, err := b.labelAt(d)
		if err != nil {
			b.fn.Edges = b.fn.Edges[:edgeStart]
			return err
		}
		if !sameTypes(l.types, dl.types) {
			b.fn.Edges = b.fn.Edges[:edgeStart]
			return fmt.Errorf("br_table label type mismatch")
		}
		if b.reachable {
			b.fn.Edges[edgeStart+int(i)] = b.edgeForLabel(l, args)
		}
	}
	if b.reachable {
		b.fn.Edges = append(b.fn.Edges, b.edgeForLabel(dl, args))
		b.setSwitchRange(idx, edgeStart, int(n)+1)
	} else {
		b.fn.Edges = b.fn.Edges[:edgeStart]
	}
	b.setUnreachable()
	return nil
}

func (b *Builder) lowerSimple(op byte) error {
	switch {
	case op == 0x10:
		fi, err := b.r.U32()
		if err != nil {
			return err
		}
		ft, err := b.funcType(fi)
		if err != nil {
			return err
		}
		args, err := b.popValues(ft.Params)
		if err != nil {
			return err
		}
		if b.reachable {
			res := b.addInst(callOp(fi, uint32(b.m.ImportedFuncCount())), uint64(fi), 0, args, ft.Results, EffectCanTrap|EffectCall|hostEffect(fi, uint32(b.m.ImportedFuncCount())))
			b.pushValues(res)
		} else {
			b.pushPoisons(ft.Results)
		}
	case op == 0x11:
		ti, err := b.r.U32()
		if err != nil {
			return err
		}
		tbl, err := b.r.U32()
		if err != nil {
			return err
		}
		tt, err := b.tableType(tbl)
		if err != nil {
			return err
		}
		ref := tableRefType(tt)
		if !isFuncRefTableType(b.m, ref) {
			return fmt.Errorf("call_indirect table %d has element type %s", tbl, wasm.RefVal(ref))
		}
		ftc, ok := b.m.TypeFunc(ti)
		if !ok {
			return fmt.Errorf("unknown type %d", ti)
		}
		ft := funcTypeFromComp(ftc)
		if err := checkIRFuncType(fmt.Sprintf("call_indirect type %d", ti), ft); err != nil {
			return err
		}
		callee, err := b.popTyped(tableAddrType(tt))
		if err != nil {
			return err
		}
		args, err := b.popValues(ft.Params)
		if err != nil {
			return err
		}
		args = append(args, callee)
		if b.reachable {
			res := b.addInst(OpCallIndirect, packCallIndirect(ti, tbl), uint64(b.m.CanonicalTypeID(ti)), args, ft.Results, EffectCanTrap|EffectCall|EffectReadTable)
			b.pushValues(res)
		} else {
			b.pushPoisons(ft.Results)
		}
	case op == 0x1a:
		_, err := b.popAny()
		return err
	case op == 0x1b || op == 0x1c:
		var typ wasm.ValType
		if op == 0x1c {
			n, err := b.r.U32()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("select result arity %d", n)
			}
			t, err := b.readValType()
			if err != nil {
				return err
			}
			if !supportsIRValType(t) {
				return fmt.Errorf("unsupported IR value type %s in select result", t)
			}
			typ = t
		}
		cond, err := b.popTyped(wasm.I32)
		if err != nil {
			return err
		}
		bval, err := b.popMaybe(typ)
		if err != nil {
			return err
		}
		aval, err := b.popMaybe(typeOf(b.fn, bval))
		if err != nil {
			return err
		}
		if typ == (wasm.ValType{}) {
			typ = typeOf(b.fn, aval)
		}
		if b.reachable {
			res := b.addInst(OpSelect, packValType(typ), 0, []ValueID{aval, bval, cond}, []wasm.ValType{typ}, EffectNone)
			b.pushValues(res)
		} else {
			b.pushPoisons([]wasm.ValType{typ})
		}
	case op >= 0x20 && op <= 0x22:
		x, err := b.r.U32()
		if err != nil {
			return err
		}
		t, ok := b.localType(x)
		if !ok {
			return fmt.Errorf("unknown local %d", x)
		}
		switch op {
		case 0x20:
			if b.reachable {
				b.pushValues(b.addInst(OpLocalGet, uint64(x), 0, nil, []wasm.ValType{t}, EffectReadLocal))
			} else {
				b.pushPoisons([]wasm.ValType{t})
			}
		case 0x21:
			v, err := b.popTyped(t)
			if err != nil {
				return err
			}
			if b.reachable {
				b.addInst(OpLocalSet, uint64(x), 0, []ValueID{v}, nil, EffectWriteLocal)
			}
		case 0x22:
			v, err := b.popTyped(t)
			if err != nil {
				return err
			}
			if b.reachable {
				// local.tee writes the local and forwards the operand value; it does not
				// read the previous local value, so only model the write dependency.
				res := b.addInst(OpLocalTee, uint64(x), 0, []ValueID{v}, []wasm.ValType{t}, EffectWriteLocal)
				b.pushValues(res)
			} else {
				b.pushPoisons([]wasm.ValType{t})
			}
		}
	case op == 0x23 || op == 0x24:
		x, err := b.r.U32()
		if err != nil {
			return err
		}
		gt, err := b.globalType(x)
		if err != nil {
			return err
		}
		if op == 0x23 {
			if b.reachable {
				b.pushValues(b.addInst(OpGlobalGet, uint64(x), 0, nil, []wasm.ValType{gt.Type}, EffectReadGlobal))
			} else {
				b.pushPoisons([]wasm.ValType{gt.Type})
			}
		} else {
			if !gt.Mutable {
				return fmt.Errorf("immutable global %d", x)
			}
			v, err := b.popTyped(gt.Type)
			if err != nil {
				return err
			}
			if b.reachable {
				b.addInst(OpGlobalSet, uint64(x), 0, []ValueID{v}, nil, EffectWriteGlobal)
			}
		}
	case op >= 0x28 && op <= 0x3e:
		return b.lowerMem(op)
	case op == 0x3f:
		mem, err := b.readZeroMemoryImmediate()
		if err != nil {
			return err
		}
		mt, err := b.memoryType(mem)
		if err != nil {
			return err
		}
		addr := memoryAddrType(mt)
		if b.reachable {
			// memory.size observes mutable memory state: a preceding memory.grow can
			// change the result, so it must not be modeled as a pure instruction.
			b.pushValues(b.addInst(OpMemorySize, uint64(mem), 0, nil, []wasm.ValType{addr}, EffectReadMem))
		} else {
			b.pushPoisons([]wasm.ValType{addr})
		}
	case op == 0x40:
		mem, err := b.readZeroMemoryImmediate()
		if err != nil {
			return err
		}
		mt, err := b.memoryType(mem)
		if err != nil {
			return err
		}
		addr := memoryAddrType(mt)
		pages, err := b.popTyped(addr)
		if err != nil {
			return err
		}
		if b.reachable {
			b.pushValues(b.addInst(OpMemoryGrow, uint64(mem), 0, []ValueID{pages}, []wasm.ValType{addr}, EffectReadMem|EffectWriteMem))
		} else {
			b.pushPoisons([]wasm.ValType{addr})
		}
	case op >= 0x41 && op <= 0x44:
		return b.lowerConst(op)
	case op >= 0x45 && op <= 0xbf || op >= 0xc0 && op <= 0xc4:
		return b.lowerNumeric(op)
	case op == 0xfc:
		return b.lowerFC()
	default:
		return fmt.Errorf("unsupported opcode 0x%02x", op)
	}
	return nil
}

func (b *Builder) lowerConst(op byte) error {
	var aux uint64
	var t wasm.ValType
	var err error
	switch op {
	case 0x41:
		var v int32
		v, err = b.r.I32()
		aux = uint64(uint32(v))
		t = wasm.I32
	case 0x42:
		var v int64
		v, err = b.r.I64()
		aux = uint64(v)
		t = wasm.I64
	case 0x43:
		var v uint32
		v, err = b.r.LEU32()
		aux = uint64(v)
		t = wasm.F32
	case 0x44:
		aux, err = b.r.LEU64()
		t = wasm.F64
	}
	if err != nil {
		return err
	}
	if b.reachable {
		b.pushValues(b.addInst(OpConst, aux, 0, nil, []wasm.ValType{t}, EffectNone))
	} else {
		b.pushPoisons([]wasm.ValType{t})
	}
	return nil
}

func (b *Builder) lowerMem(op byte) error {
	align, err := b.r.U32()
	if err != nil {
		return err
	}
	off, err := b.r.U32()
	if err != nil {
		return err
	}
	mt, err := b.memoryType(0)
	if err != nil {
		return err
	}
	addrType := memoryAddrType(mt)
	kind, res, arg, store := memInfo(op)
	if align > naturalMemAlign(kind) {
		return fmt.Errorf("invalid memory alignment %d for opcode 0x%02x", align, op)
	}
	if store {
		val, err := b.popTyped(arg)
		if err != nil {
			return err
		}
		addr, err := b.popTyped(addrType)
		if err != nil {
			return err
		}
		if b.reachable {
			b.addInst(OpStore, packMem(kind, align, 0, off), 0, []ValueID{addr, val}, nil, EffectCanTrap|EffectWriteMem)
		}
	} else {
		addr, err := b.popTyped(addrType)
		if err != nil {
			return err
		}
		if b.reachable {
			b.pushValues(b.addInst(OpLoad, packMem(kind, align, 0, off), 0, []ValueID{addr}, []wasm.ValType{res}, EffectCanTrap|EffectReadMem))
		} else {
			b.pushPoisons([]wasm.ValType{res})
		}
	}
	return nil
}

func memInfo(op byte) (MemOp, wasm.ValType, wasm.ValType, bool) {
	kind, store := memOpcodeKind(op)
	d, _ := lookupMemDesc(kind)
	if store {
		return kind, wasm.ValType{}, d.storeValue, true
	}
	return kind, d.loadResult, wasm.ValType{}, false
}

func memOpcodeKind(op byte) (MemOp, bool) {
	switch op {
	case 0x28:
		return MemI32, false
	case 0x29:
		return MemI64, false
	case 0x2a:
		return MemF32, false
	case 0x2b:
		return MemF64, false
	case 0x2c:
		return MemI32Load8S, false
	case 0x2d:
		return MemI32Load8U, false
	case 0x2e:
		return MemI32Load16S, false
	case 0x2f:
		return MemI32Load16U, false
	case 0x30:
		return MemI64Load8S, false
	case 0x31:
		return MemI64Load8U, false
	case 0x32:
		return MemI64Load16S, false
	case 0x33:
		return MemI64Load16U, false
	case 0x34:
		return MemI64Load32S, false
	case 0x35:
		return MemI64Load32U, false
	case 0x36:
		return MemI32, true
	case 0x37:
		return MemI64, true
	case 0x38:
		return MemF32, true
	case 0x39:
		return MemF64, true
	case 0x3a:
		return MemI32Store8, true
	case 0x3b:
		return MemI32Store16, true
	case 0x3c:
		return MemI64Store8, true
	case 0x3d:
		return MemI64Store16, true
	default:
		return MemI64Store32, true
	}
}

func naturalMemAlign(kind MemOp) uint32 {
	if d, ok := lookupMemDesc(kind); ok {
		return d.naturalAlign
	}
	return 0
}

func (b *Builder) lowerNumeric(op byte) error {
	// Integer tests/comparisons/unary/binary, float ops, conversions and reinterprets.
	if op == 0x45 || op == 0x50 {
		t := wasm.I32
		if op == 0x50 {
			t = wasm.I64
		}
		a, err := b.popTyped(t)
		if err != nil {
			return err
		}
		if b.reachable {
			b.pushValues(b.addInst(OpITest, packKindType(uint8(ITestEqz), t), 0, []ValueID{a}, []wasm.ValType{wasm.I32}, EffectNone))
		} else {
			b.pushPoisons([]wasm.ValType{wasm.I32})
		}
		return nil
	}
	if (op >= 0x46 && op <= 0x4f) || (op >= 0x51 && op <= 0x5a) || (op >= 0x5b && op <= 0x60) || (op >= 0x61 && op <= 0x66) {
		return b.lowerCmp(op)
	}
	if (op >= 0x67 && op <= 0x69) || (op >= 0x79 && op <= 0x7b) || (op >= 0xc0 && op <= 0xc4) {
		return b.lowerIUn(op)
	}
	if (op >= 0x6a && op <= 0x78) || (op >= 0x7c && op <= 0x8a) {
		return b.lowerIBin(op)
	}
	if (op >= 0x8b && op <= 0x91) || (op >= 0x99 && op <= 0x9f) {
		return b.lowerFUn(op)
	}
	if (op >= 0x92 && op <= 0x98) || (op >= 0xa0 && op <= 0xa6) {
		return b.lowerFBin(op)
	}
	return b.lowerConvert(op)
}

func (b *Builder) lowerCmp(op byte) error {
	t := wasm.I32
	kind := uint8(0)
	irOp := OpICmp
	if op >= 0x51 && op <= 0x5a {
		t = wasm.I64
		kind = uint8(op-0x51) + 1
	} else if op >= 0x46 && op <= 0x4f {
		kind = uint8(op-0x46) + 1
	} else if op >= 0x5b && op <= 0x60 {
		t = wasm.F32
		irOp = OpFCmp
		kind = uint8(op-0x5b) + 1
	} else {
		t = wasm.F64
		irOp = OpFCmp
		kind = uint8(op-0x61) + 1
	}
	bval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	aval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	if b.reachable {
		b.pushValues(b.addInst(irOp, packKindType(kind, t), 0, []ValueID{aval, bval}, []wasm.ValType{wasm.I32}, EffectNone))
	} else {
		b.pushPoisons([]wasm.ValType{wasm.I32})
	}
	return nil
}
func (b *Builder) lowerIUn(op byte) error {
	t := wasm.I32
	base := byte(0x67)
	if op >= 0x79 && op <= 0x7b {
		t = wasm.I64
		base = 0x79
	}
	var k uint8
	if op >= 0xc0 {
		if op <= 0xc1 {
			t = wasm.I32
			k = uint8(IUnExtend8S) + uint8(op-0xc0)
		} else {
			t = wasm.I64
			k = uint8(IUnExtend8S) + uint8(op-0xc2)
		}
	} else {
		k = uint8(op-base) + 1
	}
	a, err := b.popTyped(t)
	if err != nil {
		return err
	}
	if b.reachable {
		b.pushValues(b.addInst(OpIUnary, packKindType(k, t), 0, []ValueID{a}, []wasm.ValType{t}, EffectNone))
	} else {
		b.pushPoisons([]wasm.ValType{t})
	}
	return nil
}
func (b *Builder) lowerIBin(op byte) error {
	t := wasm.I32
	base := byte(0x6a)
	if op >= 0x7c {
		t = wasm.I64
		base = 0x7c
	}
	k := uint8(op-base) + 1
	bval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	aval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	eff := EffectNone
	if k >= uint8(IBinDivS) && k <= uint8(IBinRemU) {
		eff = EffectCanTrap
	}
	if b.reachable {
		b.pushValues(b.addInst(OpIBinary, packKindType(k, t), 0, []ValueID{aval, bval}, []wasm.ValType{t}, eff))
	} else {
		b.pushPoisons([]wasm.ValType{t})
	}
	return nil
}
func (b *Builder) lowerFUn(op byte) error {
	t := wasm.F32
	base := byte(0x8b)
	if op >= 0x99 {
		t = wasm.F64
		base = 0x99
	}
	k := uint8(op-base) + 1
	a, err := b.popTyped(t)
	if err != nil {
		return err
	}
	if b.reachable {
		b.pushValues(b.addInst(OpFUnary, packKindType(k, t), 0, []ValueID{a}, []wasm.ValType{t}, EffectNone))
	} else {
		b.pushPoisons([]wasm.ValType{t})
	}
	return nil
}
func (b *Builder) lowerFBin(op byte) error {
	t := wasm.F32
	base := byte(0x92)
	if op >= 0xa0 {
		t = wasm.F64
		base = 0xa0
	}
	k := uint8(op-base) + 1
	bval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	aval, err := b.popTyped(t)
	if err != nil {
		return err
	}
	if b.reachable {
		b.pushValues(b.addInst(OpFBinary, packKindType(k, t), 0, []ValueID{aval, bval}, []wasm.ValType{t}, EffectNone))
	} else {
		b.pushPoisons([]wasm.ValType{t})
	}
	return nil
}

func (b *Builder) lowerConvert(op byte) error {
	src, dst, kind, reint, trap, err := convertInfo(op)
	if err != nil {
		return err
	}
	a, err := b.popTyped(src)
	if err != nil {
		return err
	}
	irOp := OpConvert
	if reint {
		irOp = OpReinterpret
	}
	eff := EffectNone
	if trap {
		eff = EffectCanTrap
	}
	if b.reachable {
		b.pushValues(b.addInst(irOp, packKindType(kind, dst), 0, []ValueID{a}, []wasm.ValType{dst}, eff))
	} else {
		b.pushPoisons([]wasm.ValType{dst})
	}
	return nil
}

func convertInfo(op byte) (src, dst wasm.ValType, kind uint8, reint bool, trap bool, err error) {
	switch op {
	case 0xa7:
		return wasm.I64, wasm.I32, uint8(ConvWrapI64ToI32), false, false, nil
	case 0xa8:
		return wasm.F32, wasm.I32, uint8(ConvTruncFToIS), false, true, nil
	case 0xa9:
		return wasm.F32, wasm.I32, uint8(ConvTruncFToIU), false, true, nil
	case 0xaa:
		return wasm.F64, wasm.I32, uint8(ConvTruncFToIS), false, true, nil
	case 0xab:
		return wasm.F64, wasm.I32, uint8(ConvTruncFToIU), false, true, nil
	case 0xac:
		return wasm.I32, wasm.I64, uint8(ConvExtendI32S), false, false, nil
	case 0xad:
		return wasm.I32, wasm.I64, uint8(ConvExtendI32U), false, false, nil
	case 0xae:
		return wasm.F32, wasm.I64, uint8(ConvTruncFToIS), false, true, nil
	case 0xaf:
		return wasm.F32, wasm.I64, uint8(ConvTruncFToIU), false, true, nil
	case 0xb0:
		return wasm.F64, wasm.I64, uint8(ConvTruncFToIS), false, true, nil
	case 0xb1:
		return wasm.F64, wasm.I64, uint8(ConvTruncFToIU), false, true, nil
	case 0xb2:
		return wasm.I32, wasm.F32, uint8(ConvConvertIToFS), false, false, nil
	case 0xb3:
		return wasm.I32, wasm.F32, uint8(ConvConvertIToFU), false, false, nil
	case 0xb4:
		return wasm.I64, wasm.F32, uint8(ConvConvertIToFS), false, false, nil
	case 0xb5:
		return wasm.I64, wasm.F32, uint8(ConvConvertIToFU), false, false, nil
	case 0xb6:
		return wasm.F64, wasm.F32, uint8(ConvDemoteF64ToF32), false, false, nil
	case 0xb7:
		return wasm.I32, wasm.F64, uint8(ConvConvertIToFS), false, false, nil
	case 0xb8:
		return wasm.I32, wasm.F64, uint8(ConvConvertIToFU), false, false, nil
	case 0xb9:
		return wasm.I64, wasm.F64, uint8(ConvConvertIToFS), false, false, nil
	case 0xba:
		return wasm.I64, wasm.F64, uint8(ConvConvertIToFU), false, false, nil
	case 0xbb:
		return wasm.F32, wasm.F64, uint8(ConvPromoteF32ToF64), false, false, nil
	case 0xbc:
		return wasm.F32, wasm.I32, uint8(ReinterpF32ToI32), true, false, nil
	case 0xbd:
		return wasm.F64, wasm.I64, uint8(ReinterpF64ToI64), true, false, nil
	case 0xbe:
		return wasm.I32, wasm.F32, uint8(ReinterpI32ToF32), true, false, nil
	case 0xbf:
		return wasm.I64, wasm.F64, uint8(ReinterpI64ToF64), true, false, nil
	default:
		return wasm.ValType{}, wasm.ValType{}, 0, false, false, fmt.Errorf("unsupported conversion opcode 0x%02x", op)
	}
}

func (b *Builder) lowerFC() error {
	sub, err := b.r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		src := wasm.F32
		if sub == 2 || sub == 3 || sub == 6 || sub == 7 {
			src = wasm.F64
		}
		dst := wasm.I32
		if sub >= 4 {
			dst = wasm.I64
		}
		kind := uint8(ConvTruncSatFToIS)
		if sub%2 == 1 {
			kind = uint8(ConvTruncSatFToIU)
		}
		a, err := b.popTyped(src)
		if err != nil {
			return err
		}
		if b.reachable {
			b.pushValues(b.addInst(OpConvert, packKindType(kind, dst), 0, []ValueID{a}, []wasm.ValType{dst}, EffectNone))
		} else {
			b.pushPoisons([]wasm.ValType{dst})
		}
		return nil
	case 10:
		dst, err := b.readZeroMemoryImmediate()
		if err != nil {
			return err
		}
		src, err := b.readZeroMemoryImmediate()
		if err != nil {
			return err
		}
		dstMT, err := b.memoryType(dst)
		if err != nil {
			return err
		}
		srcMT, err := b.memoryType(src)
		if err != nil {
			return err
		}
		dstAddr := memoryAddrType(dstMT)
		srcAddr := memoryAddrType(srcMT)
		n, err := b.popTyped(minAddrType(dstAddr, srcAddr))
		if err != nil {
			return err
		}
		s, err := b.popTyped(srcAddr)
		if err != nil {
			return err
		}
		d, err := b.popTyped(dstAddr)
		if err != nil {
			return err
		}
		if b.reachable {
			b.addInst(OpMemoryCopy, uint64(dst)|uint64(src)<<32, 0, []ValueID{d, s, n}, nil, EffectCanTrap|EffectReadMem|EffectWriteMem)
		}
		return nil
	case 11:
		mem, err := b.readZeroMemoryImmediate()
		if err != nil {
			return err
		}
		mt, err := b.memoryType(mem)
		if err != nil {
			return err
		}
		addr := memoryAddrType(mt)
		n, err := b.popTyped(addr)
		if err != nil {
			return err
		}
		val, err := b.popTyped(wasm.I32)
		if err != nil {
			return err
		}
		dst, err := b.popTyped(addr)
		if err != nil {
			return err
		}
		if b.reachable {
			b.addInst(OpMemoryFill, uint64(mem), 0, []ValueID{dst, val, n}, nil, EffectCanTrap|EffectWriteMem)
		}
		return nil
	default:
		return fmt.Errorf("unsupported 0xfc opcode %d", sub)
	}
}

// --- allocation-conscious IR append helpers ---
func (b *Builder) newBlock(params []wasm.ValType) BlockID {
	id := BlockID(len(b.fn.Blocks))
	b.fn.Blocks = append(b.fn.Blocks, Block{Term: Term{Kind: TermInvalid}})
	b.preds = append(b.preds, 0)
	if len(params) > 0 {
		start := len(b.fn.ValueIDs)
		for _, t := range params {
			vid := b.newValue(t, ValueDefBlockParam, uint32(id))
			b.fn.ValueIDs = append(b.fn.ValueIDs, vid)
		}
		b.fn.Blocks[id].Params = Range{uint32(start), uint32(len(params))}
	}
	return id
}
func (b *Builder) blockParams(id BlockID) []ValueID {
	r := b.fn.Blocks[id].Params
	return b.fn.ValueIDs[r.Start:r.End()]
}
func (b *Builder) newValue(t wasm.ValType, k ValueDefKind, def uint32) ValueID {
	id := ValueID(len(b.fn.Values))
	b.fn.Values = append(b.fn.Values, Value{Type: t, DefKind: k, Def: def})
	return id
}
func (b *Builder) appendValues(vals []ValueID) Range {
	if len(vals) == 0 {
		return Range{}
	}
	st := len(b.fn.ValueIDs)
	b.fn.ValueIDs = append(b.fn.ValueIDs, vals...)
	return Range{uint32(st), uint32(len(vals))}
}
func (b *Builder) addInst(op Op, aux, aux2 uint64, args []ValueID, results []wasm.ValType, eff EffectFlags) []ValueID {
	id := InstID(len(b.fn.Insts))
	ar := b.appendValues(args)
	rr := Range{}
	if len(results) > 0 {
		st := len(b.fn.ValueIDs)
		for _, t := range results {
			b.fn.ValueIDs = append(b.fn.ValueIDs, b.newValue(t, ValueDefInst, uint32(id)))
		}
		rr = Range{uint32(st), uint32(len(results))}
	}
	b.fn.Insts = append(b.fn.Insts, Inst{Op: op, Args: ar, Results: rr, Aux: aux, Aux2: aux2, Effects: eff})
	blk := &b.fn.Blocks[b.cur]
	if blk.Insts.Len == 0 {
		blk.Insts.Start = uint32(id)
	}
	blk.Insts.Len++
	return b.fn.ValueIDs[rr.Start:rr.End()]
}
func (b *Builder) pushValues(v []ValueID) { b.stack = append(b.stack, v...) }
func (b *Builder) pushPoisons(ts []wasm.ValType) {
	for _, t := range ts {
		b.stack = append(b.stack, b.newValue(t, ValueDefPoison, 0))
	}
}
func (b *Builder) popValues(ts []wasm.ValType) ([]ValueID, error) {
	if len(ts) == 0 {
		return nil, nil
	}
	vals := make([]ValueID, len(ts))
	for i := len(ts) - 1; i >= 0; i-- {
		v, err := b.popTyped(ts[i])
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}
func (b *Builder) popTyped(t wasm.ValType) (ValueID, error) {
	if !supportsIRValType(t) {
		return InvalidValue, fmt.Errorf("unsupported IR value type %s", t)
	}
	if len(b.stack) <= b.ctrlH[len(b.ctrlH)-1] {
		if !b.reachable {
			return b.newValue(t, ValueDefPoison, 0), nil
		}
		return InvalidValue, fmt.Errorf("stack underflow popping %s", t)
	}
	v := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	if got := typeOf(b.fn, v); got != t && got != (wasm.ValType{}) {
		return InvalidValue, fmt.Errorf("type mismatch: got %s want %s", got, t)
	}
	return v, nil
}
func (b *Builder) popMaybe(t wasm.ValType) (ValueID, error) {
	if t != (wasm.ValType{}) {
		return b.popTyped(t)
	}
	// Untyped pops (drop and untyped select) are stack-polymorphic in unreachable
	// code too. Do not consume values below the current control-frame height: they
	// belong to the enclosing expression and must still be available after this
	// unreachable region closes.
	if len(b.stack) <= b.ctrlH[len(b.ctrlH)-1] {
		if !b.reachable {
			return b.newValue(wasm.I32, ValueDefPoison, 0), nil
		}
		return InvalidValue, fmt.Errorf("stack underflow")
	}
	v := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	return v, nil
}
func (b *Builder) popAny() (ValueID, error) { return b.popMaybe(wasm.ValType{}) }
func (b *Builder) setUnreachable() {
	h := b.ctrlH[len(b.ctrlH)-1]
	if len(b.stack) > h {
		b.stack = b.stack[:h]
	}
	b.reachable = false
}
func (b *Builder) branchFallthrough(to BlockID, out []wasm.ValType, height int, context string) error {
	if !b.reachable {
		return nil
	}
	vals, err := b.popValues(out)
	if err != nil {
		return err
	}
	// Structured control must restore the operand stack to the enclosing frame
	// height before transferring fallthrough values to the merge block.
	if len(b.stack) != height {
		return fmt.Errorf("%s fallthrough has %d leftover stack values", context, len(b.stack)-height)
	}
	b.setBr(to, vals)
	return nil
}
func (b *Builder) addEdge(to BlockID, args []ValueID) Edge {
	b.preds[to]++
	return Edge{To: to, Args: b.appendValues(args)}
}
func (b *Builder) setBr(to BlockID, args []ValueID) {
	e := b.addEdge(to, args)
	st := len(b.fn.Edges)
	b.fn.Edges = append(b.fn.Edges, e)
	b.fn.Blocks[b.cur].Term = Term{Kind: TermBr, Edges: Range{uint32(st), 1}}
}
func (b *Builder) setCondBr(cond ValueID, t BlockID, targs []ValueID, f BlockID, fargs []ValueID) {
	st := len(b.fn.Edges)
	b.fn.Edges = append(b.fn.Edges, b.addEdge(t, targs), b.addEdge(f, fargs))
	b.fn.Blocks[b.cur].Term = Term{Kind: TermCondBr, Cond: cond, Edges: Range{uint32(st), 2}}
}
func (b *Builder) setSwitchRange(idx ValueID, start, n int) {
	b.fn.Blocks[b.cur].Term = Term{Kind: TermSwitch, Index: idx, Edges: Range{uint32(start), uint32(n)}}
}
func (b *Builder) setReturn(args []ValueID) {
	b.fn.Blocks[b.cur].Term = Term{Kind: TermReturn, Args: b.appendValues(args)}
}
func (b *Builder) setTrap() { b.fn.Blocks[b.cur].Term = Term{Kind: TermTrap} }
func (b *Builder) labelAt(depth uint32) (label, error) {
	if int(depth) >= len(b.labels) {
		return label{}, fmt.Errorf("unknown label depth %d", depth)
	}
	return b.labels[len(b.labels)-1-int(depth)], nil
}
func (b *Builder) branchTo(l label, args []ValueID) error {
	if l.kind == labelFunc {
		b.setReturn(args)
	} else {
		b.setBr(l.target, args)
	}
	return nil
}
func (b *Builder) makeReturnBlock(ts []wasm.ValType) BlockID {
	// All branches to the function label have the function result types, so one
	// synthetic return block is enough. Reusing it keeps br_table-heavy functions
	// from allocating many identical return-only blocks and gives later CFG cleanup
	// a single canonical sink for function-label branches.
	if b.returnBlock != InvalidBlock {
		return b.returnBlock
	}
	blk := b.newBlock(ts)
	b.returnBlock = blk
	// Branch-like terminators can only target blocks, so branches to the function
	// label are represented as a tiny return block. Mark it explicitly so codegen
	// and CFG cleanup can distinguish synthetic returns from source blocks.
	b.fn.Blocks[blk].Flags |= BlockSyntheticReturn
	oldCur, oldReach := b.cur, b.reachable
	b.cur = blk
	b.reachable = true
	b.setReturn(b.blockParams(blk))
	b.cur = oldCur
	b.reachable = oldReach
	return blk
}
func (b *Builder) edgeForLabel(l label, args []ValueID) Edge {
	if l.kind == labelFunc {
		return b.addEdge(b.makeReturnBlock(l.types), args)
	}
	return b.addEdge(l.target, args)
}
func (b *Builder) terminateDeadBlocks() {
	for i := range b.fn.Blocks {
		if b.fn.Blocks[i].Term.Kind == TermInvalid {
			b.fn.Blocks[i].Term = Term{Kind: TermTrap}
		}
	}
}
func (b *Builder) readValType() (wasm.ValType, error) {
	x, err := b.r.Byte()
	if err != nil {
		return wasm.ValType{}, err
	}
	t, ok := valTypeByte(x)
	if !ok {
		return wasm.ValType{}, fmt.Errorf("invalid valtype 0x%x", x)
	}
	return t, nil
}
func (b *Builder) funcType(fi uint32) (*wasm.FuncType, error) {
	ti, err := b.funcTypeIndex(fi)
	if err != nil {
		return nil, err
	}
	if int(ti) >= len(b.out.Types) {
		return nil, fmt.Errorf("unknown type %d", ti)
	}
	if !irTypeIsFunc(b.out, ti) {
		return nil, fmt.Errorf("function %d references non-function type %d", fi, ti)
	}
	out := b.out.Types[ti]
	if err := checkIRFuncType(fmt.Sprintf("function %d type %d", fi, ti), out); err != nil {
		return nil, err
	}
	return &out, nil
}
func (b *Builder) funcTypeIndex(fi uint32) (uint32, error) {
	// Function metadata is flattened once when the builder is created. Use it for
	// O(1) call validation instead of re-scanning imports for every call opcode.
	if int(fi) >= len(b.out.FuncTypes) {
		return 0, fmt.Errorf("unknown function %d", fi)
	}
	return b.out.FuncTypes[fi], nil
}
func (b *Builder) globalType(x uint32) (wasm.GlobalType, error) {
	if int(x) >= len(b.out.Globals) {
		return wasm.GlobalType{}, fmt.Errorf("unknown global %d", x)
	}
	gt := b.out.Globals[x]
	if !supportsIRValType(globalTypeValue(gt)) {
		return wasm.GlobalType{}, fmt.Errorf("global %d has unsupported IR value type %s", x, globalTypeValue(gt))
	}
	return gt, nil
}
func (b *Builder) memoryType(x uint32) (wasm.MemType, error) {
	if x != 0 {
		return wasm.MemType{}, fmt.Errorf("multi-memory unsupported: memory index %d", x)
	}
	if int(x) >= len(b.out.Memories) {
		return wasm.MemType{}, fmt.Errorf("unknown memory %d", x)
	}
	return b.out.Memories[x], nil
}
func (b *Builder) tableType(x uint32) (wasm.TableType, error) {
	if int(x) >= len(b.out.Tables) {
		return wasm.TableType{}, fmt.Errorf("unknown table %d", x)
	}
	return b.out.Tables[x], nil
}
func typeOf(f *Func, v ValueID) wasm.ValType {
	if v == InvalidValue || int(v) >= len(f.Values) {
		return wasm.ValType{}
	}
	return f.Values[v].Type
}
func sameTypes(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func callOp(fi, imports uint32) Op {
	if fi < imports {
		return OpCallImport
	}
	return OpCall
}
func hostEffect(fi, imports uint32) EffectFlags {
	if fi < imports {
		return EffectHost
	}
	return 0
}

func rejectMultiMemory(m *wasm.Module) error {
	if memoryCount(m) > 1 {
		return fmt.Errorf("ir: multi-memory unsupported")
	}
	return nil
}

func memoryCount(m *wasm.Module) int {
	n := len(m.Memories)
	for i := range m.Imports {
		if m.Imports[i].Type.Kind == wasm.ExternMem {
			n++
		}
	}
	return n
}

func (b *Builder) readZeroMemoryImmediate() (uint32, error) {
	mem, err := b.r.U32()
	if err != nil {
		return 0, err
	}
	// wago intentionally rejects multi-memory for now. Memory instructions that
	// carry a reserved/memory immediate must therefore name memory 0, even if a
	// caller bypasses wasm.Validate and invokes the IR builder directly.
	if mem != 0 {
		return 0, fmt.Errorf("multi-memory unsupported: memory index %d", mem)
	}
	return mem, nil
}

func trimFuncStorage(f *Func) {
	// These slices are retained by the compiled IR. Clip large slack left by growth
	// so one unusually encoded function does not keep megabytes of unused backing
	// arrays alive on memory-constrained devices.
	f.Locals = trimSlack(f.Locals)
	f.LocalRuns = trimSlack(f.LocalRuns)
	f.Blocks = trimSlack(f.Blocks)
	f.Insts = trimSlack(f.Insts)
	f.Values = trimSlack(f.Values)
	f.ValueIDs = trimSlack(f.ValueIDs)
	f.Edges = trimSlack(f.Edges)
}

func trimSlack[S ~[]E, E any](s S) S {
	if len(s) == 0 {
		return nil
	}
	if cap(s) <= len(s)*2+8 {
		return s
	}
	return slices.Clip(s)
}

const localRunIndexThreshold = 8

func (b *Builder) prepareLocalLookup() {
	b.localRunStarts = b.localRunStarts[:0]
	if b.fn == nil || len(b.fn.LocalRuns) <= localRunIndexThreshold {
		return
	}
	next := uint64(len(b.fn.Locals))
	for _, run := range b.fn.LocalRuns {
		b.localRunStarts = append(b.localRunStarts, next)
		next += uint64(run.Count)
	}
}

func (b *Builder) localType(idx uint32) (wasm.ValType, bool) {
	if len(b.localRunStarts) == 0 {
		return localType(b.fn, idx)
	}
	if uint64(idx) < uint64(len(b.fn.Locals)) {
		return b.fn.Locals[idx], true
	}
	// Large local declarations stay compact in Func.LocalRuns. A builder-local
	// prefix index keeps repeated local.get/set/tee validation logarithmic without
	// retaining an expanded local-type slice in the finished IR.
	i := sort.Search(len(b.localRunStarts), func(i int) bool { return b.localRunStarts[i] > uint64(idx) }) - 1
	if i < 0 {
		return wasm.ValType{}, false
	}
	start := b.localRunStarts[i]
	run := b.fn.LocalRuns[i]
	if uint64(idx) < start+uint64(run.Count) {
		return run.Type, true
	}
	return wasm.ValType{}, false
}

func localType(f *Func, idx uint32) (wasm.ValType, bool) {
	if len(f.LocalRuns) == 0 {
		return wasm.LocalType(f.Locals, nil, idx)
	}
	params := compactLocalParams(f)
	return wasm.LocalType(params, f.LocalRuns, idx)
}

func localCount(f *Func) uint64 {
	if len(f.LocalRuns) == 0 {
		return uint64(len(f.Locals))
	}
	n, _ := wasm.LocalCount(compactLocalParams(f), f.LocalRuns)
	return n
}

func compactLocalParams(f *Func) []wasm.ValType {
	n := len(f.Sig.Params)
	if n > len(f.Locals) {
		n = len(f.Locals)
	}
	return f.Locals[:n]
}
