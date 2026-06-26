package wasm3

// ValidateModule validates module-level indexes and typechecks the core of
// function bodies. It follows the stack-polymorphic validation algorithm used
// by the proposal-aware validator, with conservative support for proposal opcodes: unknown
// proposal instructions are still decoded but must be covered here before code
// using their stack effects is accepted.
func ValidateModule(m *Module) error {
	v := &moduleValidator{m: m, funcIndex: -1}
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
		return nil
	case ExternTag:
		if v.funcTypeFromTypeIdx(et.Tag.Type) == nil {
			return v.err(ErrUnknownType, "import tag")
		}
	}
	return nil
}
func (v *moduleValidator) validateTableType(tt TableType) error {
	if tt.Limits.Max != nil && *tt.Limits.Max < tt.Limits.Min {
		return v.err(ErrInvalidLimitRange, "table max < min")
	}
	return nil
}
func (v *moduleValidator) validateMemType(mt MemType) error {
	if mt.Shared && mt.Limits.Max == nil {
		return v.err(ErrInvalidSharedMemory, "")
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
	if e.Mode.Kind == ElemActive {
		if int(e.Mode.Table) >= v.m.TableCount() {
			return v.err(ErrUnknownTable, "elem")
		}
		if err := v.validateConstExpr(e.Mode.Offset, I32); err != nil {
			return err
		}
	}
	switch e.Kind.Kind {
	case ElemFuncs:
		for _, f := range e.Kind.Funcs {
			if int(f) >= v.m.FuncCount() {
				return v.err(ErrUnknownFunc, "elem")
			}
		}
	case ElemFuncExprs:
		for _, ex := range e.Kind.Exprs {
			if err := v.validateConstExpr(ex, FuncRef); err != nil {
				return err
			}
		}
	case ElemTypedExprs:
		for _, ex := range e.Kind.Exprs {
			if err := v.validateConstExpr(ex, RefVal(e.Kind.Ref)); err != nil {
				return err
			}
		}
	}
	return nil
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
