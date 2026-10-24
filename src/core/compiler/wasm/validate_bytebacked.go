package wasm

import "runtime"

// directCodeBody is the part of a code-section function body the byte-backed
// validator keeps: compact local runs plus raw expression bytes. This matches
// DecodeModule's no-body instruction representation for decoded function bodies.
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
}

type directModule struct {
	compactData        bool
	m                  Module
	direct             directValidationEnv
	seenName           bool
	seenBranchHints    bool
	seenCode           bool
	usesDataCountInstr bool
}

// DecodedByteBackedModule is a WebAssembly module decoded without materializing
// structured function-body Expr/Instruction trees. Module contains compact
// section metadata plus raw function BodyBytes; the unexported validation state
// keeps remaining const-expression summaries for ValidateDecodedByteBackedModule.
// Active data offsets are byte-backed expressions projected through DataSegment.
// Ordinary decoding also exposes the expanded Module.Data records.
type DecodedByteBackedModule struct {
	Module *Module
	direct directValidationEnv
}

// ValidateByteBackedModule validates data through the byte-backed no-body decode
// path. It is a convenience for benchmarks and internal tests that need a
// single call around explicit decode then validate phases.
func ValidateByteBackedModule(data []byte) error {
	return validateByteBackedModule(data, 1, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateByteBackedModuleWithWorkers is ValidateByteBackedModule with bounded
// parallel function-body validation. workers <= 1 retains serial behavior.
func ValidateByteBackedModuleWithWorkers(data []byte, workers int) error {
	return validateByteBackedModule(data, workers, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateByteBackedModuleWithFeatures is the explicit-feature variant of
// ValidateByteBackedModule.
func ValidateByteBackedModuleWithFeatures(data []byte, features ValidationFeatures) error {
	return validateByteBackedModule(data, 1, features, defaultValidationLimits)
}

// ValidateByteBackedModuleWithConfig is the explicit feature, worker, and
// resource-limit variant of ValidateByteBackedModule.
func ValidateByteBackedModuleWithConfig(data []byte, features ValidationFeatures, workers int, limits ValidationLimits) error {
	return validateByteBackedModule(data, workers, features, limits)
}

func validateByteBackedModule(data []byte, workers int, features ValidationFeatures, limits ValidationLimits) error {
	dm, err := DecodeModuleByteBackedWithFeatures(data, features)
	if err != nil {
		return err
	}
	return validateDecodedByteBackedModule(dm, workers, features, limits)
}

// DecodeModuleByteBacked decodes data without materializing the structured
// Expr/Instruction tree for function bodies. Function Code entries carry Locals
// and BodyBytes, while Body is left empty. Call ValidateDecodedByteBackedModule
// before handing the module to lowering or execution paths.
func DecodeModuleByteBacked(data []byte) (*DecodedByteBackedModule, error) {
	return DecodeModuleByteBackedWithFeatures(data, ValidationFeatures{MultiMemory: true})
}

// DecodeModuleByteBackedWithFeatures selects wire grammar from explicit features.
func DecodeModuleByteBackedWithFeatures(data []byte, features ValidationFeatures) (*DecodedByteBackedModule, error) {
	return DecodeModuleByteBackedWithLimits(data, features, DecodeLimits{})
}

func DecodeModuleForValidation(data []byte, features ValidationFeatures) (*Module, error) {
	return DecodeModuleForValidationWithFeatures(data, features)
}

// DecodeModuleByteBackedWithLimits selects wire features and metadata limits.
func DecodeModuleByteBackedWithLimits(data []byte, features ValidationFeatures, limits DecodeLimits) (*DecodedByteBackedModule, error) {
	dm, err := decodeDirectModuleLimited(data, features, limits)
	if err != nil {
		return nil, err
	}
	dm.populateCodeBodies()
	if err := validateBranchHints(&dm.m); err != nil {
		return nil, err
	}
	return &DecodedByteBackedModule{Module: &dm.m, direct: dm.direct}, nil
}

// ValidateDecodedByteBackedModule validates a module produced by
// DecodeModuleByteBacked without requiring a structured function-body
// instruction tree.
func ValidateDecodedByteBackedModule(dm *DecodedByteBackedModule) error {
	return validateDecodedByteBackedModule(dm, 1, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateDecodedByteBackedModuleWithWorkers is
// ValidateDecodedByteBackedModule with bounded parallel function-body
// validation. Errors remain ordered by function index.
func ValidateDecodedByteBackedModuleWithWorkers(dm *DecodedByteBackedModule, workers int) error {
	return validateDecodedByteBackedModule(dm, workers, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateDecodedByteBackedModuleWithFeatures validates a decoded compact module
// under explicitly staged release features.
func ValidateDecodedByteBackedModuleWithFeatures(dm *DecodedByteBackedModule, features ValidationFeatures) error {
	return validateDecodedByteBackedModule(dm, 1, features, defaultValidationLimits)
}

// ValidateDecodedByteBackedModuleWithConfig validates decoded byte-backed
// metadata with explicit feature, worker, and resource-limit policy.
func ValidateDecodedByteBackedModuleWithConfig(dm *DecodedByteBackedModule, features ValidationFeatures, workers int, limits ValidationLimits) error {
	return validateDecodedByteBackedModule(dm, workers, features, limits)
}

func validateDecodedByteBackedModule(dm *DecodedByteBackedModule, workers int, features ValidationFeatures, limits ValidationLimits) error {
	if dm == nil || dm.Module == nil {
		return &ValidationError{Code: ErrTypeMismatch, Func: -1, Detail: "nil byte-backed module"}
	}
	return validateModuleWithWorkersFeaturesAndLimits(dm.Module, &dm.direct, workers, features, limits)
}

func (dm *directModule) populateCodeBodies() {
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
}

func directExpr(e directConstExpr) Expr {
	return Expr{BodyBytes: e.body}
}

func decodeDirectModuleLimited(data []byte, features ValidationFeatures, limits DecodeLimits) (*directModule, error) {
	dm, err := decodeDirectModuleInnerLimits(data, features, limits)
	runtime.KeepAlive(data)
	return dm, err
}

func decodeDirectModuleInnerLimits(data []byte, features ValidationFeatures, limits DecodeLimits) (*directModule, error) {
	return decodeDirectModuleInnerLimitsMode(data, features, limits, false, false)
}

func (dm *directModule) decodeDirectCustomSection(r *reader) error {
	if err := r.reserve(1, 512); err != nil {
		return err
	}
	name, err := r.name()
	if err != nil {
		return err
	}
	// Every custom payload retains one owned byte copy. Structured decoders
	// separately reserve their exact containers through the same parent budget.
	if err := r.reserve(uint64(r.left()), 2); err != nil {
		return err
	}
	payload, err := r.bytes(r.left())
	if err != nil {
		return err
	}
	firstName := name == "name" && !dm.seenName
	if firstName {
		ns, err := decodeOptionalNameSec(payload, r.budget)
		if err != nil {
			return err
		}
		dm.m.NameSec = ns
		dm.seenName = true
	}
	if name == branchHintSectionName {
		if dm.seenBranchHints || dm.seenCode {
			return newDecodeError(ErrInvalidSection, r.off())
		}
		hints, err := decodeBranchHintSectionWithBudget(payload, r.budget)
		if err != nil {
			return rebaseDecodeError(err, r.off()-len(payload))
		}
		dm.m.BranchHints = hints
		dm.seenBranchHints = true
	}
	ownedPayload := append([]byte(nil), payload...)
	if firstName {
		dm.m.RawNameSecPayload = ownedPayload
	}
	dm.m.Customs = append(dm.m.Customs, CustomSec{Name: name, Data: ownedPayload})
	return nil
}

// Nested readers report offsets within their own byte slice. Move a decode
// error to the containing section before the outer decoder adds the file base.
func rebaseDecodeError(err error, base int) error {
	if de, ok := err.(*DecodeError); ok {
		de.Offset += base
	}
	return err
}

func decodeDirectTableSection(dm *directModule, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	if err := reserveDecodedSlice[Table](r, n); err != nil {
		return err
	}
	capHint := boundedVecCap(n, r.left())
	dm.m.Tables = make([]Table, 0, capHint)
	dm.direct.tableHasInit = make([]bool, 0, capHint)
	dm.direct.tableInits = make([]directConstExpr, 0, capHint)
	for i := uint32(0); i < n; i++ {
		if b, ok := r.peek(); ok && b == 0x40 {
			_, _ = r.byte()
			if b2, ok := r.peek(); !ok || b2 != 0x00 {
				return newDecodeError(ErrInvalidType, r.off())
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
	if err := reserveDecodedSlice[Global](r, n); err != nil {
		return err
	}
	capHint := boundedVecCap(n, r.left())
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
	// Preserve the expanded decoder's conservative budget and rejection order.
	if err := reserveDecodedSlice[Data](r, n); err != nil {
		return err
	}
	if uint64(n) > uint64(r.left()/2) {
		return newDecodeError(ErrIndexOutOfBounds, r.off())
	}
	if dm.compactData && n != 0 {
		view := &dataSectionView{bytes: r.data, spans: make([]dataSegmentSpan, n)}
		dm.m.dataView = view
		for i := range view.spans {
			span, err := decodeDirectDataSpan(r)
			if err != nil {
				return err
			}
			view.spans[i] = span
		}
		return nil
	}
	dm.m.Data = make([]Data, n)
	for i := range dm.m.Data {
		d, off, err := decodeDirectData(r)
		if err != nil {
			return err
		}
		if d.Mode.Kind == DataActive {
			d.Mode.Offset = directExpr(off)
		}
		dm.m.Data[i] = d
	}
	return nil
}

func decodeDirectDataSpan(r *reader) (dataSegmentSpan, error) {
	var s dataSegmentSpan
	flags, err := r.u32()
	if err != nil {
		return s, err
	}
	switch flags {
	case 0, 2:
		if flags == 2 {
			s.memory, err = r.u32()
			if err != nil {
				return s, err
			}
		}
		s.offsetStart = uint32(r.off())
		if _, err = readDirectConstExprBytes(r); err != nil {
			return s, err
		}
		s.offsetEnd = uint32(r.off())
	case 1:
	default:
		return s, newDecodeError(ErrInvalidSection, r.off())
	}
	n, err := r.u32()
	if err != nil {
		return s, err
	}
	s.initStart = uint32(r.off())
	if _, err = r.bytes(int(n)); err != nil {
		return s, err
	}
	s.initEnd = uint32(r.off())
	return s, nil
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
		return d, off, newDecodeError(ErrInvalidSection, r.off())
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
	if err := reserveDecodedSlice[Elem](r, n); err != nil {
		return err
	}
	capHint := boundedVecCap(n, r.left())
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

func readElemKind(r *reader) error {
	b, err := r.byte()
	if err != nil {
		return err
	}
	if b != 0 {
		return newDecodeError(ErrInvalidType, r.off()-1)
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
		return e, de, newDecodeError(ErrInvalidSection, r.off())
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
	if err := reserveDecodedSlice[FuncIdx](r, n); err != nil {
		return err
	}
	capHint := boundedVecCap(n, r.left())
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
	if err := reserveDecodedSlice[Expr](r, n); err != nil {
		return nil, err
	}
	capHint := boundedVecCap(n, r.left())
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
			return directConstExpr{}, newDecodeError(ErrSectionSizeMismatch, r.off())
		}
		op, err := skipExprOp(r)
		if err != nil {
			return directConstExpr{}, err
		}
		switch op {
		case directBlock, directLoop, directIf, directTryTable:
			depth++
			if depth > maxInstructionNestingDepth {
				return directConstExpr{}, newDecodeError(ErrInstructionNestingLimitExceeded, r.off())
			}
		case directEnd:
			if depth == 0 {
				return directConstExpr{body: r.data[start:r.off()]}, nil
			}
			depth--
		}
	}
}

func decodeDirectCodeSection(r *reader, memarg64, multiMemory bool) ([]Func, bool, error) {
	return decodeDirectCodeSectionWithWidths(r, fixedMemargWidths(memarg64), multiMemory)
}

func decodeDirectCodeSectionWithModule(r *reader, m *Module, multiMemory bool) ([]Func, bool, error) {
	return decodeDirectCodeSectionWithWidths(r, moduleMemargWidths(m), multiMemory)
}

func decodeDirectCodeSectionWithWidths(r *reader, widths memargWidths, multiMemory bool) ([]Func, bool, error) {
	return decodeDirectCodeSectionBodyPolicy(r, widths, multiMemory, false)
}

func decodeDirectCodeSectionBodyPolicy(r *reader, widths memargWidths, multiMemory, deferBodies bool) ([]Func, bool, error) {
	n, err := r.u32()
	if err != nil {
		return nil, false, err
	}
	if err := reserveDecodedSlice[Func](r, n); err != nil {
		return nil, false, err
	}
	capHint := boundedVecCap(n, r.left())
	out := make([]Func, 0, capHint)
	sub := reader{budget: r.budget}
	if err := r.reserve(uint64(min(r.left(), maxInstructionNestingDepth)), 32); err != nil {
		return nil, false, err
	}
	var frames []exprSkipFrame
	usesDataCountInstr := false
	for i := uint32(0); i < n; i++ {
		size, err := r.u32()
		if err != nil {
			return nil, false, err
		}
		body, err := r.bytes(int(size))
		if err != nil {
			return nil, false, err
		}
		bodyStart := r.off() - len(body)
		sub.reset(body)
		locals, err := decodeLocals(&sub)
		if err != nil {
			return nil, false, rebaseDecodeError(err, bodyStart)
		}
		var exprBytes []byte
		var bodyUsesDataCount bool
		if deferBodies {
			// The body length and local declarations are already bounded. Keep
			// a nonempty raw body so validation cannot mistake it for an AST
			// function with an implicit empty expression.
			if !sub.has() {
				return nil, false, newDecodeError(ErrSectionSizeMismatch, bodyStart+sub.off())
			}
			exprBytes = sub.data[sub.pos:]
			sub.pos = len(sub.data)
		} else {
			exprBytes, frames, bodyUsesDataCount, err = readDirectFuncExprBytes(&sub, frames, widths, multiMemory)
		}
		if err != nil {
			return nil, false, rebaseDecodeError(err, bodyStart)
		}
		usesDataCountInstr = usesDataCountInstr || bodyUsesDataCount
		if sub.has() {
			return nil, false, newDecodeError(ErrSectionSizeMismatch, bodyStart+sub.off())
		}
		out = append(out, Func{Locals: locals, LocalDeclBytes: uint32(sub.off() - len(exprBytes)), BodyBytes: exprBytes})
	}
	return out, usesDataCountInstr, nil
}

type exprSkipFrame struct {
	kind     directOpKind
	seenElse bool
}

// readDirectFuncExprBytes returns the raw expression bytes of one function body
// and the (possibly grown) frame buffer so the caller can reuse it across every
// function in the code section instead of allocating a nesting stack per body.
func readDirectFuncExprBytes(r *reader, stack []exprSkipFrame, widths memargWidths, multiMemory bool) ([]byte, []exprSkipFrame, bool, error) {
	start := r.off()
	stack = stack[:0]
	usesDataCountInstr := false
	var imm InstructionImmediate
	for {
		if !r.has() {
			return nil, stack, false, newDecodeError(ErrSectionSizeMismatch, r.off())
		}
		opcode, err := r.byte()
		if err != nil {
			return nil, stack, false, err
		}
		imm = InstructionImmediate{}
		op, err := classifyExprOpAfterOpcodeWithWidths(r, opcode, &imm, widths, multiMemory)
		if err != nil {
			return nil, stack, false, err
		}
		if imm.Kind == InstrMemoryInit || imm.Kind == InstrDataDrop || imm.Kind == InstrArrayNewData || imm.Kind == InstrArrayInitData {
			usesDataCountInstr = true
		}
		switch op {
		case directBlock, directLoop, directIf, directTryTable:
			if len(stack) >= maxInstructionNestingDepth {
				return nil, stack, false, newDecodeError(ErrInstructionNestingLimitExceeded, r.off())
			}
			stack = append(stack, exprSkipFrame{kind: op})
		case directElse:
			if len(stack) == 0 {
				return nil, stack, false, newDecodeError(ErrInvalidInstruction, r.off()-1)
			}
			top := &stack[len(stack)-1]
			if top.kind != directIf || top.seenElse {
				return nil, stack, false, newDecodeError(ErrInvalidInstruction, r.off()-1)
			}
			top.seenElse = true
		case directEnd:
			if len(stack) == 0 {
				return r.data[start:r.off()], stack, usesDataCountInstr, nil
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func (v *moduleValidator) validateConstExprDirect(e directConstExpr, want ValType) error {
	return v.validateConstExprDirectWithGlobalLimit(e, want, len(v.importsOfKind(ExternGlobal))+len(v.m.Globals))
}

func (v *moduleValidator) validateConstExprDirectWithGlobalLimit(e directConstExpr, want ValType, globalLimit int) error {
	fv := v.constFV
	if fv == nil {
		fv = &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true}
		v.constFV = fv
	}
	fv.constGlobalLimit = globalLimit
	fv.resetStacks()
	fv.constResult[0] = want
	fv.pushCtrl(ctrlFunc, nil, fv.constResult[:])
	fv.rd.reset(e.body)
	r := &fv.rd
	var op directOp // reused across the loop; decodeDirectOp overwrites it each step
	for {
		if len(fv.ctrls) == 0 {
			if r.has() {
				return newDecodeError(ErrSectionSizeMismatch, r.off())
			}
			return nil
		}
		if err := fv.decodeDirectOp(r, fixedMemargWidths(false), false, &op); err != nil {
			return err
		}
		if op.kind != directInstr && op.kind != directEnd {
			return fv.verr(ErrConstExprRequired, "structured instruction")
		}
		if err := fv.stepDirectOp(&op); err != nil {
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
		if e.hasFuncs && uint(e.maxFunc) >= uint(len(v.importsOfKind(ExternFunc))+len(v.m.FuncTypes)) {
			return RefType{}, v.err(ErrUnknownFunc, "elem")
		}
		return Ref(false, AbsHeap(HeapFunc), false), nil
	case ElemFuncExprs:
		for _, ex := range e.exprs {
			if err := v.validateConstExprDirect(ex, FuncRef); err != nil {
				return RefType{}, err
			}
		}
		return FuncRef.Ref(), nil
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

// directElemRefType is the byte-backed table.init metadata lookup. Direct
// initializer bytes were already validated serially by validateDirectElem.
func (v *funcValidator) directElemRefType(index uint32) (RefType, error) {
	if uint64(index) >= uint64(len(v.direct.elements)) {
		return RefType{}, v.verr(ErrUnknownTable, "table.init elem")
	}
	e := &v.direct.elements[index]
	switch e.kind {
	case ElemFuncs, ElemFuncExprs:
		return FuncRef.Ref(), nil
	case ElemTypedExprs:
		return e.ref, nil
	default:
		return RefType{}, v.verr(ErrTypeMismatch, "unknown element kind")
	}
}

func (v *funcValidator) validateFuncDirect(body directCodeBody, ft *CompType, widths memargWidths, multiMemory bool, segmentCounts *validationSegmentCounts) error {
	widths.multiMemory = multiMemory
	v.localParams = ft.Params
	v.localRuns = body.locals.Runs
	var overflow bool
	v.localCount, overflow = LocalCount(ft.Params, body.locals.Runs)
	if overflow {
		return v.verr(ErrInvalidLimitRange, "local count overflow")
	}
	if v.localCount > uint64(v.limits.MaxFunctionLocals) {
		return v.verr(ErrInvalidLimitRange, "parameter and local count exceeds configured limit")
	}
	v.prepareLocalLookup()
	for _, run := range body.locals.Runs {
		if err := v.validateValType(run.Type); err != nil {
			return err
		}
	}
	v.resetLocalInitialization()
	v.pushCtrl(ctrlFunc, nil, ft.Results)
	v.rd.reset(body.body)
	r := &v.rd
	var op directOp // reused across the loop; decodeDirectOp overwrites it each step
	facts := v.analysisFacts()
	if facts != nil && v.analysis.hintCurrent != nil {
		return v.validateFuncDirectObservedLoop(r, widths, multiMemory, segmentCounts, facts, v.analysis.hintCurrent)
	}
	if facts != nil {
		for {
			if len(v.ctrls) == 0 {
				if r.has() {
					return newDecodeError(ErrSectionSizeMismatch, r.off())
				}
				return nil
			}
			// Immediate-free scalar operations need neither a decoded instruction
			// payload nor the general control/memory dispatcher.
			if r.pos < len(r.data) {
				opcode := r.data[r.pos]
				if opcode >= 0x20 && opcode <= 0x24 || opcode >= 0x41 && opcode <= 0x44 {
					r.pos++
					kind, err := v.stepDirectScalarImmediate(r, opcode)
					if err != nil {
						return err
					}
					facts.Flags |= validatedFuncFlagsByKind[kind]
					continue
				}
				if opcode >= 0x28 && opcode <= 0x3e {
					r.pos++
					kind := memOpcodeKind[opcode]
					arg, err := decodeMemArgWithWidths(r, widths)
					if err != nil {
						return err
					}
					if err = v.stepDirectMemory(kind, arg); err != nil {
						return err
					}
					facts.Flags |= validatedFuncFlagsByKind[kind]
					continue
				}
				if opcode >= 0x45 && opcode <= 0xc4 {
					r.pos++
					kind := simpleOpcode[opcode]
					if err := v.stepDirectNumeric(kind); err != nil {
						return err
					}
					facts.Flags |= validatedFuncFlagsByKind[kind]
					continue
				}
				if opcode == 0x05 || opcode >= 0x0b && opcode <= 0x0d {
					r.pos++
					kind, err := v.stepDirectControlImmediate(r, opcode)
					if err != nil {
						return err
					}
					if kind != InstrInvalid {
						facts.Flags |= validatedFuncFlagsByKind[kind]
					}
					continue
				}
				if opcode == 0x10 || opcode == 0x12 {
					r.pos++
					kind, err := v.stepDirectCallImmediate(r, opcode)
					if err != nil {
						return err
					}
					facts.Flags |= validatedFuncFlagsByKind[kind]
					continue
				}
			}
			if err := v.decodeDirectOp(r, widths, multiMemory, &op); err != nil {
				return err
			}
			if err := v.stepDirectOp(&op); err != nil {
				return err
			}
			if op.kind == directInstr {
				kind := op.instr.Kind
				facts.Flags |= validatedFuncFlagsByKind[kind]
				if validatedFuncNeedsPayloadByKind[kind] {
					v.observeValidatedInstructionPayload(facts, &op.instr, segmentCounts)
				}
			} else {
				facts.observeStructuredDirect(&op)
			}
		}
	}
	for {
		if len(v.ctrls) == 0 {
			if r.has() {
				return newDecodeError(ErrSectionSizeMismatch, r.off())
			}
			return nil
		}
		if r.pos < len(r.data) {
			opcode := r.data[r.pos]
			if opcode >= 0x20 && opcode <= 0x24 || opcode >= 0x41 && opcode <= 0x44 {
				r.pos++
				if _, err := v.stepDirectScalarImmediate(r, opcode); err != nil {
					return err
				}
				continue
			}
			if opcode >= 0x28 && opcode <= 0x3e {
				r.pos++
				arg, err := decodeMemArgWithWidths(r, widths)
				if err != nil {
					return err
				}
				if err = v.stepDirectMemory(memOpcodeKind[opcode], arg); err != nil {
					return err
				}
				continue
			}
			if opcode >= 0x45 && opcode <= 0xc4 {
				r.pos++
				if err := v.stepDirectNumeric(simpleOpcode[opcode]); err != nil {
					return err
				}
				continue
			}
			if opcode == 0x05 || opcode >= 0x0b && opcode <= 0x0d {
				r.pos++
				if _, err := v.stepDirectControlImmediate(r, opcode); err != nil {
					return err
				}
				continue
			}
			if opcode == 0x10 || opcode == 0x12 {
				r.pos++
				if _, err := v.stepDirectCallImmediate(r, opcode); err != nil {
					return err
				}
				continue
			}
		}
		if err := v.decodeDirectOp(r, widths, multiMemory, &op); err != nil {
			return err
		}
		if err := v.stepDirectOp(&op); err != nil {
			return err
		}
	}
}

func segmentStateCount(index uint32) uint32 {
	if index == ^uint32(0) {
		return index
	}
	return index + 1
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

func (v *funcValidator) decodeDirectOp(r *reader, widths memargWidths, multiMemory bool, out *directOp) error {
	widths.multiMemory = multiMemory
	op, err := r.byte()
	if err != nil {
		*out = directOp{}
		return err
	}
	if k := simpleOpcode[op]; k != InstrInvalid {
		*out = directOp{kind: directInstr, instr: Instruction{Kind: k}}
		return nil
	}
	switch op {
	case 0x08, 0x0c, 0x0d, 0x10, 0x12, 0x14, 0x15, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
		in, err := indexInst(r, oneIndexImmediateKind(op))
		*out = directOp{kind: directInstr, instr: in}
		return err

	case 0x02, 0x03, 0x04:
		bt, err := decodeBlockType(r)
		if err != nil {
			*out = directOp{}
			return err
		}
		k := directBlock
		if op == 0x03 {
			k = directLoop
		} else if op == 0x04 {
			k = directIf
		}
		*out = directOp{kind: k, blockType: bt}
		return nil
	case 0x05:
		*out = directOp{kind: directElse}
		return nil
	case 0x0b:
		*out = directOp{kind: directEnd}
		return nil
	case 0x0e:
		labels, err := readVec(r, func(r *reader) (uint32, error) { return r.u32() })
		if err != nil {
			*out = directOp{}
			return err
		}
		def, err := r.u32()
		v.opExt = instrExt{Indices: labels}
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrBrTable, Index: def, ext: &v.opExt}}
		return err
	case 0x11:
		in, err := twoIndexInst(r, InstrCallIndirect)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0x13:
		in, err := twoIndexInst(r, InstrReturnCallIndirect)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0x1c:
		vts, err := decodeResultType(r)
		v.opExt = instrExt{ValTypes: vts}
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrSelect, ext: &v.opExt}}
		return err
	case 0x1f:
		bt, err := decodeBlockType(r)
		if err != nil {
			*out = directOp{}
			return err
		}
		catches, err := readVec(r, decodeCatch)
		if err != nil {
			*out = directOp{}
			return err
		}
		*out = directOp{kind: directTryTable, blockType: bt, catches: catches}
		return nil
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
		ma, err := decodeMemArgWithWidths(r, widths)
		v.opExt = instrExt{MemArg: ma}
		*out = directOp{kind: directInstr, instr: Instruction{Kind: memOpcodeKind[op], ext: &v.opExt}}
		return err
	case 0x3f, 0x40:
		kind := InstrMemorySize
		if op == 0x40 {
			kind = InstrMemoryGrow
		}
		if multiMemory {
			in, err := indexInst(r, kind)
			*out = directOp{kind: directInstr, instr: in}
			return err
		}
		in, err := reservedZeroInst(r, kind)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0x41:
		x, err := r.i32()
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrI32Const, I32: x}}
		return err
	case 0x42:
		x, err := r.i64()
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrI64Const, I64: x}}
		return err
	case 0x43:
		x, err := r.le32()
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrF32Const, F32Bits: x}}
		return err
	case 0x44:
		x, err := r.le64()
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrF64Const, F64Bits: x}}
		return err
	case 0xd0:
		rt, err := decodeRefTypeForNull(r)
		v.opExt = instrExt{RefType: rt}
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrRefNull, ext: &v.opExt}}
		return err
	case 0xd3:
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrRefEq}}
		return nil
	case 0xd4:
		*out = directOp{kind: directInstr, instr: Instruction{Kind: InstrRefAsNonNull}}
		return nil
	case 0xfb:
		in, err := decodeFB(r)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0xfc:
		in, err := decodeFCWithMultiMemory(r, multiMemory)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0xfd:
		in, err := decodeFDWithMemargWidthsInto(r, widths, &v.opExt)
		*out = directOp{kind: directInstr, instr: in}
		return err
	case 0xfe:
		in, err := decodeFEWithMemargWidths(r, widths)
		*out = directOp{kind: directInstr, instr: in}
		return err
	default:
		*out = directOp{}
		return newDecodeError(ErrInvalidInstruction, r.off()-1)
	}
}

// stepDirectOp validates one decoded direct op. op is taken by pointer: directOp
// embeds a ~56-byte Instruction, so a value parameter here is a per-opcode
// duffcopy on the validator's hot path.
func (v *funcValidator) stepDirectOp(op *directOp) error {
	switch op.kind {
	case directInstr:
		return v.step(&op.instr)
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
	// Validate the immediate before the operand stack so an out-of-scope type
	// index is reported deterministically even when the condition is absent.
	ins, outs, err := v.blockSig(bt)
	if err != nil {
		return err
	}
	if err := v.popExpect(I32); err != nil {
		return err
	}
	return v.directPushCtrl(ctrlIf, ins, outs)
}

func (v *funcValidator) directStartTryTable(bt BlockType, catches []Catch) error {
	ins, outs, err := v.blockSig(bt)
	if err != nil {
		return err
	}
	for _, c := range catches {
		if err := v.validateCatchPayload(c); err != nil {
			return err
		}
	}
	return v.directPushCtrl(ctrlTry, ins, outs)
}

func (v *funcValidator) directElse() error {
	if len(v.ctrls) == 0 || v.top().kind != ctrlIf || v.top().ifSeenElse {
		return &DecodeError{Code: ErrInvalidInstruction}
	}
	// popCtrl verifies the then-arm produced exactly `out` and leaves the operand
	// stack at f.height + out. The region below f.height (everything outside the
	// if) is untouched by the then-arm, so the else-arm is opened by truncating to
	// f.height and re-pushing the block inputs — no whole-stack snapshot needed.
	f, err := v.popCtrl()
	if err != nil {
		return err
	}
	thenHeight := len(v.vals)
	v.vals = v.vals[:int(f.height)]
	if len(v.ctrls) >= maxInstructionNestingDepth {
		return &DecodeError{Code: ErrInstructionNestingLimitExceeded}
	}
	v.ctrls = append(v.ctrls, ctrlFrame{kind: ctrlIf, in: f.in, out: f.out, height: f.height, initHeight: f.initHeight, ifSeenElse: true, ifThenHeight: uint32(thenHeight)})
	v.pushAll(f.in)
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
			if uint32(len(v.vals)) != f.ifThenHeight {
				return v.verr(ErrTypeMismatch, "if branch heights")
			}
		} else if !v.matchValTypes(f.in, f.out) {
			return v.verr(ErrTypeMismatch, "if without else")
		}
	}
	return nil
}

func (v *funcValidator) validateFuncDirectObservedLoop(r *reader, widths memargWidths, multiMemory bool, segmentCounts *validationSegmentCounts, facts *ValidatedFuncFacts, observer ValidationHintInstructionObserver) error {
	var op directOp
	for {
		start := r.pos
		if len(v.ctrls) == 0 {
			if r.has() {
				return newDecodeError(ErrSectionSizeMismatch, r.off())
			}
			return nil
		}
		if !v.constOnly && r.pos < len(r.data) {
			opcode := r.data[r.pos]
			if opcode >= 0x41 && opcode <= 0x44 {
				r.pos++
				var bits uint64
				var typ ValType
				var kind InstrKind
				var err error
				switch opcode {
				case 0x41:
					var n int32
					n, err = r.i32()
					bits, typ, kind = uint64(int64(n)), I32, InstrI32Const
				case 0x42:
					var n int64
					n, err = r.i64()
					bits, typ, kind = uint64(n), I64, InstrI64Const
				case 0x43:
					var n uint32
					n, err = r.le32()
					bits, typ, kind = uint64(n), F32, InstrF32Const
				case 0x44:
					bits, err = r.le64()
					typ, kind = F64, InstrF64Const
				}
				if err != nil {
					return err
				}
				v.push(typ)
				facts.Flags |= validatedFuncFlagsByKind[kind]
				observer.Observe(opcode, kind, 0, 0, bits, 0, start, r.pos)
				continue
			}
			if opcode >= 0x20 && opcode <= 0x22 {
				r.pos++
				index, err := r.u32()
				if err != nil {
					return err
				}
				kind := oneIndexImmediateKind(opcode)
				if err := v.stepLocalIndex(kind, index); err != nil {
					return err
				}
				facts.Flags |= validatedFuncFlagsByKind[kind]
				observer.Observe(opcode, kind, index, 0, 0, 0, start, r.pos)
				continue
			}
			kind := simpleOpcode[r.data[r.pos]]
			if cat := opEffects[kind].cat; cat >= effUnary && cat <= effConv {
				r.pos++
				if err := v.stepDirectNumeric(kind); err != nil {
					return err
				}
				facts.Flags |= validatedFuncFlagsByKind[kind]
				observer.Observe(r.data[start], kind, 0, 0, 0, 0, start, r.pos)
				continue
			}
		}
		if err := v.decodeDirectOp(r, widths, multiMemory, &op); err != nil {
			return err
		}
		if err := v.stepDirectOp(&op); err != nil {
			return err
		}
		opcode := r.data[start]
		if op.kind != directInstr || opcode >= 0x20 && opcode <= 0x26 {
			observer.Observe(opcode, op.instr.Kind, op.instr.Index, op.instr.Index2, 0, 0, start, r.pos)
		} else {
			observeValidationHint(observer, opcode, start, r.pos, &op.instr)
		}
		if op.kind == directInstr {
			kind := op.instr.Kind
			facts.Flags |= validatedFuncFlagsByKind[kind]
			if validatedFuncNeedsPayloadByKind[kind] {
				v.observeValidatedInstructionPayload(facts, &op.instr, segmentCounts)
			}
		} else {
			facts.observeStructuredDirect(&op)
		}
	}
}

func decodeDirectModuleInnerLimitsMode(data []byte, features ValidationFeatures, limits DecodeLimits, deferCodeBodies, compactData bool) (*directModule, error) {
	// Keep the top-level cursor in this frame. TinyGo's conservative collector
	// can otherwise lose the heap-allocated reader while its backing slice is
	// still being consumed across allocation-heavy section decoding.
	r := reader{budget: newDecodeBudget(limits)}
	r.reset(data)
	magic, err := r.bytes(4)
	if err != nil {
		return nil, err
	}
	if string(magic) != "\x00asm" {
		return nil, newDecodeError(ErrBadMagic, 0)
	}
	ver, err := r.le32()
	if err != nil {
		return nil, err
	}
	if ver != 1 {
		return nil, newDecodeError(ErrBadVersion, 4)
	}
	dm := &directModule{compactData: compactData}
	var lastOrder uint8
	var seen uint16 // standard section IDs are the dense range 1..13
	sub := reader{budget: r.budget}
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
			ord, ok := lookupSectionOrder(id)
			if !ok {
				return nil, &DecodeError{Code: ErrInvalidSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			if ord < lastOrder {
				return nil, &DecodeError{Code: ErrSectionOrder, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			bit := uint16(1) << id
			if seen&bit != 0 {
				return nil, &DecodeError{Code: ErrDuplicateSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			seen |= bit
			lastOrder = ord
		}
		sub.reset(payload)
		switch id {
		case secCustom:
			err = dm.decodeDirectCustomSection(&sub)
		case secTable:
			err = decodeDirectTableSection(dm, &sub)
		case secGlobal:
			err = decodeDirectGlobalSection(dm, &sub)
		case secElement:
			err = decodeDirectElementSection(dm, &sub)
		case secCode:
			if deferCodeBodies {
				dm.m.Code, err = decodePendingCodeSection(&sub)
			} else {
				dm.m.Code, dm.usesDataCountInstr, err = decodeDirectCodeSectionWithModule(&sub, &dm.m, features.MultiMemory)
			}
			dm.seenCode = true
		case secData:
			err = decodeDirectDataSection(dm, &sub)
		default:
			err = decodeSection(&dm.m, &sub, id)
		}
		if err != nil {
			if de, ok := err.(*DecodeError); ok {
				de.SectionID = id
				de.SectionStart = start
				de.SectionEnd = end
				de.Offset += start
				return nil, de
			}
			return nil, err
		}
		if sub.has() {
			return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: start + sub.off(), SectionID: id, SectionStart: start, SectionEnd: end}
		}
	}
	if len(dm.m.FuncTypes) != len(dm.m.Code) {
		return nil, newDecodeError(ErrInvalidModule, len(data))
	}
	if dm.m.DataCount != nil && uint64(*dm.m.DataCount) != uint64(dm.m.DataSegmentCount()) {
		return nil, newDecodeError(ErrInvalidModule, len(data))
	}
	if dm.m.DataCount == nil && dm.usesDataCountInstr {
		return nil, newDecodeError(ErrInvalidModule, len(data))
	}
	return dm, nil
}

// stepDirectNumeric validates an immediate-free scalar stack effect in place.
// The common case replaces its operands with the result without repeatedly
// popping and pushing the same backing slice. The ordinary path retains exact
// error order and polymorphic-stack handling for underflow and mismatched types.
func (v *funcValidator) stepDirectNumeric(kind InstrKind) error {
	effect := opEffects[kind]
	input := effect.a.valType()
	output := input
	operands := 1
	switch effect.cat {
	case effBinary:
		operands = 2
	case effCompare:
		operands = 2
		output = I32
	case effTest:
		output = I32
	case effConv:
		output = effect.b.valType()
	case effUnary:
	default:
		return v.step(&Instruction{Kind: kind})
	}
	n := len(v.vals)
	if !v.constOnly && n-int(v.top().height) >= operands {
		matched := true
		for i := n - operands; i < n; i++ {
			value := v.vals[i]
			if !value.unknown() && value.t != input {
				matched = false
				break
			}
		}
		if matched {
			result := n - operands
			v.vals = v.vals[:result+1]
			v.vals[result] = val{t: output}
			return nil
		}
	}
	return v.step(&Instruction{Kind: kind})
}

// stepDirectScalarImmediate consumes scalar immediates directly into validation
// state. Values of constants are irrelevant to typing, but their complete LEB
// or fixed-width encodings are still checked before pushing the result type.
func (v *funcValidator) stepDirectScalarImmediate(r *reader, opcode byte) (InstrKind, error) {
	var kind InstrKind
	var typ ValType
	var err error
	switch opcode {
	case 0x20, 0x21, 0x22:
		switch opcode {
		case 0x20:
			kind = InstrLocalGet
		case 0x21:
			kind = InstrLocalSet
		case 0x22:
			kind = InstrLocalTee
		}
		index, e := r.u32()
		if e != nil {
			return kind, e
		}
		return kind, v.stepLocalIndex(kind, index)
	case 0x23, 0x24:
		kind = InstrGlobalGet
		if opcode == 0x24 {
			kind = InstrGlobalSet
		}
		index, e := r.u32()
		if e != nil {
			return kind, e
		}
		return kind, v.step(&Instruction{Kind: kind, Index: index})
	case 0x41:
		kind, typ = InstrI32Const, I32
		_, err = r.i32()
	case 0x42:
		kind, typ = InstrI64Const, I64
		_, err = r.i64()
	case 0x43:
		kind, typ = InstrF32Const, F32
		_, err = r.le32()
	case 0x44:
		kind, typ = InstrF64Const, F64
		_, err = r.le64()
	}
	if err == nil {
		v.push(typ)
	}
	return kind, err
}

// stepDirectMemory preserves the ordinary memory/type check order while
// replacing a load's address in place or consuming a store's two operands.
func (v *funcValidator) stepDirectMemory(kind InstrKind, arg MemArg) error {
	effect := opEffects[kind]
	address, err := v.checkMemArg(arg, uint32(effect.align))
	if err != nil {
		return err
	}
	n := len(v.vals)
	height := int(v.top().height)
	valueType := effect.a.valType()
	if effect.cat == effLoad {
		if n > height && (v.vals[n-1].unknown() || v.vals[n-1].t == address) {
			v.vals[n-1] = val{t: valueType}
			return nil
		}
		if err = v.popExpect(address); err != nil {
			return err
		}
		v.push(valueType)
		return nil
	}
	if n-height >= 2 && (v.vals[n-1].unknown() || v.vals[n-1].t == valueType) && (v.vals[n-2].unknown() || v.vals[n-2].t == address) {
		v.vals = v.vals[:n-2]
		return nil
	}
	if err = v.popExpect(valueType); err != nil {
		return err
	}
	return v.popExpect(address)
}

// DecodeModuleForValidationWithFeatures decodes section metadata and bounds each
// code body, leaving its instruction syntax to the mandatory subsequent
// ValidateModule call. Unlike DecodeModule, this is not a standalone syntax
// checker. It is the first half of the compiler's fused decode/validate path.
func DecodeModuleForValidationWithFeatures(data []byte, features ValidationFeatures) (*Module, error) {
	return decodeModuleForValidationDataMode(data, features, false)
}

// DecodeModuleForCompilation retains pointer-free data-section spans instead of
// expanded data records. Function syntax still needs subsequent validation.
// Read data through DataSegmentCount/DataSegment; ordinary
// decoding and programmatically populated Module.Data remain unchanged.
func DecodeModuleForCompilation(data []byte, features ValidationFeatures) (*Module, error) {
	return decodeModuleForValidationDataMode(data, features, true)
}

func decodeModuleForValidationDataMode(data []byte, features ValidationFeatures, compactData bool) (*Module, error) {
	dm, err := decodeDirectModuleInnerLimitsMode(data, features, DecodeLimits{}, true, compactData)
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	dm.populateCodeBodies()
	if err = validateBranchHints(&dm.m); err != nil {
		return nil, err
	}
	return &dm.m, nil
}

func decodePendingCodeSection(r *reader) ([]Func, error) {
	count, err := r.u32()
	if err != nil {
		return nil, err
	}
	if err = reserveDecodedSlice[Func](r, count); err != nil {
		return nil, err
	}
	// Every valid body needs a size byte, a local-vector byte and an end.
	// The metadata budget above bounds the full vector allocation. Reserve it
	// once when the byte count can hold it, avoiding old growth buffers for
	// large modules; retain the conservative path for impossible counts.
	capacity := boundedVecCap(count, r.left())
	if uint64(count) <= uint64(r.left()/3) {
		capacity = int(count)
	}
	out := make([]Func, 0, capacity)
	// Keep the same decode-budget reservation as the syntax-only path, even
	// though the validation control stack replaces its structural scratch.
	if err = r.reserve(uint64(min(r.left(), maxInstructionNestingDepth)), 32); err != nil {
		return nil, err
	}
	sub := reader{budget: r.budget}
	for i := uint32(0); i < count; i++ {
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		body, err := r.bytes(int(size))
		if err != nil {
			return nil, err
		}
		start := r.off() - len(body)
		sub.reset(body)
		locals, err := decodeLocals(&sub)
		if err != nil {
			return nil, rebaseDecodeError(err, start)
		}
		if len(locals.Runs) > 1 {
			locals.Runs = coalesceCompilerLocalRuns(locals.Runs)
		}
		declarations := sub.off()
		if !sub.has() {
			return nil, newDecodeError(ErrSectionSizeMismatch, start+sub.off())
		}
		expression, err := sub.bytes(sub.left())
		if err != nil {
			return nil, err
		}
		out = append(out, Func{Locals: locals, LocalDeclBytes: uint32(declarations), BodyBytes: expression})
	}
	return out, nil
}

func (v *funcValidator) stepDirectControlImmediate(r *reader, opcode byte) (InstrKind, error) {
	switch opcode {
	case 0x05:
		return InstrInvalid, v.directElse()
	case 0x0b:
		return InstrInvalid, v.directEnd()
	}
	index, err := r.u32()
	if err != nil {
		return InstrInvalid, err
	}
	kind := InstrBr
	if opcode == 0x0d {
		kind = InstrBrIf
	}
	return kind, v.stepBranch(kind, index)
}

func (v *funcValidator) stepDirectCallImmediate(r *reader, opcode byte) (InstrKind, error) {
	index, err := r.u32()
	if err != nil {
		return InstrInvalid, err
	}
	ft, ok := v.funcType(index)
	if !ok {
		return InstrInvalid, v.verr(ErrUnknownFunc, "")
	}
	if err := v.popAll(ft.Params); err != nil {
		return InstrInvalid, err
	}
	if opcode == 0x12 {
		if !v.matchValTypes(ft.Results, v.ctrls[0].out) {
			return InstrInvalid, v.verr(ErrTypeMismatch, "return_call")
		}
		v.unreachable()
		return InstrReturnCall, nil
	}
	v.pushAll(ft.Results)
	return InstrCall, nil
}

// coalesceCompilerLocalRuns gives the compiler a compact local declaration
// view while LocalDeclBytes continues to identify the original wire offset.
// Zero-count declarations remain present: their types still need validation.
func coalesceCompilerLocalRuns(runs []LocalRun) []LocalRun {
	at := 0
	for _, run := range runs {
		if at != 0 && runs[at-1].Type == run.Type {
			runs[at-1].Count += run.Count // decodeLocals already checked the complete sum.
		} else {
			runs[at] = run
			at++
		}
	}
	clear(runs[at:])
	return runs[:at]
}
