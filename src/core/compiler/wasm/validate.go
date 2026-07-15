package wasm

import (
	"sync"
	"sync/atomic"
)

// ValidationFeatures enables release-specific validation rules without making
// them part of the default product claim. A feature may validate here while the
// frontend/runtime still reject execution explicitly.
type ValidationFeatures struct {
	CompactImports bool
	MultiMemory    bool
}

// ValidateModule validates module-level indexes and typechecks function bodies.
// The default path consumes raw BodyBytes produced by DecodeModule instead of a
// structured function-body instruction tree. Programmatically constructed tests
// may still supply Func.Body instructions when BodyBytes is empty. The default
// preserves the WebAssembly 2.0 single-memory validation boundary.
func ValidateModule(m *Module) error {
	return validateModuleWithWorkersAndFeatures(m, nil, 1, ValidationFeatures{})
}

// ValidateModuleWithWorkers is ValidateModule with bounded function-body
// parallelism. Module-level declarations, element initializer expressions, and
// other constant expressions are validated serially first. workers <= 1 retains
// the allocation-minimal serial path; larger values are capped by the local-
// function count. If multiple functions are invalid, the lowest function index
// wins regardless of completion order.
func ValidateModuleWithWorkers(m *Module, workers int) error {
	return validateModuleWithWorkersAndFeatures(m, nil, workers, ValidationFeatures{})
}

// ValidateModuleWithFeatures validates a module under explicitly staged release
// features. Unsupported execution remains the frontend's responsibility.
func ValidateModuleWithFeatures(m *Module, features ValidationFeatures) error {
	return validateModuleWithWorkersAndFeatures(m, nil, 1, features)
}

// ValidateModuleWithFeaturesAndWorkers combines explicitly staged validation
// features with bounded function-body parallelism.
func ValidateModuleWithFeaturesAndWorkers(m *Module, features ValidationFeatures, workers int) error {
	return validateModuleWithWorkersAndFeatures(m, nil, workers, features)
}

func validateModuleWithWorkers(m *Module, direct *directValidationEnv, workers int) error {
	return validateModuleWithWorkersAndFeatures(m, direct, workers, ValidationFeatures{})
}

func validateModuleWithWorkersAndFeatures(m *Module, direct *directValidationEnv, workers int, features ValidationFeatures) error {
	v := &moduleValidator{m: m, funcIndex: -1, direct: direct, features: features}
	if err := v.validateModule(); err != nil {
		return err
	}
	return v.validateFunctions(workers)
}

func (v *moduleValidator) validateFunctions(workers int) error {
	if workers <= 1 || len(v.m.Code) <= 1 {
		return v.validateFunctionsSerial()
	}
	if workers > len(v.m.Code) {
		workers = len(v.m.Code)
	}
	v.freezeCompCache()
	return v.validateFunctionsParallel(workers)
}

func (v *moduleValidator) validateFunctionsSerial() error {
	importedFuncs := v.m.ImportedFuncCount()
	memarg64 := moduleMemargOffset64(v.m)
	fv := &funcValidator{moduleValidator: v}
	for i := range v.m.Code {
		if err := v.validateFunction(fv, i, importedFuncs, memarg64); err != nil {
			return err
		}
	}
	return nil
}

func (v *moduleValidator) validateFunction(fv *funcValidator, localIndex, importedFuncs int, memarg64 bool) error {
	fn := &v.m.Code[localIndex]
	abs := importedFuncs + localIndex
	if localIndex >= len(v.m.FuncTypes) {
		return v.err(ErrUnknownFunc, "code without function type")
	}
	ft, ok := v.funcType(uint32(abs))
	if !ok {
		return v.err(ErrUnknownType, "function type")
	}
	fv.beginFunc(abs)
	if len(fn.BodyBytes) != 0 {
		return fv.validateFuncDirect(directCodeBody{locals: fn.Locals, body: fn.BodyBytes}, ft, memarg64, v.features.MultiMemory)
	}
	return fv.validateFunc(*fn, ft)
}

// validateFunctionsParallel is split from the serial path so its goroutine
// closure and worker bookkeeping cannot escape into or allocate on serial
// validation. Each worker owns one funcValidator. The module/direct metadata and
// declared-function bits are immutable, the component-type cache is frozen, and
// the serial const-expression validator is no longer reachable from body checks.
func (v *moduleValidator) validateFunctionsParallel(workers int) error {
	importedFuncs := v.m.ImportedFuncCount()
	memarg64 := moduleMemargOffset64(v.m)
	errs := make([]error, len(v.m.Code))
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			fv := funcValidator{moduleValidator: v}
			for {
				i := int(next.Add(1) - 1)
				if i >= len(v.m.Code) {
					return
				}
				errs[i] = v.validateFunction(&fv, i, importedFuncs, memarg64)
			}
		}()
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			return errs[i]
		}
	}
	return nil
}

// freezeCompCache resolves every valid flat type index before workers start.
// Function validation then performs concurrent read-only map lookups. Invalid
// body immediates may still miss the cache; resolvedCompType computes those
// without mutating the frozen map so malformed modules remain race-free.
func (v *moduleValidator) freezeCompCache() {
	for i := 0; i < v.m.flattenedTypeCount(); i++ {
		_, _ = v.resolvedCompType(TypeIdx{Index: uint32(i)})
	}
	v.compCacheFrozen = true
}

type moduleValidator struct {
	m         *Module
	funcIndex int
	direct    *directValidationEnv
	features  ValidationFeatures

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
	compCache       map[uint32]compCacheEntry
	compCacheFrozen bool

	// constFV is serial module-validation scratch for global/table/data offsets
	// and element initializer expressions. Function-body validation never reaches
	// it: table.init reads the element type metadata validated in this phase.
	constFV *funcValidator
}

type compCacheEntry struct {
	ct *CompType
	ok bool
}

const (
	maxTable32Limit  = uint64(1<<32 - 1)
	maxMemory32Pages = uint64(1 << 16)
	maxMemory64Pages = uint64(1 << 48)
)

func (v *moduleValidator) err(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}

func (v *moduleValidator) validateModule() error {
	if v.m.UsesCompactImports && !v.features.CompactImports {
		return v.err(ErrUnsupportedFeature, "compact imports")
	}
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
	if v.m.MemCount() > 1 && !v.features.MultiMemory {
		return v.err(ErrUnsupportedFeature, "multiple memories")
	}
	for _, tag := range v.m.Tags {
		if err := v.validateTagType(tag, "tag"); err != nil {
			return err
		}
	}
	for i, g := range v.m.Globals {
		if err := v.validateGlobalType(g.Type); err != nil {
			return err
		}
		globalLimit := v.m.ImportedGlobalCount() + i
		if v.direct != nil {
			if i >= len(v.direct.globalInits) {
				return v.err(ErrTypeMismatch, "global init")
			}
			if err := v.validateConstExprDirectWithGlobalLimit(v.direct.globalInits[i], g.Type.Type, globalLimit); err != nil {
				return err
			}
		} else if err := v.validateConstExprWithGlobalLimit(g.Init, g.Type.Type, globalLimit); err != nil {
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
		for _, e := range v.direct.elements {
			if err := v.validateDirectElem(e); err != nil {
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
	var op directOp
	for fv.rd.has() {
		if err := fv.decodeDirectOp(&fv.rd, false, false, &op); err != nil {
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
		return v.validateTagType(et.Tag, "import tag")
	}
	return nil
}

func (v *moduleValidator) validateTagType(tag TagType, detail string) error {
	ft := v.funcTypeFromTypeIdx(tag.Type)
	if ft == nil {
		return v.err(ErrUnknownType, detail)
	}
	if len(ft.Results) != 0 {
		return v.err(ErrTypeMismatch, "non-empty tag result type")
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
	if mt.Limits.Addr64 {
		// Core 3 memory64 limits are bounded to 2^48 pages even though their
		// binary representation and the common Limits storage are uint64.
		if mt.Limits.Min > maxMemory64Pages || (mt.Limits.Max != nil && *mt.Limits.Max > maxMemory64Pages) {
			return v.err(ErrInvalidLimitRange, "memory64 limit out of range")
		}
	} else {
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
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternFunc {
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

// globalType returns a pointer to the resolved global's type. Returning a pointer
// (into the module's stable Imports/Globals slices) rather than a value avoids a
// per-access struct copy (runtime.duffcopy) on the validation hot path.
func (v *moduleValidator) globalType(idx uint32) (*GlobalType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternGlobal {
			if n == idx {
				return &im.Type.Global, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Globals) {
		return nil, false
	}
	return &v.m.Globals[local].Type, true
}
func (v *moduleValidator) tableType(idx uint32) (TableType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternTable {
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

// memoryType returns a pointer to the resolved memory's type. Pointer return (into
// the module's stable Imports/Memories slices) avoids a per-memory-op struct copy
// on the validation hot path — checkMemArg calls this for every load/store.
func (v *moduleValidator) memoryType(idx uint32) (*MemType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternMem {
			if n == idx {
				return &im.Type.Mem, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Memories) {
		return nil, false
	}
	return &v.m.Memories[local], true
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
	return v.validateConstExprWithGlobalLimit(e, want, v.m.ImportedGlobalCount())
}

func (v *moduleValidator) validateConstExprWithGlobalLimit(e Expr, want ValType, globalLimit int) error {
	if len(e.BodyBytes) != 0 {
		return v.validateConstExprDirectWithGlobalLimit(directConstExpr{body: e.BodyBytes}, want, globalLimit)
	}
	fv := &funcValidator{moduleValidator: v, funcIndex: -1, constOnly: true, constGlobalLimit: globalLimit}
	fv.resetStacks()
	fv.pushCtrl(ctrlFunc, nil, []ValType{want})
	for _, in := range e.Instrs {
		if err := fv.step(&in); err != nil {
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

// elemRefType returns previously validated element metadata for table.init.
// It deliberately does not revisit initializer expressions: those are checked
// serially by validateElem before any function worker can start.
func (v *funcValidator) elemRefType(index uint32) (RefType, error) {
	if v.direct != nil {
		return v.directElemRefType(index)
	}
	if uint64(index) >= uint64(len(v.m.Elements)) {
		return RefType{}, v.verr(ErrUnknownTable, "table.init elem")
	}
	e := &v.m.Elements[index]
	switch e.Kind.Kind {
	case ElemFuncs, ElemFuncExprs:
		return FuncRef.Ref, nil
	case ElemTypedExprs:
		return e.Kind.Ref, nil
	default:
		return RefType{}, v.verr(ErrTypeMismatch, "unknown element kind")
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
	ctrls     []ctrlFrame
	// Small inline backing stores cover the common straight-line function and
	// const-expression cases without heap-allocating separate stack slices. Larger
	// or deeply nested functions still grow normally and reuse that capacity.
	valBuf           [2]val
	ctrlBuf          [1]ctrlFrame
	constResult      [1]ValType
	localParams      []ValType
	localRuns        []LocalRun
	localCount       uint64
	constOnly        bool
	constGlobalLimit int // globals below this absolute index are visible to a const expression
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
		if err := v.step(&in); err != nil {
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
func (v *funcValidator) localType(idx uint32) (ValType, bool) {
	if uint64(idx) >= v.localCount {
		return ValType{}, false
	}
	return LocalType(v.localParams, v.localRuns, idx)
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
	case HeapNoExn:
		return b == HeapExn
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
