// Package frontend contains compiler front-end passes shared by the CLI and API.
package frontend

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm3"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

// UnsupportedError reports a feature that decodes and validates as WebAssembly
// but is outside the subset currently executable by wago's runtime/backend.
type UnsupportedError struct {
	Category string
	Feature  string
	Context  string
}

func (e *UnsupportedError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("unsupported %s %s at %s", e.Category, e.Feature, e.Context)
	}
	return fmt.Sprintf("unsupported %s %s", e.Category, e.Feature)
}

// DecodeValidate decodes, validates, and runs wago's support pass over data.
func DecodeValidate(data []byte) (*wasm3.Module, error) {
	m, err := wasm3.DecodeModule(data)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := wasm3.ValidateModule(m); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if err := RejectUnsupported(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RejectUnsupported rejects modules that require features not explicitly wired
// through the current JIT/runtime. The pass is deliberately conservative: a
// construct must be listed here before it can reach code generation.
func RejectUnsupported(m *wasm3.Module) error {
	p := supportPass{m: m}
	return p.run()
}

type supportPass struct{ m *wasm3.Module }

// SupportedTableRuntimeShape returns the runtime ABI shape for the one local
// table wago currently supports. A declared zero-length table still has runtime
// presence: call_indirect needs a descriptor whose length is zero so it can trap
// before reading an entry.
func SupportedTableRuntimeShape(m *wasm3.Module) (hasTable bool, tableSize int, err error) {
	if m == nil {
		return false, 0, fmt.Errorf("nil module")
	}
	if m.ImportedTableCount() != 0 {
		return false, 0, fmt.Errorf("imported tables are unsupported")
	}
	if len(m.Tables) == 0 {
		return false, 0, nil
	}
	if len(m.Tables) != 1 {
		return false, 0, fmt.Errorf("multiple tables are unsupported")
	}
	min := m.Tables[0].Type.Limits.Min
	if min > uint64(maxInt()) {
		return false, 0, fmt.Errorf("table minimum %d overflows int", min)
	}
	return true, int(min), nil
}

func (p supportPass) unsupported(category, feature, context string) error {
	return &UnsupportedError{Category: category, Feature: feature, Context: context}
}

func (p supportPass) run() error {
	if err := p.types(); err != nil {
		return err
	}
	if err := p.imports(); err != nil {
		return err
	}
	if err := p.tables(); err != nil {
		return err
	}
	if err := p.memories(); err != nil {
		return err
	}
	if len(p.m.Tags) != 0 {
		return p.unsupported("tag", "section", "tag section")
	}
	if len(p.m.StringRefs) != 0 {
		return p.unsupported("stringref", "section", "stringrefs section")
	}
	if p.m.Start != nil {
		return p.unsupported("start", "function", fmt.Sprintf("start function %d", *p.m.Start))
	}
	if err := p.globals(); err != nil {
		return err
	}
	if err := p.exports(); err != nil {
		return err
	}
	if err := p.elements(); err != nil {
		return err
	}
	if err := p.data(); err != nil {
		return err
	}
	if err := p.runtimeFootprint(); err != nil {
		return err
	}
	return p.funcs()
}

func (p supportPass) types() error {
	for gi, rt := range p.m.Types {
		if len(rt.SubTypes) != 1 {
			return p.unsupported("gc type", "recursive group", fmt.Sprintf("type %d", gi))
		}
		st := rt.SubTypes[0]
		if st.HasPrefix || len(st.Supers) != 0 || st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
			return p.unsupported("gc type", "subtyping metadata", fmt.Sprintf("type %d", gi))
		}
		if st.Comp.Kind != wasm3.CompFunc {
			return p.unsupported("gc type", compTypeName(st.Comp.Kind), fmt.Sprintf("type %d", gi))
		}
		if err := p.valTypes(st.Comp.Params, fmt.Sprintf("type %d params", gi)); err != nil {
			return err
		}
		if err := p.valTypes(st.Comp.Results, fmt.Sprintf("type %d results", gi)); err != nil {
			return err
		}
	}
	return nil
}

func compTypeName(k wasm3.CompTypeKind) string {
	switch k {
	case wasm3.CompArray:
		return "array type"
	case wasm3.CompStruct:
		return "struct type"
	case wasm3.CompFunc:
		return "function type"
	default:
		return "unknown type"
	}
}

func (p supportPass) imports() error {
	for i, im := range p.m.Imports {
		ctx := fmt.Sprintf("import %d %q.%q", i, im.Module, im.Name)
		switch im.Type.Kind {
		case wasm3.ExternFunc:
			ft := p.funcType(im.Type.Type)
			if ft == nil {
				return p.unsupported("import", "function with unknown type", ctx)
			}
			if len(ft.Results) != 0 {
				return p.unsupported("import", "function result", ctx)
			}
			if len(ft.Params) > 1 || (len(ft.Params) == 1 && !isNum(ft.Params[0], wasm3.NumI32)) {
				return p.unsupported("import", "function signature", ctx)
			}
		case wasm3.ExternGlobal:
			if err := p.globalType(im.Type.Global.Type, ctx); err != nil {
				return err
			}
		case wasm3.ExternTable:
			return p.unsupported("import", "table", ctx)
		case wasm3.ExternMem:
			return p.unsupported("import", "memory", ctx)
		case wasm3.ExternTag:
			return p.unsupported("import", "tag", ctx)
		default:
			return p.unsupported("import", "unknown external kind", ctx)
		}
	}
	return nil
}

func (p supportPass) tables() error {
	if p.m.ImportedTableCount()+len(p.m.Tables) > 1 {
		return p.unsupported("table", "multiple tables", "module")
	}
	for i, t := range p.m.Tables {
		ctx := fmt.Sprintf("table %d", i)
		if !isFuncRef(t.Type.Ref) {
			return p.unsupported("reference type", refTypeName(t.Type.Ref), ctx)
		}
		if t.Type.Limits.Addr64 {
			return p.unsupported("table", "64-bit limits", ctx)
		}
		if t.Init != nil {
			return p.unsupported("table", "initializer expression", ctx)
		}
	}
	return nil
}

func (p supportPass) memories() error {
	if p.m.ImportedMemCount()+len(p.m.Memories) > 1 {
		return p.unsupported("memory", "multiple memories", "module")
	}
	for i, mem := range p.m.Memories {
		ctx := fmt.Sprintf("memory %d", i)
		if mem.Shared {
			return p.unsupported("memory", "shared", ctx)
		}
		if mem.Limits.Addr64 {
			return p.unsupported("memory", "memory64", ctx)
		}
		if mem.Limits.Min > 1 {
			return p.unsupported("memory", fmt.Sprintf("minimum %d pages", mem.Limits.Min), ctx)
		}
	}
	return nil
}

func (p supportPass) globals() error {
	for i, g := range p.m.Globals {
		ctx := fmt.Sprintf("global %d", i)
		if err := p.globalType(g.Type.Type, ctx); err != nil {
			return err
		}
		if err := p.constExpr(g.Init, ctx+" initializer"); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) exports() error {
	for i, ex := range p.m.Exports {
		ctx := fmt.Sprintf("export %d %q", i, ex.Name)
		switch ex.Index.Kind {
		case wasm3.ExternFunc, wasm3.ExternGlobal:
			// Supported metadata is serialized for function and numeric-global exports.
		case wasm3.ExternTable:
			return p.unsupported("export", "table", ctx)
		case wasm3.ExternMem:
			// Memory exports are metadata-only for wago today; the instance exposes
			// linear memory directly, and preserving this keeps current MVP modules
			// that export memory runnable.
		case wasm3.ExternTag:
			return p.unsupported("export", "tag", ctx)
		default:
			return p.unsupported("export", "unknown external kind", ctx)
		}
	}
	return nil
}

func (p supportPass) elements() error {
	for i, e := range p.m.Elements {
		ctx := fmt.Sprintf("element %d", i)
		if e.Mode.Kind != wasm3.ElemActive {
			return p.unsupported("element", elemModeName(e.Mode.Kind), ctx)
		}
		if e.Mode.Table != 0 {
			return p.unsupported("element", fmt.Sprintf("table index %d", e.Mode.Table), ctx)
		}
		if err := p.constExpr(e.Mode.Offset, ctx+" offset"); err != nil {
			return err
		}
		if e.Kind.Kind != wasm3.ElemFuncs {
			return p.unsupported("reference type", elemKindName(e.Kind.Kind), ctx)
		}
	}
	return nil
}

func elemModeName(k wasm3.ElemModeKind) string {
	switch k {
	case wasm3.ElemPassive:
		return "passive segment"
	case wasm3.ElemActive:
		return "active segment"
	case wasm3.ElemDeclarative:
		return "declarative segment"
	default:
		return "unknown segment mode"
	}
}

func elemKindName(k wasm3.ElemKindKind) string {
	switch k {
	case wasm3.ElemFuncs:
		return "function index segment"
	case wasm3.ElemFuncExprs:
		return "function expression segment"
	case wasm3.ElemTypedExprs:
		return "typed expression segment"
	default:
		return "unknown segment kind"
	}
}

func (p supportPass) data() error {
	for i, d := range p.m.Data {
		ctx := fmt.Sprintf("data %d", i)
		if d.Mode.Kind != wasm3.DataActive {
			return p.unsupported("data", "passive segment", ctx)
		}
		if d.Mode.Mem != 0 {
			return p.unsupported("data", fmt.Sprintf("memory index %d", d.Mode.Mem), ctx)
		}
		if err := p.constExpr(d.Mode.Offset, ctx+" offset"); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) runtimeFootprint() error {
	_, tableSize, err := SupportedTableRuntimeShape(p.m)
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	maxParams, maxResults := p.maxLocalFuncSlots()
	need, err := wruntime.InstantiateArenaNeed(p.m.GlobalCount(), tableSize, len(p.m.Elements), maxParams, maxResults)
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	if need > wruntime.InstantiateArenaSize {
		return p.unsupported("runtime footprint", fmt.Sprintf("instantiate arena need %d > limit %d", need, wruntime.InstantiateArenaSize), "instantiate arena")
	}
	return nil
}

func (p supportPass) maxLocalFuncSlots() (params, results int) {
	for li := range p.m.FuncTypes {
		ft, ok := p.m.LocalFuncType(li)
		if !ok {
			continue
		}
		if len(ft.Params) > params {
			params = len(ft.Params)
		}
		if len(ft.Results) > results {
			results = len(ft.Results)
		}
	}
	return params, results
}

func (p supportPass) funcs() error {
	for i, fn := range p.m.Code {
		ctx := fmt.Sprintf("function %d", p.m.ImportedFuncCount()+i)
		for j, run := range fn.Locals.Runs {
			if err := p.valType(run.Type, fmt.Sprintf("%s local run %d", ctx, j)); err != nil {
				return err
			}
		}
		if err := p.expr(fn.Body, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) expr(e wasm3.Expr, context string) error {
	for i, in := range e.Instrs {
		ctx := fmt.Sprintf("%s instruction %d", context, i)
		if err := p.instr(in, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) constExpr(e wasm3.Expr, context string) error {
	for i, in := range e.Instrs {
		switch in.Kind {
		case wasm3.InstrI32Const, wasm3.InstrI64Const, wasm3.InstrF32Const, wasm3.InstrF64Const, wasm3.InstrGlobalGet:
		default:
			return p.unsupported("const expression", in.Kind.String(), fmt.Sprintf("%s instruction %d", context, i))
		}
	}
	return nil
}

func (p supportPass) instr(in wasm3.Instruction, context string) error {
	if err := p.blockType(in.BlockType, context); err != nil {
		return err
	}
	if len(in.ValTypes) != 0 {
		if err := p.valTypes(in.ValTypes, context+" select types"); err != nil {
			return err
		}
	}
	if in.MemArg.Mem != nil {
		// The byte-oriented amd64 backend still parses MVP memargs directly from
		// validated BodyBytes. Reject every explicit multi-memory memarg form,
		// including index 0, until that parser understands the extended encoding.
		return p.unsupported("memory", fmt.Sprintf("explicit index %d", *in.MemArg.Mem), context)
	}
	if (in.Kind == wasm3.InstrMemorySize || in.Kind == wasm3.InstrMemoryGrow) && in.Index != 0 {
		return p.unsupported("memory", fmt.Sprintf("index %d", in.Index), context)
	}
	if err := p.instructionKind(in.Kind, context); err != nil {
		return err
	}
	switch in.Kind {
	case wasm3.InstrBlock, wasm3.InstrLoop:
		return p.expr(in.Body, context+" body")
	case wasm3.InstrIf:
		if err := p.expr(wasm3.Expr{Instrs: in.Then}, context+" then"); err != nil {
			return err
		}
		return p.expr(wasm3.Expr{Instrs: in.Else}, context+" else")
	case wasm3.InstrMemoryCopy:
		if in.Index != 0 || in.Index2 != 0 {
			return p.unsupported("memory", fmt.Sprintf("copy indexes %d,%d", in.Index, in.Index2), context)
		}
	case wasm3.InstrMemoryFill:
		if in.Index != 0 {
			return p.unsupported("memory", fmt.Sprintf("fill index %d", in.Index), context)
		}
	case wasm3.InstrCallIndirect:
		if in.Index2 != 0 {
			return p.unsupported("table", fmt.Sprintf("call_indirect table %d", in.Index2), context)
		}
	}
	return nil
}

func (p supportPass) instructionKind(k wasm3.InstrKind, context string) error {
	switch k {
	case wasm3.InstrUnreachable, wasm3.InstrNop,
		wasm3.InstrBlock, wasm3.InstrLoop, wasm3.InstrIf,
		wasm3.InstrBr, wasm3.InstrBrIf, wasm3.InstrBrTable, wasm3.InstrReturn,
		wasm3.InstrCall, wasm3.InstrCallIndirect,
		wasm3.InstrDrop, wasm3.InstrSelect,
		wasm3.InstrLocalGet, wasm3.InstrLocalSet, wasm3.InstrLocalTee,
		wasm3.InstrGlobalGet, wasm3.InstrGlobalSet,
		wasm3.InstrI32Load, wasm3.InstrI64Load, wasm3.InstrF32Load, wasm3.InstrF64Load,
		wasm3.InstrI32Load8S, wasm3.InstrI32Load8U, wasm3.InstrI32Load16S, wasm3.InstrI32Load16U,
		wasm3.InstrI32Store, wasm3.InstrI64Store, wasm3.InstrF32Store, wasm3.InstrF64Store,
		wasm3.InstrI32Store8, wasm3.InstrI32Store16,
		wasm3.InstrI32Const, wasm3.InstrI64Const, wasm3.InstrF32Const, wasm3.InstrF64Const,
		wasm3.InstrI32Eqz, wasm3.InstrI32Eq, wasm3.InstrI32Ne, wasm3.InstrI32LtS, wasm3.InstrI32LtU, wasm3.InstrI32GtS, wasm3.InstrI32GtU, wasm3.InstrI32LeS, wasm3.InstrI32LeU, wasm3.InstrI32GeS, wasm3.InstrI32GeU,
		wasm3.InstrI64Eqz, wasm3.InstrI64Eq, wasm3.InstrI64Ne, wasm3.InstrI64LtS, wasm3.InstrI64LtU, wasm3.InstrI64GtS, wasm3.InstrI64GtU, wasm3.InstrI64LeS, wasm3.InstrI64LeU, wasm3.InstrI64GeS, wasm3.InstrI64GeU,
		wasm3.InstrF32Eq, wasm3.InstrF32Ne, wasm3.InstrF32Lt, wasm3.InstrF32Gt, wasm3.InstrF32Le, wasm3.InstrF32Ge,
		wasm3.InstrF64Eq, wasm3.InstrF64Ne, wasm3.InstrF64Lt, wasm3.InstrF64Gt, wasm3.InstrF64Le, wasm3.InstrF64Ge,
		wasm3.InstrI32Clz, wasm3.InstrI32Ctz, wasm3.InstrI32Popcnt,
		wasm3.InstrI32Add, wasm3.InstrI32Sub, wasm3.InstrI32Mul, wasm3.InstrI32DivS, wasm3.InstrI32DivU, wasm3.InstrI32RemS, wasm3.InstrI32RemU, wasm3.InstrI32And, wasm3.InstrI32Or, wasm3.InstrI32Xor, wasm3.InstrI32Shl, wasm3.InstrI32ShrS, wasm3.InstrI32ShrU, wasm3.InstrI32Rotl, wasm3.InstrI32Rotr,
		wasm3.InstrI64Clz, wasm3.InstrI64Ctz, wasm3.InstrI64Popcnt,
		wasm3.InstrI64Add, wasm3.InstrI64Sub, wasm3.InstrI64Mul, wasm3.InstrI64DivS, wasm3.InstrI64DivU, wasm3.InstrI64RemS, wasm3.InstrI64RemU, wasm3.InstrI64And, wasm3.InstrI64Or, wasm3.InstrI64Xor, wasm3.InstrI64Shl, wasm3.InstrI64ShrS, wasm3.InstrI64ShrU, wasm3.InstrI64Rotl, wasm3.InstrI64Rotr,
		wasm3.InstrF32Abs, wasm3.InstrF32Neg, wasm3.InstrF32Sqrt, wasm3.InstrF32Add, wasm3.InstrF32Sub, wasm3.InstrF32Mul, wasm3.InstrF32Div, wasm3.InstrF32Min, wasm3.InstrF32Max,
		wasm3.InstrF64Abs, wasm3.InstrF64Neg, wasm3.InstrF64Sqrt, wasm3.InstrF64Add, wasm3.InstrF64Sub, wasm3.InstrF64Mul, wasm3.InstrF64Div, wasm3.InstrF64Min, wasm3.InstrF64Max,
		wasm3.InstrI32WrapI64, wasm3.InstrI32TruncF32S, wasm3.InstrI32TruncF32U, wasm3.InstrI32TruncF64S, wasm3.InstrI32TruncF64U,
		wasm3.InstrI64ExtendI32S, wasm3.InstrI64ExtendI32U, wasm3.InstrI64TruncF32S, wasm3.InstrI64TruncF32U, wasm3.InstrI64TruncF64S, wasm3.InstrI64TruncF64U,
		wasm3.InstrF32ConvertI32S, wasm3.InstrF32ConvertI32U, wasm3.InstrF32ConvertI64S, wasm3.InstrF32ConvertI64U, wasm3.InstrF32DemoteF64,
		wasm3.InstrF64ConvertI32S, wasm3.InstrF64ConvertI32U, wasm3.InstrF64ConvertI64S, wasm3.InstrF64ConvertI64U, wasm3.InstrF64PromoteF32,
		wasm3.InstrI32ReinterpretF32, wasm3.InstrI64ReinterpretF64, wasm3.InstrF32ReinterpretI32, wasm3.InstrF64ReinterpretI64,
		wasm3.InstrMemoryCopy, wasm3.InstrMemoryFill:
		return nil
	}
	if isGCInstruction(k) {
		return p.unsupported("gc instruction", k.String(), context)
	}
	if isReferenceInstruction(k) {
		return p.unsupported("reference instruction", k.String(), context)
	}
	return p.unsupported("instruction", k.String(), context)
}

func isReferenceInstruction(k wasm3.InstrKind) bool {
	switch k {
	case wasm3.InstrRefNull, wasm3.InstrRefIsNull, wasm3.InstrRefFunc, wasm3.InstrRefEq, wasm3.InstrRefAsNonNull,
		wasm3.InstrBrOnNull, wasm3.InstrBrOnNonNull, wasm3.InstrRefTest, wasm3.InstrRefCast,
		wasm3.InstrBrOnCast, wasm3.InstrBrOnCastFail, wasm3.InstrAnyConvertExtern,
		wasm3.InstrExternConvertAny, wasm3.InstrRefI31, wasm3.InstrI31GetS, wasm3.InstrI31GetU,
		wasm3.InstrCallRef, wasm3.InstrReturnCallRef:
		return true
	default:
		return false
	}
}

func isGCInstruction(k wasm3.InstrKind) bool {
	switch k {
	case wasm3.InstrStructNew, wasm3.InstrStructNewDefault, wasm3.InstrStructNewDesc,
		wasm3.InstrStructNewDefaultDesc, wasm3.InstrStructGet, wasm3.InstrStructGetS,
		wasm3.InstrStructGetU, wasm3.InstrStructAtomicGet, wasm3.InstrStructAtomicGetS,
		wasm3.InstrStructAtomicGetU, wasm3.InstrStructSet, wasm3.InstrArrayNew,
		wasm3.InstrArrayNewDefault, wasm3.InstrArrayNewFixed, wasm3.InstrArrayNewData,
		wasm3.InstrArrayNewElem, wasm3.InstrArrayGet, wasm3.InstrArrayGetS,
		wasm3.InstrArrayGetU, wasm3.InstrArraySet, wasm3.InstrArrayLen,
		wasm3.InstrArrayFill, wasm3.InstrArrayCopy, wasm3.InstrArrayInitData,
		wasm3.InstrArrayInitElem, wasm3.InstrRefGetDesc, wasm3.InstrRefTestDesc,
		wasm3.InstrRefCastDescEq:
		return true
	default:
		return false
	}
}

func (p supportPass) blockType(bt wasm3.BlockType, context string) error {
	switch bt.Kind {
	case wasm3.BlockVoid:
		return nil
	case wasm3.BlockVal:
		return p.valType(bt.Val, context+" block type")
	case wasm3.BlockTypeIndex:
		return nil
	default:
		return p.unsupported("block", "unknown type", context)
	}
}

func (p supportPass) valTypes(vs []wasm3.ValType, context string) error {
	for i, vt := range vs {
		if err := p.valType(vt, fmt.Sprintf("%s[%d]", context, i)); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) valType(v wasm3.ValType, context string) error {
	if v.Kind == wasm3.ValNum {
		switch v.Num {
		case wasm3.NumI32, wasm3.NumI64, wasm3.NumF32, wasm3.NumF64:
			return nil
		}
	}
	if v.Kind == wasm3.ValRef {
		return p.unsupported("reference type", valTypeName(v), context)
	}
	return p.unsupported("value type", valTypeName(v), context)
}

func (p supportPass) globalType(v wasm3.ValType, context string) error {
	if err := p.valType(v, context); err == nil {
		return nil
	}
	return p.unsupported("global type", valTypeName(v), context)
}

func valTypeName(v wasm3.ValType) string {
	if v.Kind == wasm3.ValRef {
		return refTypeName(v.Ref)
	}
	return v.String()
}

func refTypeName(rt wasm3.RefType) string {
	if isFuncRef(rt) {
		return "funcref"
	}
	if rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm3.HeapAbs && rt.Heap.Abs == wasm3.HeapExtern {
		return "externref"
	}
	return wasm3.RefVal(rt).String()
}

func isNum(v wasm3.ValType, n wasm3.NumType) bool { return v.Kind == wasm3.ValNum && v.Num == n }

func maxInt() int { return int(^uint(0) >> 1) }

func (p supportPass) funcType(idx wasm3.TypeIdx) *wasm3.CompType {
	if idx.Rec || int(idx.Index) >= len(p.m.Types) || len(p.m.Types[idx.Index].SubTypes) != 1 {
		return nil
	}
	ct := &p.m.Types[idx.Index].SubTypes[0].Comp
	if ct.Kind != wasm3.CompFunc {
		return nil
	}
	return ct
}

func isFuncRef(rt wasm3.RefType) bool {
	return rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm3.HeapAbs && rt.Heap.Abs == wasm3.HeapFunc
}
