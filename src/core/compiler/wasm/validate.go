package wasm

// vt is a ValType or vtUnknown for unreachable-code stack polymorphism.
type vt int16

const vtUnknown vt = -1

type ctrlKind uint8

const (
	ckFunc ctrlKind = iota
	ckBlock
	ckLoop
	ckIf
	ckElse
)

type ctrlFrame struct {
	kind        ctrlKind
	in          []ValType
	out         []ValType
	height      int
	unreachable bool
}

func labelTypes(f *ctrlFrame) []ValType {
	if f.kind == ckLoop {
		return f.in
	}
	return f.out
}

type validator struct {
	m        *Module
	r        *Reader
	vals     []vt
	ctrls    []ctrlFrame
	locals   []ValType
	memCount int
	tblCount int
	fnIndex  int
}

func (v *validator) verr(c ErrCode) error { return &ValidationError{Code: c, Func: v.fnIndex} }

func (v *validator) topCtrl() *ctrlFrame { return &v.ctrls[len(v.ctrls)-1] }

func (v *validator) pushVal(t vt) { v.vals = append(v.vals, t) }

func (v *validator) popVal() (vt, error) {
	f := v.topCtrl()
	if len(v.vals) == f.height {
		if f.unreachable {
			return vtUnknown, nil
		}
		return 0, v.verr(ErrTypeMismatch) // operand stack underflow
	}
	t := v.vals[len(v.vals)-1]
	v.vals = v.vals[:len(v.vals)-1]
	return t, nil
}

func (v *validator) popExpect(expect vt) (vt, error) {
	actual, err := v.popVal()
	if err != nil {
		return 0, err
	}
	if actual != expect && actual != vtUnknown && expect != vtUnknown {
		return 0, v.verr(ErrTypeMismatch)
	}
	if actual == vtUnknown {
		return expect, nil
	}
	return actual, nil
}

func (v *validator) popT(t ValType) error { _, err := v.popExpect(vt(t)); return err }
func (v *validator) pushT(t ValType)      { v.pushVal(vt(t)) }

func (v *validator) pushVals(ts []ValType) {
	for _, t := range ts {
		v.pushT(t)
	}
}

func (v *validator) popVals(ts []ValType) error {
	for i := len(ts) - 1; i >= 0; i-- {
		if err := v.popT(ts[i]); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) pushCtrl(kind ctrlKind, in, out []ValType) error {
	if err := v.popVals(in); err != nil {
		return err
	}
	v.ctrls = append(v.ctrls, ctrlFrame{kind: kind, in: in, out: out, height: len(v.vals)})
	v.pushVals(in)
	return nil
}

func (v *validator) popCtrl() (ctrlFrame, error) {
	if len(v.ctrls) == 0 {
		return ctrlFrame{}, v.verr(ErrTypeMismatch)
	}
	f := v.topCtrl()
	if err := v.popVals(f.out); err != nil {
		return ctrlFrame{}, err
	}
	if len(v.vals) != f.height {
		return ctrlFrame{}, v.verr(ErrTypeMismatch) // leftover operands
	}
	frame := *f
	v.ctrls = v.ctrls[:len(v.ctrls)-1]
	return frame, nil
}

func (v *validator) setUnreachable() {
	f := v.topCtrl()
	v.vals = v.vals[:f.height]
	v.ctrls[len(v.ctrls)-1].unreachable = true
}

// Validate checks module structure and function bodies.
func Validate(m *Module) error {
	v := &validator{m: m, fnIndex: -1}
	v.memCount = m.memCount()
	v.tblCount = m.tableCount()

	if err := m.validateModuleLevel(); err != nil {
		return err
	}

	if m.Start != nil {
		ft, ok := m.funcType(*m.Start)
		if !ok {
			return &ValidationError{Code: ErrUnknownFunc, Func: -1}
		}
		if len(ft.Params) != 0 || len(ft.Results) != 0 {
			return &ValidationError{Code: ErrTypeMismatch, Func: -1}
		}
	}

	for i := range m.Code {
		ti := m.Functions[i]
		if int(ti) >= len(m.Types) {
			return &ValidationError{Code: ErrUnknownType, Func: i + m.ImportedFuncCount()}
		}
		v.fnIndex = i + m.ImportedFuncCount()
		if err := v.validateFunc(&m.Code[i], &m.Types[ti]); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) validateFunc(code *Code, ft *FuncType) error {
	v.locals = v.locals[:0]
	v.locals = append(v.locals, ft.Params...)
	for _, le := range code.Locals {
		for i := uint32(0); i < le.Count; i++ {
			v.locals = append(v.locals, le.Type)
		}
	}
	v.vals = v.vals[:0]
	v.ctrls = v.ctrls[:0]
	v.ctrls = append(v.ctrls, ctrlFrame{kind: ckFunc, out: ft.Results, height: 0})
	v.r = NewReader(code.Body)

	for len(v.ctrls) > 0 {
		if !v.r.HasNext() {
			return v.verr(ErrTypeMismatch) // ran out of bytes before closing all blocks
		}
		op, err := v.r.Byte()
		if err != nil {
			return err
		}
		if err := v.step(op); err != nil {
			return err
		}
	}
	if v.r.HasNext() {
		return v.verr(ErrTypeMismatch) // trailing bytes after function end
	}
	return nil
}

func (v *validator) binop(t ValType) error {
	if err := v.popT(t); err != nil {
		return err
	}
	if err := v.popT(t); err != nil {
		return err
	}
	v.pushT(t)
	return nil
}

func (v *validator) unop(t ValType) error {
	if err := v.popT(t); err != nil {
		return err
	}
	v.pushT(t)
	return nil
}

func (v *validator) cmp(t ValType) error {
	if err := v.popT(t); err != nil {
		return err
	}
	if err := v.popT(t); err != nil {
		return err
	}
	v.pushT(I32)
	return nil
}

func (v *validator) testop(t ValType) error {
	if err := v.popT(t); err != nil {
		return err
	}
	v.pushT(I32)
	return nil
}

func (v *validator) cvt(from, to ValType) error {
	if err := v.popT(from); err != nil {
		return err
	}
	v.pushT(to)
	return nil
}

func (m *Module) memCount() int {
	n := len(m.Memories)
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternMem {
			n++
		}
	}
	return n
}

func (m *Module) tableCount() int {
	n := len(m.Tables)
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternTable {
			n++
		}
	}
	return n
}

func (m *Module) funcType(idx uint32) (*FuncType, bool) {
	i := 0
	for j := range m.Imports {
		if m.Imports[j].Kind != ExternFunc {
			continue
		}
		if uint32(i) == idx {
			if int(m.Imports[j].TypeIndex) >= len(m.Types) {
				return nil, false
			}
			return &m.Types[m.Imports[j].TypeIndex], true
		}
		i++
	}
	local := int(idx) - i
	if local < 0 || local >= len(m.Functions) {
		return nil, false
	}
	ti := m.Functions[local]
	if int(ti) >= len(m.Types) {
		return nil, false
	}
	return &m.Types[ti], true
}

func (m *Module) globalType(idx uint32) (GlobalType, bool) { return m.GlobalType(idx) }

// GlobalType returns the declared type for a wasm global index.
func (m *Module) GlobalType(idx uint32) (GlobalType, bool) {
	i := 0
	for j := range m.Imports {
		if m.Imports[j].Kind != ExternGlobal {
			continue
		}
		if uint32(i) == idx {
			return m.Imports[j].Global, true
		}
		i++
	}
	local := int(idx) - i
	if local < 0 || local >= len(m.Globals) {
		return GlobalType{}, false
	}
	return m.Globals[local].Type, true
}
