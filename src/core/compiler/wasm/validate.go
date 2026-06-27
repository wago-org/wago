package wasm

// ValidateModule validates module-level indexes and typechecks the core of
// function bodies. It follows the stack-polymorphic validation algorithm used
// by the proposal-aware validator, with conservative support for proposal opcodes: unknown
// proposal instructions are still decoded but must be covered here before code
// using their stack effects is accepted.
func ValidateModule(m *Module) error {
	v := &moduleValidator{m: m, funcIndex: -1}
	// Multi-memory is intentionally outside wago's current support matrix. Reject
	// it before any backend/IR lowering can observe a non-zero memory index.
	if m.MemCount() > 1 {
		return v.err(ErrUnsupportedFeature, "multi-memory")
	}
	// The current runtime ABI carries exactly one internally allocated table
	// descriptor. Imported or multiple tables would make call_indirect/table
	// initializers silently address the wrong table, so reject them at the support
	// boundary until table imports/multi-table are implemented end-to-end.
	if m.TableCount() > 1 || m.ImportedTableCount() > 0 {
		return v.err(ErrUnsupportedFeature, "multi-table")
	}
	if err := v.validateModule(); err != nil {
		return err
	}
	for i, fn := range m.Code {
		abs := m.ImportedFuncCount() + i
		if i >= len(m.FuncTypes) {
			return v.err(ErrUnknownFunc, "code without function type")
		}
		ft, ok := v.funcType(uint32(abs))
		if !ok {
			return v.err(ErrUnknownType, "function type")
		}
		fv := &funcValidator{moduleValidator: v, funcIndex: abs}
		if err := fv.validateFunc(fn, ft); err != nil {
			return err
		}
	}
	return nil
}

type moduleValidator struct {
	m         *Module
	funcIndex int
}

const (
	maxTable32Limit  = uint64(1<<32 - 1)
	maxMemory32Pages = uint64(1 << 16)
)

func (v *moduleValidator) err(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}

func (v *moduleValidator) validateModule() error {
	for _, rt := range v.m.Types {
		for _, st := range rt.SubTypes {
			for _, sup := range st.Supers {
				if !v.validTypeIdx(sup) {
					return v.err(ErrUnknownType, "supertype")
				}
			}
			if st.Metadata.Describes != nil && !v.validTypeIdx(*st.Metadata.Describes) {
				return v.err(ErrUnknownType, "describes")
			}
			if st.Metadata.Descriptor != nil && !v.validTypeIdx(*st.Metadata.Descriptor) {
				return v.err(ErrUnknownType, "descriptor")
			}
			if err := v.validateCompType(st.Comp); err != nil {
				return err
			}
		}
	}
	for _, im := range v.m.Imports {
		if err := v.validateExternType(im.Type); err != nil {
			return err
		}
	}
	for _, ti := range v.m.FuncTypes {
		if !v.validTypeIdx(ti) || v.funcTypeFromTypeIdx(ti) == nil {
			return v.err(ErrUnknownType, "function section")
		}
	}
	for _, t := range v.m.Tables {
		if err := v.validateTableType(t.Type); err != nil {
			return err
		}
		if t.Init != nil {
			if err := v.validateConstExpr(*t.Init, RefVal(t.Type.Ref)); err != nil {
				return err
			}
		}
	}
	for _, mem := range v.m.Memories {
		if err := v.validateMemType(mem); err != nil {
			return err
		}
	}
	for _, tag := range v.m.Tags {
		if !v.validTypeIdx(tag.Type) || v.funcTypeFromTypeIdx(tag.Type) == nil {
			return v.err(ErrUnknownType, "tag")
		}
	}
	for _, g := range v.m.Globals {
		if err := v.validateGlobalType(g.Type); err != nil {
			return err
		}
		if err := v.validateConstExpr(g.Init, g.Type.Type); err != nil {
			return err
		}
	}
	seenExports := map[string]bool{}
	for _, ex := range v.m.Exports {
		if seenExports[ex.Name] {
			return v.err(ErrDuplicateExport, ex.Name)
		}
		seenExports[ex.Name] = true
		if !v.validExternIdx(ex.Index) {
			return v.err(ErrUnknownFunc, "export index")
		}
	}
	if v.m.Start != nil {
		ft, ok := v.funcType(uint32(*v.m.Start))
		if !ok {
			return v.err(ErrUnknownFunc, "start")
		}
		if len(ft.Params) != 0 || len(ft.Results) != 0 {
			return v.err(ErrTypeMismatch, "start type")
		}
	}
	for _, e := range v.m.Elements {
		if err := v.validateElem(e); err != nil {
			return err
		}
	}
	activeData := 0
	for _, d := range v.m.Data {
		if d.Mode.Kind == DataActive {
			activeData++
			mt, ok := v.memoryType(uint32(d.Mode.Mem))
			if !ok {
				return v.err(ErrUnknownMemory, "data")
			}
			want := I32
			if mt.Limits.Addr64 {
				want = I64
			}
			if err := v.validateConstExpr(d.Mode.Offset, want); err != nil {
				return err
			}
		}
	}
	if v.m.DataCount != nil && int(*v.m.DataCount) != len(v.m.Data) {
		return v.err(ErrInvalidDataCount, "")
	}
	_ = activeData
	return nil
}

func (v *moduleValidator) validateExternType(et ExternType) error {
	switch et.Kind {
	case ExternFunc:
		if v.funcTypeFromTypeIdx(et.Type) == nil {
			return v.err(ErrUnknownType, "import func")
		}
	case ExternTable:
		return v.validateTableType(et.Table)
	case ExternMem:
		return v.validateMemType(et.Mem)
	case ExternGlobal:
		return v.validateGlobalType(et.Global)
	case ExternTag:
		if v.funcTypeFromTypeIdx(et.Tag.Type) == nil {
			return v.err(ErrUnknownType, "import tag")
		}
	}
	return nil
}
func (v *moduleValidator) validateTableType(tt TableType) error {
	if err := v.validateRefType(tt.Ref); err != nil {
		return err
	}
	if !tt.Limits.Addr64 {
		// Table32 limits are u32 in the binary format; keep oversized values out
		// even though the shared Limits representation stores proposal limits as u64.
		if tt.Limits.Min > maxTable32Limit || (tt.Limits.Max != nil && *tt.Limits.Max > maxTable32Limit) {
			return v.err(ErrInvalidLimitRange, "table32 limit out of range")
		}
	}
	if tt.Limits.Max != nil && *tt.Limits.Max < tt.Limits.Min {
		return v.err(ErrInvalidLimitRange, "table max < min")
	}
	return nil
}

func (v *moduleValidator) validateGlobalType(gt GlobalType) error {
	return v.validateValType(gt.Type)
}

func (v *moduleValidator) validateCompType(ct CompType) error {
	switch ct.Kind {
	case CompFunc:
		for _, t := range ct.Params {
			if err := v.validateValType(t); err != nil {
				return err
			}
		}
		for _, t := range ct.Results {
			if err := v.validateValType(t); err != nil {
				return err
			}
		}
	case CompStruct:
		for _, f := range ct.Fields {
			if err := v.validateFieldType(f); err != nil {
				return err
			}
		}
	case CompArray:
		return v.validateFieldType(ct.Array)
	default:
		return v.err(ErrUnknownType, "component type")
	}
	return nil
}

func (v *moduleValidator) validateFieldType(ft FieldType) error {
	return v.validateStorageType(ft.Storage)
}

func (v *moduleValidator) validateStorageType(st StorageType) error {
	if st.Packed {
		return nil
	}
	return v.validateValType(st.Val)
}

func (v *moduleValidator) validateValType(t ValType) error {
	switch t.Kind {
	case ValNum, ValVec:
		return nil
	case ValRef:
		return v.validateRefType(t.Ref)
	default:
		return v.err(ErrUnknownType, "value type")
	}
}

func (v *moduleValidator) validateRefType(rt RefType) error {
	return v.validateHeapType(rt.Heap)
}

func (v *moduleValidator) validateHeapType(ht HeapType) error {
	switch ht.Kind {
	case HeapAbs:
		return nil
	case HeapTypeIndex:
		if !v.validTypeIdx(ht.Type) {
			return v.err(ErrUnknownType, "heap type")
		}
		return nil
	case HeapDefType:
		if ht.Def == nil {
			return v.err(ErrUnknownType, "heap def type")
		}
		return nil
	default:
		return v.err(ErrUnknownType, "heap type")
	}
}
func (v *moduleValidator) validateMemType(mt MemType) error {
	if mt.Shared && mt.Limits.Max == nil {
		return v.err(ErrInvalidSharedMemory, "")
	}
	if !mt.Limits.Addr64 {
		// Memory32 limits are page counts bounded to the 4 GiB address space.
		// Reject values that only fit because the common Limits storage is uint64.
		if mt.Limits.Min > maxMemory32Pages || (mt.Limits.Max != nil && *mt.Limits.Max > maxMemory32Pages) {
			return v.err(ErrInvalidLimitRange, "memory32 limit out of range")
		}
	}
	if mt.Limits.Max != nil && *mt.Limits.Max < mt.Limits.Min {
		return v.err(ErrInvalidLimitRange, "memory max < min")
	}
	return nil
}
func (v *moduleValidator) funcType(idx uint32) (*CompType, bool) {
	n := uint32(0)
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternFunc {
			if n == idx {
				ft := v.funcTypeFromTypeIdx(im.Type.Type)
				return ft, ft != nil
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.FuncTypes) {
		return nil, false
	}
	ft := v.funcTypeFromTypeIdx(v.m.FuncTypes[local])
	return ft, ft != nil
}
func (v *moduleValidator) globalType(idx uint32) (GlobalType, bool) {
	n := uint32(0)
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternGlobal {
			if n == idx {
				return im.Type.Global, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Globals) {
		return GlobalType{}, false
	}
	return v.m.Globals[local].Type, true
}
func (v *moduleValidator) tableType(idx uint32) (TableType, bool) {
	n := uint32(0)
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternTable {
			if n == idx {
				return im.Type.Table, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Tables) {
		return TableType{}, false
	}
	return v.m.Tables[local].Type, true
}

func (v *moduleValidator) memoryType(idx uint32) (MemType, bool) {
	n := uint32(0)
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternMem {
			if n == idx {
				return im.Type.Mem, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Memories) {
		return MemType{}, false
	}
	return v.m.Memories[local], true
}
func (v *moduleValidator) validExternIdx(x ExternIdx) bool {
	switch x.Kind {
	case ExternFunc:
		return int(x.Index) < v.m.FuncCount()
	case ExternTable:
		return int(x.Index) < v.m.TableCount()
	case ExternMem:
		return int(x.Index) < v.m.MemCount()
	case ExternGlobal:
		return int(x.Index) < v.m.GlobalCount()
	case ExternTag:
		return int(x.Index) < v.m.TagCount()
	}
	return false
}

func (v *moduleValidator) validateConstExpr(e Expr, want ValType) error {
	fv := &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true}
	fv.pushCtrl(ctrlFunc, nil, []ValType{want})
	for _, in := range e.Instrs {
		if err := fv.step(in); err != nil {
			return err
		}
	}
	_, err := fv.popCtrl()
	return err
}
func (v *moduleValidator) validateElem(e Elem) error {
	elemRef, err := v.validateElemPayload(e)
	if err != nil {
		return err
	}
	if e.Mode.Kind == ElemActive {
		tt, ok := v.tableType(uint32(e.Mode.Table))
		if !ok {
			return v.err(ErrUnknownTable, "elem")
		}
		want := I32
		if tt.Limits.Addr64 {
			want = I64
		}
		if err := v.validateConstExpr(e.Mode.Offset, want); err != nil {
			return err
		}
		// Active segments initialize a table directly, so their element reference
		// type must be assignment-compatible with the target table element type.
		if !v.refSubtype(elemRef, tt.Ref) {
			return v.err(ErrTypeMismatch, "element type does not match table")
		}
	}
	return nil
}

func (v *moduleValidator) validateElemPayload(e Elem) (RefType, error) {
	switch e.Kind.Kind {
	case ElemFuncs:
		for _, f := range e.Kind.Funcs {
			if int(f) >= v.m.FuncCount() {
				return RefType{}, v.err(ErrUnknownFunc, "elem")
			}
		}
		return FuncRef.Ref, nil
	case ElemFuncExprs:
		for _, ex := range e.Kind.Exprs {
			if err := v.validateConstExpr(ex, FuncRef); err != nil {
				return RefType{}, err
			}
		}
		return FuncRef.Ref, nil
	case ElemTypedExprs:
		// Validate the declared element reference type even when the segment has no
		// initializer expressions; empty typed segments still carry type indexes.
		if err := v.validateRefType(e.Kind.Ref); err != nil {
			return RefType{}, err
		}
		for _, ex := range e.Kind.Exprs {
			if err := v.validateConstExpr(ex, RefVal(e.Kind.Ref)); err != nil {
				return RefType{}, err
			}
		}
		return e.Kind.Ref, nil
	default:
		return RefType{}, v.err(ErrTypeMismatch, "unknown element kind")
	}
}

type val struct {
	t       ValType
	unknown bool
}
type ctrlKind uint8

const (
	ctrlFunc ctrlKind = iota
	ctrlBlock
	ctrlLoop
	ctrlIf
)

type ctrlFrame struct {
	kind        ctrlKind
	in, out     []ValType
	height      int
	unreachable bool
}

type funcValidator struct {
	*moduleValidator
	funcIndex int
	vals      []val
	ctrls     []ctrlFrame
	locals    []ValType
	constOnly bool
}

func (v *funcValidator) verr(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}
func (v *funcValidator) validateFunc(fn Func, ft *CompType) error {
	v.locals = append([]ValType{}, ft.Params...)
	for _, run := range fn.Locals.Runs {
		if err := v.validateValType(run.Type); err != nil {
			return err
		}
		for i := uint32(0); i < run.Count; i++ {
			v.locals = append(v.locals, run.Type)
		}
	}
	v.pushCtrl(ctrlFunc, nil, ft.Results)
	for _, in := range fn.Body.Instrs {
		if err := v.step(in); err != nil {
			return err
		}
	}
	_, err := v.popCtrl()
	return err
}
func (v *funcValidator) top() *ctrlFrame { return &v.ctrls[len(v.ctrls)-1] }
func (v *funcValidator) push(t ValType)  { v.vals = append(v.vals, val{t: t}) }
func (v *funcValidator) pushAll(ts []ValType) {
	for _, t := range ts {
		v.push(t)
	}
}
func (v *funcValidator) pop() (val, error) {
	f := v.top()
	if len(v.vals) == f.height {
		if f.unreachable {
			return val{unknown: true}, nil
		}
		return val{}, v.verr(ErrTypeMismatch, "stack underflow")
	}
	x := v.vals[len(v.vals)-1]
	v.vals = v.vals[:len(v.vals)-1]
	return x, nil
}
func (v *funcValidator) popExpect(t ValType) error {
	x, err := v.pop()
	if err != nil {
		return err
	}
	if !x.unknown && !v.subtype(x.t, t) {
		return v.verr(ErrTypeMismatch, x.t.String()+" is not "+t.String())
	}
	return nil
}
func (v *funcValidator) popAll(ts []ValType) error {
	for i := len(ts) - 1; i >= 0; i-- {
		if err := v.popExpect(ts[i]); err != nil {
			return err
		}
	}
	return nil
}
func (v *funcValidator) pushCtrl(k ctrlKind, in, out []ValType) error {
	if err := v.popAll(in); err != nil {
		return err
	}
	v.ctrls = append(v.ctrls, ctrlFrame{kind: k, in: in, out: out, height: len(v.vals)})
	v.pushAll(in)
	return nil
}
func (v *funcValidator) popCtrl() (ctrlFrame, error) {
	if len(v.ctrls) == 0 {
		return ctrlFrame{}, v.verr(ErrTypeMismatch, "no control")
	}
	f := *v.top()
	if err := v.popAll(f.out); err != nil {
		return f, err
	}
	if len(v.vals) != f.height {
		return f, v.verr(ErrTypeMismatch, "leftover values")
	}
	v.ctrls = v.ctrls[:len(v.ctrls)-1]
	v.pushAll(f.out)
	return f, nil
}
func (v *funcValidator) unreachable() {
	f := v.top()
	v.vals = v.vals[:f.height]
	v.ctrls[len(v.ctrls)-1].unreachable = true
}
func (v *funcValidator) label(depth uint32) ([]ValType, error) {
	if int(depth) >= len(v.ctrls) {
		return nil, v.verr(ErrUnknownLabel, "")
	}
	f := v.ctrls[len(v.ctrls)-1-int(depth)]
	if f.kind == ctrlLoop {
		return f.in, nil
	}
	return f.out, nil
}
func (v *funcValidator) subtype(a, b ValType) bool {
	if b.Kind == ValBot || a.Kind == ValBot {
		return true
	}
	if equalValType(a, b) {
		return true
	}
	if a.Kind == ValRef && b.Kind == ValRef {
		return v.refSubtype(a.Ref, b.Ref)
	}
	return false
}
func (v *funcValidator) refSubtype(a, b RefType) bool {
	return v.moduleValidator.refSubtype(a, b)
}
func absHeapSubtype(a, b AbsHeapType) bool {
	if a == b {
		return true
	}
	switch a {
	case HeapNoFunc:
		return b == HeapFunc
	case HeapNoExtern:
		return b == HeapExtern
	case HeapNone:
		return b == HeapAny || b == HeapEq || b == HeapStruct || b == HeapArray || b == HeapI31
	case HeapI31, HeapStruct, HeapArray:
		return b == HeapEq || b == HeapAny
	case HeapEq:
		return b == HeapAny
	case HeapFunc:
		return b == HeapAny
	case HeapString:
		return b == HeapAny
	}
	return false
}
func (v *funcValidator) blockSig(bt BlockType) (in, out []ValType, err error) {
	switch bt.Kind {
	case BlockVoid:
		return nil, nil, nil
	case BlockVal:
		if err := v.validateValType(bt.Val); err != nil {
			return nil, nil, err
		}
		return nil, []ValType{bt.Val}, nil
	case BlockTypeIndex:
		ft := v.funcTypeFromTypeIdx(bt.Type)
		if ft == nil {
			return nil, nil, v.verr(ErrUnknownType, "block")
		}
		return ft.Params, ft.Results, nil
	}
	return nil, nil, v.verr(ErrUnknownType, "")
}
