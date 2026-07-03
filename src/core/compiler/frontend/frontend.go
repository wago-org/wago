// Package frontend contains compiler front-end passes shared by the CLI and API.
package frontend

import (
	"fmt"
	"strconv"

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

// DecodeValidate decodes without materializing function-body instruction trees,
// validates, and runs wago's support pass over data.
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

// Features toggles the optional WebAssembly proposals the support pass accepts.
// It is a plain frontend-side set (no dependency on the public wago config) that
// callers map their feature configuration onto.
type Features struct {
	SignExtension   bool // i32/i64.extend{8,16,32}_s
	BulkMemory      bool // memory.copy / memory.fill
	SaturatingTrunc bool // i32/i64.trunc_sat_f32/f64_s/u (non-trapping float→int)
}

// AllFeatures is the full optional set wago's backend lowers today; it is the
// default applied by RejectUnsupported.
func AllFeatures() Features {
	return Features{SignExtension: true, BulkMemory: true, SaturatingTrunc: true}
}

// RejectUnsupported rejects modules that require features not explicitly wired
// through the current JIT/runtime, accepting wago's full optional feature set.
func RejectUnsupported(m *wasm.Module) error {
	return RejectUnsupportedWithFeatures(m, AllFeatures())
}

// RejectUnsupportedWithFeatures is RejectUnsupported with optional proposals
// gated by f: a disabled proposal makes its instructions unsupported. The pass
// is deliberately conservative: a construct must be listed here before it can
// reach code generation.
func RejectUnsupportedWithFeatures(m *wasm.Module, f Features) error {
	p := supportPass{m: m, feat: f}
	return p.run()
}

type supportPass struct {
	m    *wasm.Module
	feat Features
}

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
	importedFuncs := p.m.ImportedFuncCount()
	if p.m.Start != nil && int(*p.m.Start) < importedFuncs {
		return p.unsupported("start", "imported function", fmt.Sprintf("start function %d", *p.m.Start))
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
			// Host imports use a log-and-replay model that captures only the
			// first i32 argument and returns no result. Permit any number of
			// trailing arguments — e.g. AssemblyScript's
			// env.abort(msg, file, line, col), all i32 — as long as every
			// argument is a numeric scalar (one operand-stack slot each) and the
			// captured first argument is i32.
			if len(ft.Params) > 0 && !isNum(ft.Params[0], wasm.NumI32) {
				return p.unsupported("import", "function signature", ctx)
			}
			for _, pt := range ft.Params {
				if pt.Kind != wasm.ValNum {
					return p.unsupported("import", "function signature", ctx)
				}
			}
		case wasm.ExternGlobal:
			if err := p.globalType(im.Type.Global.Type, ctx); err != nil {
				return err
			}
		case wasm.ExternTable:
			return p.unsupported("import", "table", ctx)
		case wasm.ExternMem:
			if err := p.checkMemType(im.Type.Mem, ctx); err != nil {
				return err
			}
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
		if err := p.checkMemType(mem, fmt.Sprintf("memory %d", i)); err != nil {
			return err
		}
	}
	return nil
}

// checkMemType rejects memory shapes outside wago's non-shared, 32-bit model
// (used for both defined and imported memories). Multi-page memories are
// supported up to the 65535-page cap (4 GiB minus one page).
func (p supportPass) checkMemType(mem wasm.MemType, ctx string) error {
	if mem.Shared {
		return p.unsupported("memory", "shared", ctx)
	}
	if mem.Limits.Addr64 {
		return p.unsupported("memory", "memory64", ctx)
	}
	if mem.Limits.Min > 65535 {
		return p.unsupported("memory", fmt.Sprintf("minimum %d pages exceeds 65535", mem.Limits.Min), ctx)
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
			// Table exports are metadata-only for wago today: the instance keeps
			// its table internally for call_indirect. Accepting them keeps MVP
			// modules that export a table runnable (there is no host table object).
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
		FuncImportCount: p.m.ImportedFuncCount(),
		GlobalCount:     p.m.GlobalCount(),
		HasTable:        hasTable,
		TableSize:       tableSize,
		ElemCount:       len(p.m.Elements),
		MaxParamSlots:   maxParams,
		MaxResultSlots:  maxResults,
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
	importedFuncs := p.m.ImportedFuncCount()
	for i, fn := range p.m.Code {
		ctx := fmt.Sprintf("function %d", importedFuncs+i)
		for j, run := range fn.Locals.Runs {
			if err := p.valType(run.Type, fmt.Sprintf("%s local run %d", ctx, j)); err != nil {
				return err
			}
		}
		body := wasm.Expr{BodyBytes: fn.BodyBytes}
		if len(fn.BodyBytes) == 0 {
			body = fn.Body
		}
		if err := p.expr(body, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p supportPass) expr(e wasm.Expr, context string) error {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return p.exprBytes(e.BodyBytes, context)
	}
	for i, in := range e.Instrs {
		if err := p.instr(in, instructionContext(context, i)); err != nil {
			return err
		}
	}
	return nil
}

func instructionContext(context string, instr int) string {
	return context + " instruction " + strconv.Itoa(instr)
}

func (p supportPass) exprBytes(body []byte, context string) error {
	r := wasm.NewReader(body)
	for instr := 0; r.HasNext(); instr++ {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		sawEnd, err := p.instrByte(r, op, context, instr)
		if err != nil {
			return err
		}
		if sawEnd && !r.HasNext() {
			return nil
		}
	}
	return nil
}

func (p supportPass) instrByte(r *wasm.Reader, op byte, context string, instr int) (bool, error) {
	ctx := func() string { return instructionContext(context, instr) }
	skipBlockType := func() error {
		b, err := r.Byte()
		if err != nil {
			return err
		}
		if b == 0x40 || b == 0x7f || b == 0x7e || b == 0x7d || b == 0x7c {
			return nil
		}
		if isRefTypeLeadByte(b) || b == 0x7b {
			return p.unsupported("value type", fmt.Sprintf("0x%02x", b), ctx())
		}
		// Multi-value block type: the first byte was part of a signed LEB. The
		// validator has already checked that it resolves to a valid type.
		for b&0x80 != 0 {
			var err error
			b, err = r.Byte()
			if err != nil {
				return err
			}
		}
		return nil
	}
	skipValType := func() error {
		b, err := r.Byte()
		if err != nil {
			return err
		}
		if b == 0x7f || b == 0x7e || b == 0x7d || b == 0x7c {
			return nil
		}
		return p.unsupported("value type", fmt.Sprintf("0x%02x", b), ctx())
	}
	switch op {
	case 0x00, 0x01, 0x05, 0x0f, 0x1a, 0x1b,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65,
		0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70,
		0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b,
		0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86,
		0x87, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f, 0x90, 0x91,
		0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0x9b, 0x9c,
		0x9d, 0x9e, 0x9f, 0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
		0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf, 0xb0, 0xb1, 0xb2,
		0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd,
		0xbe, 0xbf:
		return false, nil
	case 0x0b:
		return true, nil
	case 0x02, 0x03, 0x04:
		return false, skipBlockType()
	case 0x0c, 0x0d, 0x10, 0x20, 0x21, 0x22, 0x23, 0x24, 0x0e:
		_, err := wasm.ClassifyInstructionImmediate(r, op)
		return false, err
	case 0x11:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if imm.Index2 != 0 {
			return false, p.unsupported("table", fmt.Sprintf("call_indirect table %d", imm.Index2), ctx())
		}
		return false, nil
	case 0x1c:
		n, err := r.U32()
		if err != nil {
			return false, err
		}
		for i := uint32(0); i < n; i++ {
			if err := skipValType(); err != nil {
				return false, err
			}
		}
		return false, nil
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32,
		0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d,
		0x3e:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if imm.HasMemIndex {
			return false, p.unsupported("memory", fmt.Sprintf("explicit index %d", imm.MemIndex), ctx())
		}
		return false, nil
	case 0x3f, 0x40:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if imm.Index != 0 {
			return false, p.unsupported("memory", fmt.Sprintf("index %d", imm.Index), ctx())
		}
		return false, nil
	case 0x41, 0x42, 0x43, 0x44:
		_, err := wasm.ClassifyInstructionImmediate(r, op)
		return false, err
	case 0xc0, 0xc1, 0xc2, 0xc3, 0xc4:
		if !p.feat.SignExtension {
			return false, p.unsupported("instruction", "sign-extension-ops disabled", ctx())
		}
		return false, nil
	case 0xd0:
		if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
			return false, err
		}
		return false, p.unsupported("reference instruction", "RefNull", ctx())
	case 0xfd:
		imm, err := wasm.ClassifyInstructionImmediate(r, op)
		if err != nil {
			return false, err
		}
		if !supportedSIMDInstruction(imm) {
			return false, p.unsupported("instruction", simdUnsupportedName(imm), ctx())
		}
		return false, nil
	case 0xfb, 0xfe:
		if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
			return false, err
		}
		return false, p.unsupported("instruction", fmt.Sprintf("opcode 0x%02x", op), ctx())
	case 0xfc:
		return false, p.fcInstrByte(r, ctx)
	default:
		return false, p.unsupported("instruction", fmt.Sprintf("opcode 0x%02x", op), ctx())
	}
}

func simdUnsupportedName(imm wasm.InstructionImmediate) string {
	if imm.Kind == wasm.InstrInvalid {
		return fmt.Sprintf("0xFD opcode %d", imm.Subopcode)
	}
	return imm.Kind.String()
}

func supportedSIMDInstruction(imm wasm.InstructionImmediate) bool {
	if imm.Subopcode == 12 { // classifyFDBytes skips the 16-byte literal without allocating a V128Const instruction.
		return true
	}
	switch imm.Kind {
	case wasm.InstrV128Load, wasm.InstrV128Store,
		wasm.InstrI8x16Splat, wasm.InstrI16x8Splat, wasm.InstrI32x4Splat, wasm.InstrI64x2Splat,
		wasm.InstrF32x4Splat, wasm.InstrF64x2Splat,
		wasm.InstrI8x16ExtractLaneS, wasm.InstrI8x16ExtractLaneU, wasm.InstrI8x16ReplaceLane,
		wasm.InstrI16x8ExtractLaneS, wasm.InstrI16x8ExtractLaneU, wasm.InstrI16x8ReplaceLane,
		wasm.InstrI32x4ExtractLane, wasm.InstrI32x4ReplaceLane,
		wasm.InstrI64x2ExtractLane, wasm.InstrI64x2ReplaceLane,
		wasm.InstrF32x4ExtractLane, wasm.InstrF32x4ReplaceLane,
		wasm.InstrF64x2ExtractLane, wasm.InstrF64x2ReplaceLane,
		wasm.InstrI8x16Eq, wasm.InstrI8x16Ne, wasm.InstrI8x16LtS, wasm.InstrI8x16LtU, wasm.InstrI8x16GtS, wasm.InstrI8x16GtU, wasm.InstrI8x16LeS, wasm.InstrI8x16LeU, wasm.InstrI8x16GeS, wasm.InstrI8x16GeU, wasm.InstrI8x16Add, wasm.InstrI8x16Sub, wasm.InstrI8x16MinS, wasm.InstrI8x16MinU, wasm.InstrI8x16MaxS, wasm.InstrI8x16MaxU,
		wasm.InstrI16x8Eq, wasm.InstrI16x8Ne, wasm.InstrI16x8LtS, wasm.InstrI16x8LtU, wasm.InstrI16x8GtS, wasm.InstrI16x8GtU, wasm.InstrI16x8LeS, wasm.InstrI16x8LeU, wasm.InstrI16x8GeS, wasm.InstrI16x8GeU, wasm.InstrI16x8Add, wasm.InstrI16x8Sub, wasm.InstrI16x8Mul, wasm.InstrI16x8MinS, wasm.InstrI16x8MinU, wasm.InstrI16x8MaxS, wasm.InstrI16x8MaxU,
		wasm.InstrI8x16Abs, wasm.InstrI8x16Neg, wasm.InstrI8x16Popcnt,
		wasm.InstrI16x8Abs, wasm.InstrI16x8Neg,
		wasm.InstrI32x4Abs, wasm.InstrI32x4Neg,
		wasm.InstrI32x4Eq, wasm.InstrI32x4Ne, wasm.InstrI32x4LtS, wasm.InstrI32x4LtU, wasm.InstrI32x4GtS, wasm.InstrI32x4GtU, wasm.InstrI32x4LeS, wasm.InstrI32x4LeU, wasm.InstrI32x4GeS, wasm.InstrI32x4GeU, wasm.InstrI32x4Add, wasm.InstrI32x4Sub, wasm.InstrI32x4Mul, wasm.InstrI32x4MinS, wasm.InstrI32x4MinU, wasm.InstrI32x4MaxS, wasm.InstrI32x4MaxU,
		wasm.InstrI64x2Neg,
		wasm.InstrI64x2Eq, wasm.InstrI64x2Ne, wasm.InstrI64x2Add, wasm.InstrI64x2Sub,
		wasm.InstrF32x4Eq, wasm.InstrF32x4Ne, wasm.InstrF32x4Lt, wasm.InstrF32x4Gt, wasm.InstrF32x4Le, wasm.InstrF32x4Ge,
		wasm.InstrF64x2Eq, wasm.InstrF64x2Ne, wasm.InstrF64x2Lt, wasm.InstrF64x2Gt, wasm.InstrF64x2Le, wasm.InstrF64x2Ge,
		wasm.InstrF32x4Add, wasm.InstrF32x4Sub, wasm.InstrF32x4Mul, wasm.InstrF32x4Div,
		wasm.InstrF64x2Add, wasm.InstrF64x2Sub, wasm.InstrF64x2Mul, wasm.InstrF64x2Div,
		wasm.InstrV128Not, wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Bitselect,
		wasm.InstrV128AnyTrue,
		wasm.InstrI8x16AllTrue, wasm.InstrI8x16Bitmask,
		wasm.InstrI16x8AllTrue, wasm.InstrI16x8Bitmask,
		wasm.InstrI32x4AllTrue, wasm.InstrI32x4Bitmask,
		wasm.InstrI64x2AllTrue, wasm.InstrI64x2Bitmask:
		return true
	}
	return false
}

func (p supportPass) fcInstrByte(r *wasm.Reader, context func() string) error {
	imm, err := wasm.ClassifyInstructionImmediate(r, 0xfc)
	if err != nil {
		return err
	}
	switch imm.Subopcode {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		if !p.feat.SaturatingTrunc {
			return p.unsupported("instruction", "nontrapping-float-to-int-conversion disabled", context())
		}
		return nil
	case 10:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "memory.copy (bulk-memory-operations disabled)", context())
		}
		if imm.Index != 0 || imm.Index2 != 0 {
			return p.unsupported("memory", fmt.Sprintf("copy indexes %d,%d", imm.Index, imm.Index2), context())
		}
		return nil
	case 11:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", "memory.fill (bulk-memory-operations disabled)", context())
		}
		if imm.Index != 0 {
			return p.unsupported("memory", fmt.Sprintf("fill index %d", imm.Index), context())
		}
		return nil
	default:
		return p.unsupported("instruction", fmt.Sprintf("0xfc %d", imm.Subopcode), context())
	}
}

func isRefTypeLeadByte(b byte) bool {
	return b == 0x63 || b == 0x64 || b == 0x6f || b == 0x70 || b == 0x6e || b == 0x6d || b == 0x6c || b == 0x6b || b == 0x6a || b == 0x69 || b == 0x71 || b == 0x72 || b == 0x73 || b == 0x74
}

func (p supportPass) constExpr(e wasm.Expr, context string) error {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return p.constExprBytes(e.BodyBytes, context)
	}
	for i, in := range e.Instrs {
		switch in.Kind {
		case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const, wasm.InstrGlobalGet:
		default:
			return p.unsupported("const expression", in.Kind.String(), instructionContext(context, i))
		}
	}
	return nil
}

func (p supportPass) constExprBytes(body []byte, context string) error {
	r := wasm.NewReader(body)
	op, err := r.Byte()
	if err != nil {
		return err
	}
	switch op {
	case 0x23:
		if _, err := r.U32(); err != nil {
			return err
		}
	case 0x41:
		if _, err := r.I32(); err != nil {
			return err
		}
	case 0x42:
		if _, err := r.I64(); err != nil {
			return err
		}
	case 0x43:
		if _, err := r.Bytes(4); err != nil {
			return err
		}
	case 0x44:
		if _, err := r.Bytes(8); err != nil {
			return err
		}
	default:
		feature := fmt.Sprintf("opcode 0x%02x", op)
		return p.unsupported("const expression", feature, instructionContext(context, 0))
	}
	end, err := r.Byte()
	if err != nil {
		return err
	}
	if end != 0x0b || r.BytesLeft() != 0 {
		return p.unsupported("const expression", "multi-instruction", context)
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
	// Proposals gated by the configured feature set.
	switch k {
	case wasm.InstrI32Extend8S, wasm.InstrI32Extend16S, wasm.InstrI64Extend8S, wasm.InstrI64Extend16S, wasm.InstrI64Extend32S:
		if !p.feat.SignExtension {
			return p.unsupported("instruction", k.String()+" (sign-extension-ops disabled)", context)
		}
		return nil
	case wasm.InstrMemoryCopy, wasm.InstrMemoryFill:
		if !p.feat.BulkMemory {
			return p.unsupported("instruction", k.String()+" (bulk-memory-operations disabled)", context)
		}
		return nil
	case wasm.InstrI32TruncSatF32S, wasm.InstrI32TruncSatF32U, wasm.InstrI32TruncSatF64S, wasm.InstrI32TruncSatF64U,
		wasm.InstrI64TruncSatF32S, wasm.InstrI64TruncSatF32U, wasm.InstrI64TruncSatF64S, wasm.InstrI64TruncSatF64U:
		if !p.feat.SaturatingTrunc {
			return p.unsupported("instruction", k.String()+" (nontrapping-float-to-int-conversion disabled)", context)
		}
		return nil
	}
	switch k {
	case wasm.InstrUnreachable, wasm.InstrNop,
		wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf,
		wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn,
		wasm.InstrCall, wasm.InstrCallIndirect,
		wasm.InstrDrop, wasm.InstrSelect,
		wasm.InstrMemorySize, wasm.InstrMemoryGrow,
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
		wasm.InstrF32Ceil, wasm.InstrF32Floor, wasm.InstrF32Trunc, wasm.InstrF32Nearest, wasm.InstrF32Copysign,
		wasm.InstrF64Abs, wasm.InstrF64Neg, wasm.InstrF64Sqrt, wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div, wasm.InstrF64Min, wasm.InstrF64Max,
		wasm.InstrF64Ceil, wasm.InstrF64Floor, wasm.InstrF64Trunc, wasm.InstrF64Nearest, wasm.InstrF64Copysign,
		wasm.InstrI32WrapI64, wasm.InstrI32TruncF32S, wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64S, wasm.InstrI32TruncF64U,
		wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U, wasm.InstrI64TruncF32S, wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64S, wasm.InstrI64TruncF64U,
		wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U, wasm.InstrF32DemoteF64,
		wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U, wasm.InstrF64PromoteF32,
		wasm.InstrI32ReinterpretF32, wasm.InstrI64ReinterpretF64, wasm.InstrF32ReinterpretI32, wasm.InstrF64ReinterpretI64:
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
	if v.Kind == wasm.ValVec && wasm.EqualValType(v, wasm.V128) {
		return nil
	}
	if v.Kind == wasm.ValRef {
		return p.unsupported("reference type", valTypeName(v), context)
	}
	return p.unsupported("value type", valTypeName(v), context)
}

func (p supportPass) globalType(v wasm.ValType, context string) error {
	if v.Kind == wasm.ValVec {
		return p.unsupported("global type", valTypeName(v), context)
	}
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
