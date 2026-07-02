package wasm

// directCodeBody is the part of a code-section function body the direct
// validator keeps: compact local runs plus raw expression bytes. It deliberately
// skips the Expr/Instruction tree used by DecodeModule.
type directCodeBody struct {
	locals Locals
	body   []byte
}

type directConstExpr struct {
	body []byte
}

type directElem struct {
	modeKind ElemModeKind
	table    TableIdx
	offset   directConstExpr
	kind     ElemKindKind
	ref      RefType
	hasFuncs bool
	maxFunc  FuncIdx
	funcs    []FuncIdx
	elemLen  uint32
	exprs    []directConstExpr
}

type directValidationEnv struct {
	tableHasInit []bool
	tableInits   []directConstExpr
	globalInits  []directConstExpr
	elements     []directElem
	dataOffsets  []directConstExpr
}

type directModule struct {
	m        Module
	code     []directCodeBody
	direct   directValidationEnv
	seenName bool
}

// DecodedModuleNoBodyAST is a WebAssembly module decoded without materializing
// structured function-body Expr/Instruction trees. Module contains the compact
// section metadata plus raw function BodyBytes; the unexported validation state
// keeps const-expression summaries needed by ValidateModuleNoBodyAST.
type DecodedModuleNoBodyAST struct {
	Module *Module
	direct directValidationEnv
}

// ValidateModuleDirect validates data while reading the Wasm binary without
// materializing function-body Expr/Instruction IR. This is a temporary
// performance-experiment lane: module metadata still reuses the existing compact
// wasm structs so the validator semantics can be ported incrementally.
func ValidateModuleDirect(data []byte) error {
	dm, err := DecodeModuleNoBodyAST(data)
	if err != nil {
		return err
	}
	return ValidateModuleNoBodyAST(dm)
}

// DecodeModuleNoBodyAST decodes data without materializing the structured
// Expr/Instruction tree for function bodies. Function Code entries carry Locals
// and BodyBytes, while Body is left empty. Call ValidateModuleNoBodyAST before
// handing the module to lowering or execution paths.
func DecodeModuleNoBodyAST(data []byte) (*DecodedModuleNoBodyAST, error) {
	dm, err := decodeDirectModule(data)
	if err != nil {
		return nil, err
	}
	dm.populateCodeBodies()
	return &DecodedModuleNoBodyAST{Module: &dm.m, direct: dm.direct}, nil
}

// ValidateModuleNoBodyAST validates a module produced by DecodeModuleNoBodyAST
// without requiring the structured function-body AST.
func ValidateModuleNoBodyAST(dm *DecodedModuleNoBodyAST) error {
	if dm == nil || dm.Module == nil {
		return &ValidationError{Code: ErrTypeMismatch, Func: -1, Detail: "nil no-body module"}
	}
	v := &moduleValidator{m: dm.Module, funcIndex: -1, direct: &dm.direct}
	if err := v.validateModule(); err != nil {
		return err
	}
	for i, fn := range dm.Module.Code {
		abs := dm.Module.ImportedFuncCount() + i
		if i >= len(dm.Module.FuncTypes) {
			return v.err(ErrUnknownFunc, "code without function type")
		}
		ft, ok := v.funcType(uint32(abs))
		if !ok {
			return v.err(ErrUnknownType, "function type")
		}
		fv := &funcValidator{moduleValidator: v, funcIndex: abs}
		body := directCodeBody{locals: fn.Locals, body: fn.BodyBytes}
		if err := fv.validateFuncDirect(body, ft); err != nil {
			return err
		}
	}
	return nil
}

func (dm *directModule) populateCodeBodies() {
	dm.m.Code = make([]Func, len(dm.code))
	for i, body := range dm.code {
		dm.m.Code[i] = Func{Locals: body.locals, BodyBytes: body.body}
	}
	for i := range dm.m.Tables {
		if i >= len(dm.direct.tableHasInit) || !dm.direct.tableHasInit[i] {
			continue
		}
		e := directExpr(dm.direct.tableInits[i])
		dm.m.Tables[i].Init = &e
	}
	for i := range dm.m.Globals {
		if i >= len(dm.direct.globalInits) {
			break
		}
		dm.m.Globals[i].Init = directExpr(dm.direct.globalInits[i])
	}
	for i := range dm.m.Elements {
		if i >= len(dm.direct.elements) {
			break
		}
		de := dm.direct.elements[i]
		if de.modeKind == ElemActive {
			dm.m.Elements[i].Mode.Offset = directExpr(de.offset)
		}
		switch de.kind {
		case ElemFuncs:
			dm.m.Elements[i].Kind.Funcs = de.funcs
		case ElemFuncExprs, ElemTypedExprs:
			dm.m.Elements[i].Kind.Exprs = make([]Expr, len(de.exprs))
			for j := range de.exprs {
				dm.m.Elements[i].Kind.Exprs[j] = directExpr(de.exprs[j])
			}
		}
	}
	for i := range dm.m.Data {
		if i >= len(dm.direct.dataOffsets) {
			break
		}
		if dm.m.Data[i].Mode.Kind == DataActive {
			dm.m.Data[i].Mode.Offset = directExpr(dm.direct.dataOffsets[i])
		}
	}
}

func directExpr(e directConstExpr) Expr {
	return Expr{BodyBytes: e.body}
}

func decodeDirectModule(data []byte) (*directModule, error) {
	r := newReader(data)
	magic, err := r.bytes(4)
	if err != nil {
		return nil, err
	}
	if string(magic) != "\x00asm" {
		return nil, &DecodeError{Code: ErrBadMagic, Offset: 0}
	}
	ver, err := r.le32()
	if err != nil {
		return nil, err
	}
	if ver != 1 {
		return nil, &DecodeError{Code: ErrBadVersion, Offset: 4}
	}
	dm := &directModule{}
	lastOrder := 0
	seen := map[byte]bool{}
	var stringRefs [][]byte
	for r.has() {
		id, err := r.byte()
		if err != nil {
			return nil, err
		}
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		start := r.off()
		payload, err := r.bytes(int(size))
		if err != nil {
			return nil, err
		}
		end := r.off()
		if id != secCustom {
			ord, ok := sectionOrder[id]
			if !ok {
				return nil, &DecodeError{Code: ErrInvalidSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			if ord < lastOrder {
				return nil, &DecodeError{Code: ErrSectionOrder, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			if seen[id] {
				return nil, &DecodeError{Code: ErrDuplicateSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			seen[id] = true
			lastOrder = ord
		}
		sub := newReader(payload)
		switch id {
		case secCustom:
			err = dm.decodeDirectCustomSection(sub)
		case secStringRefs:
			err = decodeDirectStringRefsSection(&dm.m, sub)
		case secTable:
			err = decodeDirectTableSection(dm, sub)
		case secGlobal:
			err = decodeDirectGlobalSection(dm, sub)
		case secElement:
			err = decodeDirectElementSection(dm, sub)
		case secCode:
			dm.code, err = decodeDirectCodeSection(sub)
		case secData:
			err = decodeDirectDataSection(dm, sub)
		default:
			err = decodeSection(&dm.m, sub, id, &stringRefs)
		}
		if err != nil {
			if de, ok := err.(*DecodeError); ok {
				de.SectionID = id
				de.SectionStart = start
				de.SectionEnd = end
				if de.Offset == 0 {
					de.Offset = start
				}
				return nil, de
			}
			return nil, err
		}
		if sub.has() {
			return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: start + sub.off(), SectionID: id, SectionStart: start, SectionEnd: end}
		}
	}
	if len(dm.m.FuncTypes) != len(dm.code) {
		return nil, &DecodeError{Code: ErrInvalidModule, Offset: len(data)}
	}
	return dm, nil
}

func (dm *directModule) decodeDirectCustomSection(r *reader) error {
	name, err := r.name()
	if err != nil {
		return err
	}
	payload, err := r.bytes(r.left())
	if err != nil {
		return err
	}
	if name == "name" {
		if dm.seenName {
			return &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
		}
		ns, err := decodeNameSec(payload)
		if err != nil {
			return err
		}
		dm.m.NameSec = ns
		dm.m.RawNameSecPayload = append([]byte(nil), payload...)
		dm.seenName = true
	}
	dm.m.Customs = append(dm.m.Customs, CustomSec{Name: name, Data: append([]byte(nil), payload...)})
	return nil
}

func decodeDirectStringRefsSection(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	refs := make([][]byte, 0, capHint)
	for i := uint32(0); i < n; i++ {
		sz, err := r.u32()
		if err != nil {
			return err
		}
		b, err := r.bytes(int(sz))
		if err != nil {
			return err
		}
		refs = append(refs, append([]byte(nil), b...))
	}
	m.StringRefs = refs
	return nil
}

func decodeDirectTableSection(dm *directModule, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	dm.m.Tables = make([]Table, 0, capHint)
	dm.direct.tableHasInit = make([]bool, 0, capHint)
	dm.direct.tableInits = make([]directConstExpr, 0, capHint)
	for i := uint32(0); i < n; i++ {
		if b, ok := r.peek(); ok && b == 0x40 {
			_, _ = r.byte()
			if b2, ok := r.peek(); !ok || b2 != 0x00 {
				return &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			_, _ = r.byte()
			tt, err := decodeTableType(r)
			if err != nil {
				return err
			}
			init, err := readDirectConstExprBytes(r)
			if err != nil {
				return err
			}
			dm.m.Tables = append(dm.m.Tables, Table{Type: tt})
			dm.direct.tableHasInit = append(dm.direct.tableHasInit, true)
			dm.direct.tableInits = append(dm.direct.tableInits, init)
			continue
		}
		tt, err := decodeTableType(r)
		if err != nil {
			return err
		}
		dm.m.Tables = append(dm.m.Tables, Table{Type: tt})
		dm.direct.tableHasInit = append(dm.direct.tableHasInit, false)
		dm.direct.tableInits = append(dm.direct.tableInits, directConstExpr{})
	}
	return nil
}

func decodeDirectGlobalSection(dm *directModule, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	dm.m.Globals = make([]Global, 0, capHint)
	dm.direct.globalInits = make([]directConstExpr, 0, capHint)
	for i := uint32(0); i < n; i++ {
		gt, err := decodeGlobalType(r)
		if err != nil {
			return err
		}
		init, err := readDirectConstExprBytes(r)
		if err != nil {
			return err
		}
		dm.m.Globals = append(dm.m.Globals, Global{Type: gt})
		dm.direct.globalInits = append(dm.direct.globalInits, init)
	}
	return nil
}

func decodeDirectDataSection(dm *directModule, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	dm.m.Data = make([]Data, 0, capHint)
	dm.direct.dataOffsets = make([]directConstExpr, 0, capHint)
	for i := uint32(0); i < n; i++ {
		d, off, err := decodeDirectData(r)
		if err != nil {
			return err
		}
		dm.m.Data = append(dm.m.Data, d)
		dm.direct.dataOffsets = append(dm.direct.dataOffsets, off)
	}
	return nil
}

func decodeDirectData(r *reader) (Data, directConstExpr, error) {
	flags, err := r.u32()
	if err != nil {
		return Data{}, directConstExpr{}, err
	}
	d := Data{}
	var off directConstExpr
	switch flags {
	case 0:
		e, err := readDirectConstExprBytes(r)
		if err != nil {
			return d, off, err
		}
		off = e
		d.Mode = DataMode{Kind: DataActive}
	case 1:
		d.Mode = DataMode{Kind: DataPassive}
	case 2:
		mi, err := r.u32()
		if err != nil {
			return d, off, err
		}
		e, err := readDirectConstExprBytes(r)
		if err != nil {
			return d, off, err
		}
		off = e
		d.Mode = DataMode{Kind: DataActive, Mem: MemIdx(mi)}
	default:
		return d, off, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	n, err := r.u32()
	if err != nil {
		return d, off, err
	}
	d.Init, err = r.bytes(int(n))
	if err != nil {
		return d, off, err
	}
	return d, off, nil
}

func decodeDirectElementSection(dm *directModule, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	dm.m.Elements = make([]Elem, 0, capHint)
	dm.direct.elements = make([]directElem, 0, capHint)
	for i := uint32(0); i < n; i++ {
		e, elem, err := decodeDirectElem(r)
		if err != nil {
			return err
		}
		dm.m.Elements = append(dm.m.Elements, e)
		dm.direct.elements = append(dm.direct.elements, elem)
	}
	return nil
}

func decodeDirectElem(r *reader) (Elem, directElem, error) {
	flags, err := r.u32()
	if err != nil {
		return Elem{}, directElem{}, err
	}
	var e Elem
	var de directElem
	switch flags {
	case 0:
		off, err := readDirectConstExprBytes(r)
		if err != nil {
			return e, de, err
		}
		de.modeKind = ElemActive
		de.offset = off
		de.kind = ElemFuncs
		if err := readDirectFuncIdxSummary(r, &de); err != nil {
			return e, de, err
		}
	case 1:
		if err := readElemKind(r); err != nil {
			return e, de, err
		}
		de.modeKind = ElemPassive
		de.kind = ElemFuncs
		if err := readDirectFuncIdxSummary(r, &de); err != nil {
			return e, de, err
		}
	case 2:
		t, err := r.u32()
		if err != nil {
			return e, de, err
		}
		off, err := readDirectConstExprBytes(r)
		if err != nil {
			return e, de, err
		}
		if err := readElemKind(r); err != nil {
			return e, de, err
		}
		de.modeKind = ElemActive
		de.table = TableIdx(t)
		de.offset = off
		de.kind = ElemFuncs
		if err := readDirectFuncIdxSummary(r, &de); err != nil {
			return e, de, err
		}
	case 3:
		if err := readElemKind(r); err != nil {
			return e, de, err
		}
		de.modeKind = ElemDeclarative
		de.kind = ElemFuncs
		if err := readDirectFuncIdxSummary(r, &de); err != nil {
			return e, de, err
		}
	case 4:
		off, err := readDirectConstExprBytes(r)
		if err != nil {
			return e, de, err
		}
		exprs, err := readDirectConstExprVec(r)
		if err != nil {
			return e, de, err
		}
		de.modeKind = ElemActive
		de.offset = off
		de.kind = ElemFuncExprs
		de.elemLen = uint32(len(exprs))
		de.exprs = exprs
	case 5:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, de, err
		}
		exprs, err := readDirectConstExprVec(r)
		if err != nil {
			return e, de, err
		}
		de.modeKind = ElemPassive
		de.kind = ElemTypedExprs
		de.ref = rt
		de.elemLen = uint32(len(exprs))
		de.exprs = exprs
	case 6:
		t, err := r.u32()
		if err != nil {
			return e, de, err
		}
		off, err := readDirectConstExprBytes(r)
		if err != nil {
			return e, de, err
		}
		rt, err := decodeRefType(r)
		if err != nil {
			return e, de, err
		}
		exprs, err := readDirectConstExprVec(r)
		if err != nil {
			return e, de, err
		}
		de.modeKind = ElemActive
		de.table = TableIdx(t)
		de.offset = off
		de.kind = ElemTypedExprs
		de.ref = rt
		de.elemLen = uint32(len(exprs))
		de.exprs = exprs
	case 7:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, de, err
		}
		exprs, err := readDirectConstExprVec(r)
		if err != nil {
			return e, de, err
		}
		de.modeKind = ElemDeclarative
		de.kind = ElemTypedExprs
		de.ref = rt
		de.elemLen = uint32(len(exprs))
		de.exprs = exprs
	default:
		return e, de, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	e.Mode = ElemMode{Kind: de.modeKind, Table: de.table}
	e.Kind = ElemKind{Kind: de.kind, Ref: de.ref}
	return e, de, nil
}

func readDirectFuncIdxSummary(r *reader, de *directElem) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	de.elemLen = n
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	de.funcs = make([]FuncIdx, 0, capHint)
	for i := uint32(0); i < n; i++ {
		x, err := r.u32()
		if err != nil {
			return err
		}
		fi := FuncIdx(x)
		de.funcs = append(de.funcs, fi)
		if !de.hasFuncs || x > uint32(de.maxFunc) {
			de.hasFuncs = true
			de.maxFunc = fi
		}
	}
	return nil
}

func readDirectConstExprVec(r *reader) ([]directConstExpr, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	exprs := make([]directConstExpr, 0, capHint)
	for i := uint32(0); i < n; i++ {
		e, err := readDirectConstExprBytes(r)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, e)
	}
	return exprs, nil
}

func readDirectConstExprBytes(r *reader) (directConstExpr, error) {
	start := r.off()
	depth := 0
	for {
		if !r.has() {
			return directConstExpr{}, &DecodeError{Code: ErrSectionSizeMismatch, Offset: r.off()}
		}
		op, err := decodeDirectOp(r)
		if err != nil {
			return directConstExpr{}, err
		}
		switch op.kind {
		case directBlock, directLoop, directIf, directTryTable:
			depth++
			if depth > maxInstructionNestingDepth {
				return directConstExpr{}, &DecodeError{Code: ErrInstructionNestingLimitExceeded, Offset: r.off()}
			}
		case directEnd:
			if depth == 0 {
				return directConstExpr{body: r.data[start:r.off()]}, nil
			}
			depth--
		}
	}
}

func decodeDirectCodeSection(r *reader) ([]directCodeBody, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	out := make([]directCodeBody, 0, capHint)
	for i := uint32(0); i < n; i++ {
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		body, err := r.bytes(int(size))
		if err != nil {
			return nil, err
		}
		sub := newReader(body)
		locals, err := decodeLocals(sub)
		if err != nil {
			return nil, err
		}
		exprStart := sub.off()
		out = append(out, directCodeBody{locals: locals, body: body[exprStart:]})
	}
	return out, nil
}

func (v *moduleValidator) validateConstExprDirect(e directConstExpr, want ValType) error {
	fv := &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true}
	fv.pushCtrl(ctrlFunc, nil, []ValType{want})
	r := newReader(e.body)
	for {
		if len(fv.ctrls) == 0 {
			if r.has() {
				return &DecodeError{Code: ErrSectionSizeMismatch, Offset: r.off()}
			}
			return nil
		}
		op, err := decodeDirectOp(r)
		if err != nil {
			return err
		}
		if op.kind != directInstr && op.kind != directEnd {
			return fv.verr(ErrConstExprRequired, "structured instruction")
		}
		if err := fv.stepDirectOp(op); err != nil {
			return err
		}
	}
}

func (v *moduleValidator) validateDirectElem(e directElem) error {
	elemRef, err := v.validateDirectElemPayload(e)
	if err != nil {
		return err
	}
	if e.modeKind == ElemActive {
		tt, ok := v.tableType(uint32(e.table))
		if !ok {
			return v.err(ErrUnknownTable, "elem")
		}
		want := I32
		if tt.Limits.Addr64 {
			want = I64
		}
		if err := v.validateConstExprDirect(e.offset, want); err != nil {
			return err
		}
		if !v.refSubtype(elemRef, tt.Ref) {
			return v.err(ErrTypeMismatch, "element type does not match table")
		}
	}
	return nil
}

func (v *moduleValidator) validateDirectElemPayload(e directElem) (RefType, error) {
	switch e.kind {
	case ElemFuncs:
		if e.hasFuncs && int(e.maxFunc) >= v.m.FuncCount() {
			return RefType{}, v.err(ErrUnknownFunc, "elem")
		}
		return FuncRef.Ref, nil
	case ElemFuncExprs:
		for _, ex := range e.exprs {
			if err := v.validateConstExprDirect(ex, FuncRef); err != nil {
				return RefType{}, err
			}
		}
		return FuncRef.Ref, nil
	case ElemTypedExprs:
		if err := v.validateRefType(e.ref); err != nil {
			return RefType{}, err
		}
		for _, ex := range e.exprs {
			if err := v.validateConstExprDirect(ex, RefVal(e.ref)); err != nil {
				return RefType{}, err
			}
		}
		return e.ref, nil
	default:
		return RefType{}, v.err(ErrTypeMismatch, "unknown element kind")
	}
}

func (v *funcValidator) validateFuncDirect(body directCodeBody, ft *CompType) error {
	v.localParams = ft.Params
	v.localRuns = body.locals.Runs
	var overflow bool
	v.localCount, overflow = LocalCount(ft.Params, body.locals.Runs)
	if overflow {
		return v.verr(ErrInvalidLimitRange, "local count overflow")
	}
	for _, run := range body.locals.Runs {
		if err := v.validateValType(run.Type); err != nil {
			return err
		}
	}
	v.pushCtrl(ctrlFunc, nil, ft.Results)
	r := newReader(body.body)
	for {
		if len(v.ctrls) == 0 {
			if r.has() {
				return &DecodeError{Code: ErrSectionSizeMismatch, Offset: r.off()}
			}
			return nil
		}
		op, err := decodeDirectOp(r)
		if err != nil {
			return err
		}
		if err := v.stepDirectOp(op); err != nil {
			return err
		}
	}
}

type directOpKind uint8

const (
	directInstr directOpKind = iota
	directBlock
	directLoop
	directIf
	directTryTable
	directElse
	directEnd
)

type directOp struct {
	kind      directOpKind
	instr     Instruction
	blockType BlockType
	catches   []Catch
}

func decodeDirectOp(r *reader) (directOp, error) {
	op, err := r.byte()
	if err != nil {
		return directOp{}, err
	}
	if k := simpleOpcode[op]; k != InstrInvalid {
		return directOp{kind: directInstr, instr: Instruction{Kind: k}}, nil
	}
	switch op {
	case 0x02, 0x03, 0x04:
		bt, err := decodeBlockType(r)
		if err != nil {
			return directOp{}, err
		}
		k := directBlock
		if op == 0x03 {
			k = directLoop
		} else if op == 0x04 {
			k = directIf
		}
		return directOp{kind: k, blockType: bt}, nil
	case 0x05:
		return directOp{kind: directElse}, nil
	case 0x08:
		in, err := indexInst(r, InstrThrow)
		return directOp{kind: directInstr, instr: in}, err
	case 0x0b:
		return directOp{kind: directEnd}, nil
	case 0x0c:
		in, err := indexInst(r, InstrBr)
		return directOp{kind: directInstr, instr: in}, err
	case 0x0d:
		in, err := indexInst(r, InstrBrIf)
		return directOp{kind: directInstr, instr: in}, err
	case 0x0e:
		labels, err := readVec(r, func(r *reader) (uint32, error) { return r.u32() })
		if err != nil {
			return directOp{}, err
		}
		def, err := r.u32()
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrBrTable, Index: def, ext: &instrExt{Indices: labels}}}, err
	case 0x10:
		in, err := indexInst(r, InstrCall)
		return directOp{kind: directInstr, instr: in}, err
	case 0x11:
		in, err := twoIndexInst(r, InstrCallIndirect)
		return directOp{kind: directInstr, instr: in}, err
	case 0x12:
		in, err := indexInst(r, InstrReturnCall)
		return directOp{kind: directInstr, instr: in}, err
	case 0x13:
		in, err := twoIndexInst(r, InstrReturnCallIndirect)
		return directOp{kind: directInstr, instr: in}, err
	case 0x14:
		in, err := indexInst(r, InstrCallRef)
		return directOp{kind: directInstr, instr: in}, err
	case 0x15:
		in, err := indexInst(r, InstrReturnCallRef)
		return directOp{kind: directInstr, instr: in}, err
	case 0x1c:
		vts, err := decodeResultType(r)
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: vts}}}, err
	case 0x1f:
		bt, err := decodeBlockType(r)
		if err != nil {
			return directOp{}, err
		}
		catches, err := readVec(r, decodeCatch)
		if err != nil {
			return directOp{}, err
		}
		return directOp{kind: directTryTable, blockType: bt, catches: catches}, nil
	case 0x20:
		in, err := indexInst(r, InstrLocalGet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x21:
		in, err := indexInst(r, InstrLocalSet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x22:
		in, err := indexInst(r, InstrLocalTee)
		return directOp{kind: directInstr, instr: in}, err
	case 0x23:
		in, err := indexInst(r, InstrGlobalGet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x24:
		in, err := indexInst(r, InstrGlobalSet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x25:
		in, err := indexInst(r, InstrTableGet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x26:
		in, err := indexInst(r, InstrTableSet)
		return directOp{kind: directInstr, instr: in}, err
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
		ma, err := decodeMemArg(r)
		return directOp{kind: directInstr, instr: Instruction{Kind: memOpcodeKind[op], ext: &instrExt{MemArg: ma}}}, err
	case 0x3f:
		in, err := memidxInst(r, InstrMemorySize)
		return directOp{kind: directInstr, instr: in}, err
	case 0x40:
		in, err := memidxInst(r, InstrMemoryGrow)
		return directOp{kind: directInstr, instr: in}, err
	case 0x41:
		x, err := r.i32()
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrI32Const, I32: x}}, err
	case 0x42:
		x, err := r.i64()
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrI64Const, I64: x}}, err
	case 0x43:
		x, err := r.le32()
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrF32Const, F32Bits: x}}, err
	case 0x44:
		x, err := r.le64()
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrF64Const, F64Bits: x}}, err
	case 0xd0:
		rt, err := decodeRefTypeForNull(r)
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: rt}}}, err
	case 0xd2:
		in, err := indexInst(r, InstrRefFunc)
		return directOp{kind: directInstr, instr: in}, err
	case 0xd3:
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrRefEq}}, nil
	case 0xd4:
		return directOp{kind: directInstr, instr: Instruction{Kind: InstrRefAsNonNull}}, nil
	case 0xd5:
		in, err := indexInst(r, InstrBrOnNull)
		return directOp{kind: directInstr, instr: in}, err
	case 0xd6:
		in, err := indexInst(r, InstrBrOnNonNull)
		return directOp{kind: directInstr, instr: in}, err
	case 0xfb:
		in, err := decodeFB(r)
		return directOp{kind: directInstr, instr: in}, err
	case 0xfc:
		in, err := decodeFC(r)
		return directOp{kind: directInstr, instr: in}, err
	case 0xfd:
		in, err := decodeFD(r)
		return directOp{kind: directInstr, instr: in}, err
	case 0xfe:
		in, err := decodeFE(r)
		return directOp{kind: directInstr, instr: in}, err
	default:
		return directOp{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}

func (v *funcValidator) stepDirectOp(op directOp) error {
	switch op.kind {
	case directInstr:
		return v.step(op.instr)
	case directBlock:
		ins, outs, err := v.blockSig(op.blockType)
		if err != nil {
			return err
		}
		return v.directPushCtrl(ctrlBlock, ins, outs)
	case directLoop:
		ins, outs, err := v.blockSig(op.blockType)
		if err != nil {
			return err
		}
		return v.directPushCtrl(ctrlLoop, ins, outs)
	case directIf:
		return v.directStartIf(op.blockType)
	case directTryTable:
		return v.directStartTryTable(op.blockType, op.catches)
	case directElse:
		return v.directElse()
	case directEnd:
		return v.directEnd()
	default:
		return v.verr(ErrUnsupportedValidationOpcode, "direct")
	}
}

func (v *funcValidator) directPushCtrl(k ctrlKind, in, out []ValType) error {
	if len(v.ctrls) >= maxInstructionNestingDepth {
		return &DecodeError{Code: ErrInstructionNestingLimitExceeded}
	}
	return v.pushCtrl(k, in, out)
}

func (v *funcValidator) directStartIf(bt BlockType) error {
	if err := v.popExpect(I32); err != nil {
		return err
	}
	ins, outs, err := v.blockSig(bt)
	if err != nil {
		return err
	}
	baseVals := append([]val(nil), v.vals...)
	baseCtrls := append([]ctrlFrame(nil), v.ctrls...)
	if err := v.directPushCtrl(ctrlIf, ins, outs); err != nil {
		return err
	}
	f := v.top()
	f.ifBaseVals = baseVals
	f.ifBaseCtrls = baseCtrls
	return nil
}

func (v *funcValidator) directStartTryTable(bt BlockType, catches []Catch) error {
	ins, outs, err := v.blockSig(bt)
	if err != nil {
		return err
	}
	for _, c := range catches {
		lt, err := v.label(uint32(c.Label))
		if err != nil {
			return err
		}
		var payload []ValType
		if c.Kind == CatchTag || c.Kind == CatchRef {
			if int(c.Tag) >= v.m.TagCount() {
				return v.verr(ErrUnknownTag, "catch")
			}
			ft, ok := v.tagFuncType(uint32(c.Tag))
			if !ok {
				return v.verr(ErrUnknownTag, "catch")
			}
			payload = append(payload, ft.Params...)
		}
		if c.Kind == CatchRef || c.Kind == CatchAllRef {
			payload = append(payload, RefVal(AbsRef(HeapExn)))
		}
		if c.Kind == CatchAll && len(lt) != 0 {
			return v.verr(ErrTypeMismatch, "catch_all label must expect no values")
		}
		if len(payload) != len(lt) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
		for i := range payload {
			if !v.subtype(payload[i], lt[i]) {
				return v.verr(ErrTypeMismatch, "catch payload label mismatch")
			}
		}
	}
	return v.directPushCtrl(ctrlBlock, ins, outs)
}

func (v *funcValidator) directElse() error {
	if len(v.ctrls) == 0 || v.top().kind != ctrlIf || v.top().ifSeenElse {
		return &DecodeError{Code: ErrInvalidInstruction}
	}
	f := *v.top()
	if _, err := v.popCtrl(); err != nil {
		return err
	}
	thenVals := append([]val(nil), v.vals...)
	v.vals = f.ifBaseVals
	v.ctrls = f.ifBaseCtrls
	if err := v.directPushCtrl(ctrlIf, f.in, f.out); err != nil {
		return err
	}
	nf := v.top()
	nf.ifSeenElse = true
	nf.ifThenVals = thenVals
	return nil
}

func (v *funcValidator) directEnd() error {
	if len(v.ctrls) == 0 {
		return &DecodeError{Code: ErrInvalidInstruction}
	}
	f := *v.top()
	if _, err := v.popCtrl(); err != nil {
		return err
	}
	if f.kind == ctrlIf {
		if f.ifSeenElse {
			if len(v.vals) != len(f.ifThenVals) {
				return v.verr(ErrTypeMismatch, "if branch heights")
			}
		} else if !sameValTypes(f.in, f.out) {
			return v.verr(ErrTypeMismatch, "if without else")
		}
	}
	return nil
}
