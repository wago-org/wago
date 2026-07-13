package wasm

// ValidateModule validates module-level indexes and typechecks function bodies.
// The default path consumes raw BodyBytes produced by DecodeModule instead of a
// structured function-body instruction tree. Programmatically constructed tests
// may still supply Func.Body instructions when BodyBytes is empty.
func ValidateModule(m *Module) error {
	v := &moduleValidator{m: m, funcIndex: -1}
	if err := v.validateModule(); err != nil {
		return err
	}
	importedFuncs := m.ImportedFuncCount()
	memarg64 := moduleMemargOffset64(m)
	fv := &funcValidator{moduleValidator: v}
	for i, fn := range m.Code {
		abs := importedFuncs + i
		if i >= len(m.FuncTypes) {
			return v.err(ErrUnknownFunc, "code without function type")
		}
		ft, ok := v.funcType(uint32(abs))
		if !ok {
			return v.err(ErrUnknownType, "function type")
		}
		fv.beginFunc(abs)
		if len(fn.BodyBytes) != 0 {
			if err := fv.validateFuncDirect(directCodeBody{locals: fn.Locals, body: fn.BodyBytes}, ft, memarg64); err != nil {
				return err
			}
			continue
		}
		if err := fv.validateFunc(fn, ft); err != nil {
			return err
		}
	}
	return nil
}

type moduleValidator struct {
	m         *Module
	funcIndex int
	direct    *directValidationEnv

	// declaredFuncBits is the module validation context's declared function-
	// reference set. The inline word keeps the common <=64-function module from
	// allocating; larger modules use one bounded bitset allocation.
	declaredFuncBits []uint64
	declaredFuncBuf  [1]uint64

	// compCache memoizes resolveCompTypeRecIndexes keyed by flat type index. The
	// module's types are immutable during validation, so a given non-recursive
	// type index always resolves to the same CompType. funcTypeFromTypeIdx and
	// compTypeFromTypeIdx are called once per block/call/call_indirect, and
	// re-resolving allocated fresh Params/Results slices plus a CompType each
	// time; caching returns a shared read-only pointer instead.
	compCache map[uint32]compCacheEntry

	// constFV is reused across the module's const-expression checks (global/table/
	// data offsets, element expressions) to avoid a fresh validator + reader per
	// expression.
	constFV *funcValidator

	// directElemPayload caches byte-backed element payload validation. table.init
	// may reference the same segment thousands of times, but its ref type and
	// const-expression validity are module invariants.
	directElemPayload    []RefType
	directElemPayloadSet []bool

	// The supported runtime shape has at most one memory. Cache its resolved
	// type so memory-heavy bytecode does not rescan the import list per opcode.
	memory0      MemType
	memory0Known bool
	memory0OK    bool

	// A tiny direct-mapped function-signature cache removes repeated import/type
	// walks in call-heavy bodies without retaining O(functions) pointers.
	funcCache              [16]funcTypeCacheEntry
	globalImportCount      uint32
	globalImportCountKnown bool
}

type compCacheEntry struct {
	ct *CompType
	ok bool
}

type funcTypeCacheEntry struct {
	idx   uint32
	ct    *CompType
	ok    bool
	valid bool
}

const (
	maxTable32Limit  = uint64(1<<32 - 1)
	maxMemory32Pages = uint64(1 << 16)
)

func (v *moduleValidator) err(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}

func (v *moduleValidator) validateModule() error {
	v.collectDeclaredFuncs()
	for gi, rt := range v.m.Types {
		for _, st := range rt.SubTypes {
			for _, sup := range st.Supers {
				if !v.validTypeIdxInRecGroup(sup, gi) {
					return v.err(ErrUnknownType, "supertype")
				}
			}
			if st.Metadata.Describes != nil && !v.validTypeIdxInRecGroup(*st.Metadata.Describes, gi) {
				return v.err(ErrUnknownType, "describes")
			}
			if st.Metadata.Descriptor != nil && !v.validTypeIdxInRecGroup(*st.Metadata.Descriptor, gi) {
				return v.err(ErrUnknownType, "descriptor")
			}
			if err := v.validateCompTypeInRecGroup(st.Comp, gi); err != nil {
				return err
			}
		}
	}
	if err := v.validateSubtypeMetadata(); err != nil {
		return err
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
	for i, t := range v.m.Tables {
		if err := v.validateTableType(t.Type); err != nil {
			return err
		}
		if v.direct != nil {
			if i < len(v.direct.tableHasInit) && v.direct.tableHasInit[i] {
				if err := v.validateConstExprDirect(v.direct.tableInits[i], RefVal(t.Type.Ref)); err != nil {
					return err
				}
			}
		} else if t.Init != nil {
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
	if v.m.MemCount() > 1 {
		return v.err(ErrUnsupportedFeature, "multiple memories")
	}
	for _, tag := range v.m.Tags {
		if !v.validTypeIdx(tag.Type) || v.funcTypeFromTypeIdx(tag.Type) == nil {
			return v.err(ErrUnknownType, "tag")
		}
	}
	for i, g := range v.m.Globals {
		if err := v.validateGlobalType(g.Type); err != nil {
			return err
		}
		if v.direct != nil {
			if i >= len(v.direct.globalInits) {
				return v.err(ErrTypeMismatch, "global init")
			}
			if err := v.validateConstExprDirect(v.direct.globalInits[i], g.Type.Type); err != nil {
				return err
			}
		} else if err := v.validateConstExpr(g.Init, g.Type.Type); err != nil {
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
	if v.direct != nil {
		for i := range v.direct.elements {
			if err := v.validateDirectElem(i); err != nil {
				return err
			}
		}
	} else {
		for _, e := range v.m.Elements {
			if err := v.validateElem(e); err != nil {
				return err
			}
		}
	}
	activeData := 0
	for i, d := range v.m.Data {
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
			if v.direct != nil {
				if i >= len(v.direct.dataOffsets) {
					return v.err(ErrTypeMismatch, "data offset")
				}
				if err := v.validateConstExprDirect(v.direct.dataOffsets[i], want); err != nil {
					return err
				}
			} else if err := v.validateConstExpr(d.Mode.Offset, want); err != nil {
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

func (v *moduleValidator) collectDeclaredFuncs() {
	for _, ex := range v.m.Exports {
		if ex.Index.Kind == ExternFunc {
			v.declareFunc(ex.Index.Index)
		}
	}
	for _, table := range v.m.Tables {
		if table.Init != nil {
			v.collectDeclaredFuncsInExpr(*table.Init)
		}
	}
	for _, global := range v.m.Globals {
		v.collectDeclaredFuncsInExpr(global.Init)
	}
	for _, elem := range v.m.Elements {
		switch elem.Kind.Kind {
		case ElemFuncs:
			for _, idx := range elem.Kind.Funcs {
				v.declareFunc(uint32(idx))
			}
		case ElemFuncExprs, ElemTypedExprs:
			for _, expr := range elem.Kind.Exprs {
				v.collectDeclaredFuncsInExpr(expr)
			}
		}
	}
}

func (v *moduleValidator) collectDeclaredFuncsInExpr(expr Expr) {
	if len(expr.BodyBytes) == 0 {
		for _, in := range expr.Instrs {
			if in.Kind == InstrRefFunc {
				v.declareFunc(in.Index)
			}
		}
		return
	}

	fv := v.constFV
	if fv == nil {
		fv = &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true}
		v.constFV = fv
	}
	fv.rd.reset(expr.BodyBytes)
	for fv.rd.has() {
		op, err := fv.decodeDirectOp(&fv.rd, false)
		if err != nil {
			// The normal const-expression validation path reports malformed bytes;
			// declaration collection must not change validation error ordering.
			return
		}
		if op.kind == directInstr && op.instr.Kind == InstrRefFunc {
			v.declareFunc(op.instr.Index)
		}
	}
}

func (v *moduleValidator) declareFunc(idx uint32) {
	funcCount := v.m.FuncCount()
	if uint64(idx) >= uint64(funcCount) {
		return
	}
	if v.declaredFuncBits == nil {
		words := (uint64(funcCount) + 63) / 64
		if words == 1 {
			v.declaredFuncBits = v.declaredFuncBuf[:]
		} else {
			v.declaredFuncBits = make([]uint64, int(words))
		}
	}
	v.declaredFuncBits[idx/64] |= uint64(1) << (idx % 64)
}

func (v *moduleValidator) isDeclaredFunc(idx uint32) bool {
	word := idx / 64
	return int(word) < len(v.declaredFuncBits) && v.declaredFuncBits[word]&(uint64(1)<<(idx%64)) != 0
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

func (v *moduleValidator) validateCompTypeInRecGroup(ct CompType, recGroup int) error {
	switch ct.Kind {
	case CompFunc:
		for _, t := range ct.Params {
			if err := v.validateValTypeInRecGroup(t, recGroup); err != nil {
				return err
			}
		}
		for _, t := range ct.Results {
			if err := v.validateValTypeInRecGroup(t, recGroup); err != nil {
				return err
			}
		}
	case CompStruct:
		for _, f := range ct.Fields {
			if err := v.validateFieldTypeInRecGroup(f, recGroup); err != nil {
				return err
			}
		}
	case CompArray:
		return v.validateFieldTypeInRecGroup(ct.Array, recGroup)
	default:
		return v.err(ErrUnknownType, "component type")
	}
	return nil
}

func (v *moduleValidator) validateFieldTypeInRecGroup(ft FieldType, recGroup int) error {
	return v.validateStorageTypeInRecGroup(ft.Storage, recGroup)
}

func (v *moduleValidator) validateStorageTypeInRecGroup(st StorageType, recGroup int) error {
	if st.Packed {
		switch st.Pack {
		case PackI8, PackI16:
			return nil
		default:
			return v.err(ErrUnknownType, "packed storage")
		}
	}
	return v.validateValTypeInRecGroup(st.Val, recGroup)
}

func (v *moduleValidator) validateValType(t ValType) error {
	return v.validateValTypeInRecGroup(t, -1)
}

func (v *moduleValidator) validateValTypeInRecGroup(t ValType, recGroup int) error {
	switch t.Kind {
	case ValNum, ValVec:
		return nil
	case ValRef:
		return v.validateRefTypeInRecGroup(t.Ref, recGroup)
	default:
		return v.err(ErrUnknownType, "value type")
	}
}

func (v *moduleValidator) validateRefType(rt RefType) error {
	return v.validateRefTypeInRecGroup(rt, -1)
}

func (v *moduleValidator) validateRefTypeInRecGroup(rt RefType, recGroup int) error {
	return v.validateHeapTypeInRecGroup(rt.Heap, recGroup)
}

func (v *moduleValidator) validateHeapType(ht HeapType) error {
	return v.validateHeapTypeInRecGroup(ht, -1)
}

func (v *moduleValidator) validateHeapTypeInRecGroup(ht HeapType, recGroup int) error {
	switch ht.Kind {
	case HeapAbs:
		return nil
	case HeapTypeIndex:
		if !v.validTypeIdxInRecGroup(ht.Type, recGroup) {
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
	slot := &v.funcCache[idx&(uint32(len(v.funcCache))-1)]
	if slot.valid && slot.idx == idx {
		return slot.ct, slot.ok
	}
	ct, ok := v.funcTypeUncached(idx)
	*slot = funcTypeCacheEntry{idx: idx, ct: ct, ok: ok, valid: true}
	return ct, ok
}

func (v *moduleValidator) funcTypeUncached(idx uint32) (*CompType, bool) {
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
	n := v.importedGlobalCount()
	if idx >= n {
		local := int(idx - n)
		if local < 0 || local >= len(v.m.Globals) {
			return GlobalType{}, false
		}
		return v.m.Globals[local].Type, true
	}
	// Imported globals are uncommon in generated modules. Preserve the direct
	// import lookup only for that prefix; local globals avoid this scan entirely.
	n = 0
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternGlobal {
			if n == idx {
				return im.Type.Global, true
			}
			n++
		}
	}
	return GlobalType{}, false
}

func (v *moduleValidator) importedGlobalCount() uint32 {
	if v.globalImportCountKnown {
		return v.globalImportCount
	}
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternGlobal {
			v.globalImportCount++
		}
	}
	v.globalImportCountKnown = true
	return v.globalImportCount
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
	if idx == 0 && v.memory0Known {
		return v.memory0, v.memory0OK
	}
	n := uint32(0)
	for _, im := range v.m.Imports {
		if im.Type.Kind == ExternMem {
			if n == idx {
				if idx == 0 {
					v.memory0, v.memory0OK, v.memory0Known = im.Type.Mem, true, true
				}
				return im.Type.Mem, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Memories) {
		if idx == 0 {
			v.memory0Known = true
		}
		return MemType{}, false
	}
	if idx == 0 {
		v.memory0, v.memory0OK, v.memory0Known = v.m.Memories[local], true, true
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
	if len(e.BodyBytes) != 0 {
		return v.validateConstExprDirect(directConstExpr{body: e.BodyBytes}, want)
	}
	fv := &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true}
	fv.resetStacks()
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
	kind    ValTypeKind
	num     NumType
	slot    uint32
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

	// Byte-backed binary validation does not build nested If instruction bodies,
	// so it tracks then/else arms while streaming opcodes. ifThenHeight records
	// the operand-stack height at the end of the then-arm (after its results were
	// re-pushed) so the else-arm end can confirm both arms leave the same shape.
	ifThenHeight int
	ifSeenElse   bool
}

type funcValidator struct {
	*moduleValidator
	funcIndex int
	vals      []val
	// refVals holds reference payloads by operand-stack slot. Scalar values use
	// only the compact val tag above, so ordinary numeric validation no longer
	// copies ValType's full reference-shaped payload on every push/pop.
	refVals []RefType
	ctrls   []ctrlFrame
	// Small inline backing stores cover the common straight-line function and
	// const-expression cases without heap-allocating separate stack slices. Larger
	// or deeply nested functions still grow normally and reuse that capacity.
	valBuf      [2]val
	ctrlBuf     [1]ctrlFrame
	constResult [1]ValType
	localParams []ValType
	localRuns   []LocalRun
	localCount  uint64
	constOnly   bool
	// rd is reused across bodies validated by this funcValidator so the byte
	// cursor is not heap-allocated per function/const-expression.
	rd reader
	// opExt is a scratch instruction-immediate payload reused across the streamed
	// opcodes of one body. decodeDirectOp fills it and stepDirectOp consumes it
	// immediately without retaining the pointer, so a single buffer avoids a heap
	// instrExt per memory/br_table/select/ref.null instruction.
	opExt instrExt
}

func (v *funcValidator) verr(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}

// beginFunc resets the per-function operand/control stacks so a single
// funcValidator can be reused across every function body in a module. Reusing
// the value and control slices keeps their capacity between functions, avoiding
// the append-from-nil regrowth that dominated validation allocations.
func (v *funcValidator) beginFunc(funcIndex int) {
	v.funcIndex = funcIndex
	v.constOnly = false
	v.resetStacks()
}

func (v *funcValidator) resetStacks() {
	if v.vals == nil {
		v.vals = v.valBuf[:0]
	} else {
		v.vals = v.vals[:0]
	}
	if v.ctrls == nil {
		v.ctrls = v.ctrlBuf[:0]
	} else {
		v.ctrls = v.ctrls[:0]
	}
}
func (v *funcValidator) validateFunc(fn Func, ft *CompType) error {
	v.localParams = ft.Params
	v.localRuns = fn.Locals.Runs
	var overflow bool
	v.localCount, overflow = LocalCount(ft.Params, fn.Locals.Runs)
	if overflow {
		return v.verr(ErrInvalidLimitRange, "local count overflow")
	}
	for _, run := range fn.Locals.Runs {
		if err := v.validateValType(run.Type); err != nil {
			return err
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
func (v *funcValidator) push(t ValType) {
	v.pushPtr(&t)
}

func (v *funcValidator) pushPtr(t *ValType) {
	x := val{kind: t.Kind, num: t.Num, slot: uint32(len(v.vals))}
	if t.Kind == ValRef {
		v.ensureRefSlot(x.slot)
		v.refVals[x.slot] = t.Ref
	}
	v.vals = append(v.vals, x)
}

// pushVal preserves a popped value while moving any reference payload to its
// new operand-stack slot.
func (v *funcValidator) pushVal(x val) {
	oldSlot := x.slot
	x.slot = uint32(len(v.vals))
	if x.kind == ValRef {
		v.ensureRefSlot(x.slot)
		v.refVals[x.slot] = v.refVals[oldSlot]
	}
	v.vals = append(v.vals, x)
}

func (v *funcValidator) ensureRefSlot(slot uint32) {
	if int(slot) < len(v.refVals) {
		return
	}
	v.refVals = append(v.refVals, make([]RefType, int(slot)+1-len(v.refVals))...)
}

func (v *funcValidator) valType(x val) ValType {
	t := ValType{Kind: x.kind, Num: x.num}
	if x.kind == ValRef {
		t.Ref = v.refVals[x.slot]
	}
	return t
}

func (v *funcValidator) setValType(x *val, t ValType) {
	x.kind, x.num = t.Kind, t.Num
	if t.Kind == ValRef {
		v.ensureRefSlot(x.slot)
		v.refVals[x.slot] = t.Ref
	}
}
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
	return v.popExpectPtr(&t)
}

func (v *funcValidator) popExpectPtr(t *ValType) error {
	x, err := v.pop()
	if err != nil {
		return err
	}
	// Numeric/vector values dominate generated wasm. Their exact match is two
	// inline fields; keep reference subtyping on the complete general path.
	if !x.unknown && !(x.kind != ValRef && x.kind == t.Kind && x.num == t.Num) {
		got := v.valType(x)
		if !v.subtype(got, *t) {
			return v.verr(ErrTypeMismatch, got.String()+" is not "+t.String())
		}
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
func (v *funcValidator) localType(idx uint32) (ValType, bool) {
	t, ok := v.localTypePtr(idx)
	if !ok {
		return ValType{}, false
	}
	return *t, true
}

func (v *funcValidator) localTypePtr(idx uint32) (*ValType, bool) {
	if uint64(idx) >= v.localCount {
		return nil, false
	}
	if uint64(idx) < uint64(len(v.localParams)) {
		return &v.localParams[idx], true
	}
	rem := uint64(idx) - uint64(len(v.localParams))
	for i := range v.localRuns {
		if rem < uint64(v.localRuns[i].Count) {
			return &v.localRuns[i].Type, true
		}
		rem -= uint64(v.localRuns[i].Count)
	}
	return nil, false
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
	}
	return false
}

// Single-value block results are by far the most common non-void block
// signature. Returning a shared read-only slice avoids allocating a one-element
// []ValType for every such block/loop/if. blockSig results are only ever read
// (stored in ctrlFrame.in/out, iterated by popAll/pushAll, returned by label).
var (
	blockOutI32  = []ValType{I32}
	blockOutI64  = []ValType{I64}
	blockOutF32  = []ValType{F32}
	blockOutF64  = []ValType{F64}
	blockOutV128 = []ValType{V128}
)

func singleValTypeSlice(t ValType) []ValType {
	switch t.Kind {
	case ValNum:
		switch t.Num {
		case NumI32:
			return blockOutI32
		case NumI64:
			return blockOutI64
		case NumF32:
			return blockOutF32
		case NumF64:
			return blockOutF64
		}
	case ValVec:
		return blockOutV128
	}
	return []ValType{t}
}

func (v *funcValidator) blockSig(bt BlockType) (in, out []ValType, err error) {
	switch bt.Kind {
	case BlockVoid:
		return nil, nil, nil
	case BlockVal:
		if err := v.validateValType(bt.Val); err != nil {
			return nil, nil, err
		}
		return nil, singleValTypeSlice(bt.Val), nil
	case BlockTypeIndex:
		ft := v.funcTypeFromTypeIdx(bt.Type)
		if ft == nil {
			return nil, nil, v.verr(ErrUnknownType, "block")
		}
		return ft.Params, ft.Results, nil
	}
	return nil, nil, v.verr(ErrUnknownType, "")
}
