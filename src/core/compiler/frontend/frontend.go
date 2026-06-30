// Package frontend contains compiler front-end passes shared by the CLI and API.
package frontend

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
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
func DecodeValidate(data []byte) (*wasm.Module, error) {
	m, err := wasm.DecodeModule(data)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
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
func RejectUnsupported(m *wasm.Module) error {
	p := supportPass{m: m}
	return p.run()
}

type supportPass struct{ m *wasm.Module }

// SupportedTableRuntimeShape returns the runtime ABI shape for the one local
// table wago currently supports. A declared zero-length table still has runtime
// presence: call_indirect needs a descriptor whose length is zero so it can trap
// before reading an entry.
func SupportedTableRuntimeShape(m *wasm.Module) (hasTable bool, tableSize int, err error) {
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
		if st.Comp.Kind != wasm.CompFunc {
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

func compTypeName(k wasm.CompTypeKind) string {
	switch k {
	case wasm.CompArray:
		return "array type"
	case wasm.CompStruct:
		return "struct type"
	case wasm.CompFunc:
		return "function type"
	default:
		return "unknown type"
	}
}

func (p supportPass) imports() error {
	for i, im := range p.m.Imports {
		ctx := fmt.Sprintf("import %d %q.%q", i, im.Module, im.Name)
		switch im.Type.Kind {
		case wasm.ExternFunc:
			ft := p.funcType(im.Type.Type)
			if ft == nil {
				return p.unsupported("import", "function with unknown type", ctx)
			}
			if len(ft.Results) != 0 {
				return p.unsupported("import", "function result", ctx)
			}
			if len(ft.Params) > 1 || (len(ft.Params) == 1 && !isNum(ft.Params[0], wasm.NumI32)) {
				return p.unsupported("import", "function signature", ctx)
			}
		case wasm.ExternGlobal:
			if err := p.globalType(im.Type.Global.Type, ctx); err != nil {
				return err
			}
		case wasm.ExternTable:
			return p.unsupported("import", "table", ctx)
		case wasm.ExternMem:
			return p.unsupported("import", "memory", ctx)
		case wasm.ExternTag:
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
		case wasm.ExternFunc, wasm.ExternGlobal:
			// Supported metadata is serialized for function and numeric-global exports.
		case wasm.ExternTable:
			return p.unsupported("export", "table", ctx)
		case wasm.ExternMem:
			// Memory exports are metadata-only for wago today; the instance exposes
			// linear memory directly, and preserving this keeps current MVP modules
			// that export memory runnable.
		case wasm.ExternTag:
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
		if e.Mode.Kind != wasm.ElemActive {
			return p.unsupported("element", elemModeName(e.Mode.Kind), ctx)
		}
		if e.Mode.Table != 0 {
			return p.unsupported("element", fmt.Sprintf("table index %d", e.Mode.Table), ctx)
		}
		if err := p.constExpr(e.Mode.Offset, ctx+" offset"); err != nil {
			return err
		}
		if e.Kind.Kind != wasm.ElemFuncs {
			return p.unsupported("reference type", elemKindName(e.Kind.Kind), ctx)
		}
	}
	return nil
}

func elemModeName(k wasm.ElemModeKind) string {
	switch k {
	case wasm.ElemPassive:
		return "passive segment"
	case wasm.ElemActive:
		return "active segment"
	case wasm.ElemDeclarative:
		return "declarative segment"
	default:
		return "unknown segment mode"
	}
}

func elemKindName(k wasm.ElemKindKind) string {
	switch k {
	case wasm.ElemFuncs:
		return "function index segment"
	case wasm.ElemFuncExprs:
		return "function expression segment"
	case wasm.ElemTypedExprs:
		return "typed expression segment"
	default:
		return "unknown segment kind"
	}
}

func (p supportPass) data() error {
	for i, d := range p.m.Data {
		ctx := fmt.Sprintf("data %d", i)
		if d.Mode.Kind != wasm.DataActive {
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
	hasTable, tableSize, err := SupportedTableRuntimeShape(p.m)
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	maxParams, maxResults := p.maxLocalFuncSlots()
	need, err := runtime.InstantiateArenaNeed(runtime.InstantiateFootprint{
		GlobalCount:    p.m.GlobalCount(),
		HasTable:       hasTable,
		TableSize:      tableSize,
		ElemCount:      len(p.m.Elements),
		MaxParamSlots:  maxParams,
		MaxResultSlots: maxResults,
	})
	if err != nil {
		return p.unsupported("runtime footprint", err.Error(), "instantiate arena")
	}
	if need > runtime.InstantiateArenaSize {
		return p.unsupported("runtime footprint", fmt.Sprintf("instantiate arena need %d > limit %d", need, runtime.InstantiateArenaSize), "instantiate arena")
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

func (p supportPass) expr(e wasm.Expr, context string) error {
	for i, in := range e.Instrs {
		ctx := fmt.Sprintf("%s instruction %d", context, i)
		if err := p.instr(in, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) constExpr(e wasm.Expr, context string) error {
	for i, in := range e.Instrs {
		switch in.Kind {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const, wasm.InstrGlobalGet:
		default:
			return p.unsupported("const expression", in.Kind.String(), fmt.Sprintf("%s instruction %d", context, i))
		}
	}
	return nil
}

func (p supportPass) instr(in wasm.Instruction, context string) error {
	if err := p.blockType(in.BlockType(), context); err != nil {
		return err
	}
	if len(in.ValTypes()) != 0 {
		if err := p.valTypes(in.ValTypes(), context+" select types"); err != nil {
			return err
		}
	}
	if in.MemArg().Mem != nil {
		// The byte-oriented amd64 backend still parses MVP memargs directly from
		// validated BodyBytes. Reject every explicit multi-memory memarg form,
		// including index 0, until that parser understands the extended encoding.
		return p.unsupported("memory", fmt.Sprintf("explicit index %d", *in.MemArg().Mem), context)
	}
	if (in.Kind == wasm.InstrMemorySize || in.Kind == wasm.InstrMemoryGrow) && in.Index != 0 {
		return p.unsupported("memory", fmt.Sprintf("index %d", in.Index), context)
	}
	if err := p.instructionKind(in.Kind, context); err != nil {
		return err
	}
	switch in.Kind {
	case wasm.InstrBlock, wasm.InstrLoop:
		return p.expr(in.Body(), context+" body")
	case wasm.InstrIf:
		if err := p.expr(wasm.Expr{Instrs: in.Then()}, context+" then"); err != nil {
			return err
		}
		return p.expr(wasm.Expr{Instrs: in.Else()}, context+" else")
	case wasm.InstrMemoryCopy:
		if in.Index != 0 || in.Index2 != 0 {
			return p.unsupported("memory", fmt.Sprintf("copy indexes %d,%d", in.Index, in.Index2), context)
		}
	case wasm.InstrMemoryFill:
		if in.Index != 0 {
			return p.unsupported("memory", fmt.Sprintf("fill index %d", in.Index), context)
		}
	case wasm.InstrCallIndirect:
		if in.Index2 != 0 {
			return p.unsupported("table", fmt.Sprintf("call_indirect table %d", in.Index2), context)
		}
	}
	return nil
}

func (p supportPass) instructionKind(k wasm.InstrKind, context string) error {
	switch k {
	case wasm.InstrUnreachable, wasm.InstrNop,
		wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
		wasm.InstrCall, wasm.InstrCallIndirect,
		wasm.InstrDrop, wasm.InstrSelect,
		wasm.InstrLocalGet, wasm.InstrLocalSet, wasm.InstrLocalTee,
		wasm.InstrGlobalGet, wasm.InstrGlobalSet,
		wasm.InstrI32Load, wasm.InstrI64Load, wasm.InstrF32Load, wasm.InstrF64Load,
		wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI32Load16S, wasm.InstrI32Load16U,
		wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI64Load16S, wasm.InstrI64Load16U, wasm.InstrI64Load32S, wasm.InstrI64Load32U,
		wasm.InstrI32Store, wasm.InstrI64Store, wasm.InstrF32Store, wasm.InstrF64Store,
		wasm.InstrI32Store8, wasm.InstrI32Store16, wasm.InstrI64Store8, wasm.InstrI64Store16, wasm.InstrI64Store32,
		wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const,
		wasm.InstrI32Eqz, wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU, wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU, wasm.InstrI32GeS, wasm.InstrI32GeU,
		wasm.InstrI64Eqz, wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU, wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU, wasm.InstrI64GeS, wasm.InstrI64GeU,
		wasm.InstrF32Eq, wasm.InstrF32Ne, wasm.InstrF32Lt, wasm.InstrF32Gt, wasm.InstrF32Le, wasm.InstrF32Ge,
		wasm.InstrF64Eq, wasm.InstrF64Ne, wasm.InstrF64Lt, wasm.InstrF64Gt, wasm.InstrF64Le, wasm.InstrF64Ge,
		wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
		wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul, wasm.InstrI32DivS, wasm.InstrI32DivU, wasm.InstrI32RemS, wasm.InstrI32RemU, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor, wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
		wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul, wasm.InstrI64DivS, wasm.InstrI64DivU, wasm.InstrI64RemS, wasm.InstrI64RemU, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor, wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr,
		wasm.InstrF32Abs, wasm.InstrF32Neg, wasm.InstrF32Sqrt, wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div, wasm.InstrF32Min, wasm.InstrF32Max,
		wasm.InstrF64Abs, wasm.InstrF64Neg, wasm.InstrF64Sqrt, wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div, wasm.InstrF64Min, wasm.InstrF64Max,
		wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S,
		wasm.InstrI32WrapI64, wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U, wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF32DemoteF64,
		wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32,
		wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64, wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64,
		wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
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

func isReferenceInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrRefNull, wasm.InstrRefIsNull, wasm.InstrRefFunc, wasm.InstrRefEq, wasm.InstrRefAsNonNull,
		wasm.InstrBrOnNull, wasm.InstrBrOnNonNull, wasm.InstrRefTest, wasm.InstrRefCast,
		wasm.InstrBrOnCast, wasm.InstrBrOnCastFail, wasm.InstrAnyConvertExtern,
		wasm.InstrExternConvertAny, wasm.InstrRefI31, wasm.InstrI31GetS, wasm.InstrI31GetU,
		wasm.InstrCallRef, wasm.InstrReturnCallRef:
		return true
	default:
		return false
	}
}

func isGCInstruction(k wasm.InstrKind) bool {
	switch k {
	case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructNewDesc,
		wasm.InstrStructNewDefaultDesc, wasm.InstrStructGet, wasm.InstrStructGetS,
		wasm.InstrStructGetU, wasm.InstrStructAtomicGet, wasm.InstrStructAtomicGetS,
		wasm.InstrStructAtomicGetU, wasm.InstrStructSet, wasm.InstrArrayNew,
		wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed, wasm.InstrArrayNewData,
		wasm.InstrArrayNewElem, wasm.InstrArrayGet, wasm.InstrArrayGetS,
		wasm.InstrArrayGetU, wasm.InstrArraySet, wasm.InstrArrayLen,
		wasm.InstrArrayFill, wasm.InstrArrayCopy, wasm.InstrArrayInitData,
		wasm.InstrArrayInitElem, wasm.InstrRefGetDesc, wasm.InstrRefTestDesc,
		wasm.InstrRefCastDescEq:
		return true
	default:
		return false
	}
}

func (p supportPass) blockType(bt wasm.BlockType, context string) error {
	switch bt.Kind {
	case wasm.BlockVoid:
		return nil
	case wasm.BlockVal:
		return p.valType(bt.Val, context+" block type")
	case wasm.BlockTypeIndex:
		return nil
	default:
		return p.unsupported("block", "unknown type", context)
	}
}

func (p supportPass) valTypes(vs []wasm.ValType, context string) error {
	for i, vt := range vs {
		if err := p.valType(vt, fmt.Sprintf("%s[%d]", context, i)); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) valType(v wasm.ValType, context string) error {
	if v.Kind == wasm.ValNum {
		switch v.Num {
		case wasm.NumI32, wasm.NumI64, wasm.NumF32, wasm.NumF64:
			return nil
		}
	}
	if v.Kind == wasm.ValRef {
		return p.unsupported("reference type", valTypeName(v), context)
	}
	return p.unsupported("value type", valTypeName(v), context)
}

func (p supportPass) globalType(v wasm.ValType, context string) error {
	if err := p.valType(v, context); err == nil {
		return nil
	}
	return p.unsupported("global type", valTypeName(v), context)
}

func valTypeName(v wasm.ValType) string {
	if v.Kind == wasm.ValRef {
		return refTypeName(v.Ref)
	}
	return v.String()
}

func refTypeName(rt wasm.RefType) string {
	if isFuncRef(rt) {
		return "funcref"
	}
	if rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs && rt.Heap.Abs == wasm.HeapExtern {
		return "externref"
	}
	return wasm.RefVal(rt).String()
}

func isNum(v wasm.ValType, n wasm.NumType) bool { return v.Kind == wasm.ValNum && v.Num == n }

func maxInt() int { return int(^uint(0) >> 1) }

func (p supportPass) funcType(idx wasm.TypeIdx) *wasm.CompType {
	if idx.Rec || int(idx.Index) >= len(p.m.Types) || len(p.m.Types[idx.Index].SubTypes) != 1 {
		return nil
	}
	ct := &p.m.Types[idx.Index].SubTypes[0].Comp
	if ct.Kind != wasm.CompFunc {
		return nil
	}
	return ct
}

func isFuncRef(rt wasm.RefType) bool {
	return rt.Nullable && rt.Bare && !rt.Exact && rt.Heap.Kind == wasm.HeapAbs && rt.Heap.Abs == wasm.HeapFunc
}
