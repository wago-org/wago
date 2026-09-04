package wasm

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// ValidationFeatures enables release-specific validation rules without making
// them part of the default product claim. A feature may validate here while the
// frontend/runtime still reject execution explicitly.
type ValidationFeatures struct {
	CompactImports       bool
	MultiMemory          bool
	ExtendedConstGlobals bool // prior immutable local global.get in constant expressions
	GCConstExpr          bool // internal staged admission for GC allocation/conversion constant expressions
}

// ValidationLimits bounds implementation resources consumed by one module.
// MaxFunctionLocals counts function parameters and declared locals together.
// MaxMemoriesPerModule counts imported and local memories. A zero field selects
// its default value.
type ValidationLimits struct {
	MaxFunctionLocals    uint32
	MaxMemoriesPerModule uint32
}

// DefaultMaxFunctionLocals is the largest count represented by the current
// uint16-backed compiler metadata. Native frame safety is checked separately.
const DefaultMaxFunctionLocals uint32 = MaximumFunctionLocals

// DefaultMaxMemoriesPerModule is the ordinary validation ceiling. It matches
// the WebAssembly JavaScript API implementation limit. The configurable maximum
// cannot exceed the Linux process registry capacity.
const DefaultMaxMemoriesPerModule uint32 = 100

// MaximumFunctionLocals is the largest configurable validation ceiling.
const MaximumFunctionLocals uint32 = 1<<16 - 1

// MaximumMemoriesPerModule is the largest configurable validation ceiling.
const MaximumMemoriesPerModule uint32 = 4096

var defaultValidationLimits = ValidationLimits{
	MaxFunctionLocals:    DefaultMaxFunctionLocals,
	MaxMemoriesPerModule: DefaultMaxMemoriesPerModule,
}

// ValidateModule validates module-level indexes and typechecks function bodies.
// The default path consumes raw BodyBytes produced by DecodeModule instead of a
// structured function-body instruction tree. Programmatically constructed tests
// may still supply Func.Body instructions when BodyBytes is empty. The default
// preserves the WebAssembly 2.0 single-memory validation boundary.
func ValidateModule(m *Module) error {
	return validateModuleWithWorkersFeaturesAndLimits(m, nil, 1, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateModuleWithWorkers is ValidateModule with bounded function-body
// parallelism. Module-level declarations, element initializer expressions, and
// other constant expressions are validated serially first. workers <= 1 retains
// the allocation-minimal serial path; larger values are capped by the local-
// function count. If multiple functions are invalid, the lowest function index
// wins regardless of completion order.
func ValidateModuleWithWorkers(m *Module, workers int) error {
	return validateModuleWithWorkersFeaturesAndLimits(m, nil, workers, ValidationFeatures{}, defaultValidationLimits)
}

// ValidateModuleWithFeatures validates a module under explicitly staged release
// features. Unsupported execution remains the frontend's responsibility.
func ValidateModuleWithFeatures(m *Module, features ValidationFeatures) error {
	return validateModuleWithWorkersFeaturesAndLimits(m, nil, 1, features, defaultValidationLimits)
}

// ValidateModuleWithFeaturesAndWorkers combines explicitly staged validation
// features with bounded function-body parallelism.
func ValidateModuleWithFeaturesAndWorkers(m *Module, features ValidationFeatures, workers int) error {
	return validateModuleWithWorkersFeaturesAndLimits(m, nil, workers, features, defaultValidationLimits)
}

// ValidateModuleWithConfig validates a module with explicit feature, worker,
// and resource-limit policy.
func ValidateModuleWithConfig(m *Module, features ValidationFeatures, workers int, limits ValidationLimits) error {
	return validateModuleWithWorkersFeaturesAndLimits(m, nil, workers, features, limits)
}

// ValidateModuleWithAnalysis validates a module and gathers transient,
// architecture-neutral facts during the same function-body walk. analysis is
// cleared on failure and is valid only when this function returns nil.
func ValidateModuleWithAnalysis(m *Module, features ValidationFeatures, workers int, limits ValidationLimits, analysis *ValidatedModuleAnalysis) error {
	return validateModuleWithWorkersFeaturesAndLimitsAnalysis(m, nil, workers, features, limits, analysis)
}

func validateModuleWithWorkersFeaturesAndLimits(m *Module, direct *directValidationEnv, workers int, features ValidationFeatures, limits ValidationLimits) error {
	return validateModuleWithWorkersFeaturesAndLimitsAnalysis(m, direct, workers, features, limits, nil)
}

func validateModuleWithWorkersFeaturesAndLimitsAnalysis(m *Module, direct *directValidationEnv, workers int, features ValidationFeatures, limits ValidationLimits, analysis *ValidatedModuleAnalysis) (err error) {
	if analysis != nil {
		analysis.reset(m)
		defer func() {
			if err != nil {
				*analysis = ValidatedModuleAnalysis{}
			}
		}()
	}
	if limits.MaxFunctionLocals == 0 {
		limits.MaxFunctionLocals = DefaultMaxFunctionLocals
	}
	if limits.MaxMemoriesPerModule == 0 {
		limits.MaxMemoriesPerModule = DefaultMaxMemoriesPerModule
	}
	if limits.MaxFunctionLocals > MaximumFunctionLocals {
		return &ValidationError{Code: ErrInvalidLimitRange, Func: -1, Detail: "configured function local limit exceeds 65535"}
	}
	if limits.MaxMemoriesPerModule > MaximumMemoriesPerModule {
		return &ValidationError{Code: ErrInvalidLimitRange, Func: -1, Detail: "configured memory count limit exceeds 4096"}
	}
	// Keep the serial validation owner in this frame. TinyGo's conservative
	// collector can otherwise lose a short-lived heap validator while nested
	// decoding allocates, leaving its inline operand/control stacks reclaimed
	// during validation.
	v := moduleValidator{
		m:                m,
		funcIndex:        -1,
		direct:           direct,
		features:         features,
		limits:           limits,
		analysis:         analysis,
		analysisFuncBase: m.ImportedFuncCount(),
	}
	if err := v.validateModule(); err != nil {
		runtime.KeepAlive(m)
		runtime.KeepAlive(direct)
		return err
	}
	err = v.validateFunctions(workers)
	if err == nil && analysis != nil {
		analysis.finish()
	}
	// TinyGo's conservative collector needs the metadata owners to remain live
	// while validators consume their nested byte slices and type views.
	runtime.KeepAlive(m)
	runtime.KeepAlive(direct)
	return err
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
	widths := moduleMemargWidths(v.m)
	// Keep the reusable operand/control-stack owner in the validating frame.
	// Besides avoiding one allocation, this makes its lifetime explicit for
	// TinyGo's conservative collector across allocation-heavy decode steps.
	fv := funcValidator{moduleValidator: v}
	for i := range v.m.Code {
		if err := v.validateFunction(&fv, i, importedFuncs, widths); err != nil {
			return err
		}
	}
	return nil
}

func (v *moduleValidator) validateFunction(fv *funcValidator, localIndex, importedFuncs int, widths memargWidths) error {
	fn := &v.m.Code[localIndex]
	abs := importedFuncs + localIndex
	if localIndex >= len(v.m.FuncTypes) {
		return v.err(ErrUnknownFunc, "code without function type")
	}
	ft, ok := v.funcType(uint32(abs))
	if !ok {
		return v.err(ErrUnknownType, "function type")
	}
	if v.analysis != nil {
		v.analysis.Funcs[localIndex] = ValidatedFuncFacts{BodyBytes: saturatingUint32(len(fn.BodyBytes))}
	}
	fv.beginFunc(abs)
	var err error
	if len(fn.BodyBytes) != 0 {
		err = fv.validateFuncDirect(directCodeBody{locals: fn.Locals, body: fn.BodyBytes}, ft, widths, v.features.MultiMemory)
	} else {
		err = fv.validateFunc(*fn, ft)
	}
	if err == nil && v.analysis != nil {
		v.analysis.Funcs[localIndex].LocalCount = uint16(fv.localCount)
	}
	return err
}

// validateFunctionsParallel is split from the serial path so its goroutine
// closure and worker bookkeeping cannot escape into or allocate on serial
// validation. Each worker owns one funcValidator. The module/direct metadata and
// declared-function bits are immutable, the component-type cache is frozen, and
// the serial const-expression validator is no longer reachable from body checks.
func (v *moduleValidator) validateFunctionsParallel(workers int) error {
	importedFuncs := v.m.ImportedFuncCount()
	widths := moduleMemargWidths(v.m)
	type result struct {
		index int
		err   error
	}
	results := make([]result, workers)
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			fv := funcValidator{moduleValidator: v}
			for {
				i := int(next.Add(1) - 1)
				if i >= len(v.m.Code) {
					return
				}
				if err := v.validateFunction(&fv, i, importedFuncs, widths); err != nil {
					results[worker] = result{index: i, err: err}
					return
				}
			}
		}()
	}
	wg.Wait()
	lowest := len(v.m.Code)
	var first error
	for i := range results {
		if results[i].err != nil && results[i].index < lowest {
			lowest = results[i].index
			first = results[i].err
		}
	}
	return first
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
	limits    ValidationLimits
	analysis  *ValidatedModuleAnalysis
	// analysisFuncBase converts the absolute funcIndex already carried by a
	// funcValidator into the caller-owned declared-function summary index.
	analysisFuncBase int

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

	// Type-section indexes are built once and reused by GC subtype validation.
	// Without them, every flat or recursive-group lookup rescans preceding groups.
	typeIndexReady bool
	flatSubTypes   []moduleSubTypeRef
	typeGroupBases []int

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
		for si, st := range rt.SubTypes {
			if len(st.Supers) > 1 {
				return v.err(ErrTypeMismatch, "multiple supertypes")
			}
			for _, sup := range st.Supers {
				if !v.validTypeIdxInRecGroup(sup, gi) {
					return v.err(ErrUnknownType, "supertype")
				}
				if sup.Rec && sup.Index >= uint32(si) {
					return v.err(ErrTypeMismatch, "supertype must precede subtype")
				}
			}
			if describes, present := st.Metadata.Describes.Get(); present && !v.validTypeIdxInRecGroup(describes, gi) {
				return v.err(ErrUnknownType, "describes")
			}
			if descriptor, present := st.Metadata.Descriptor.Get(); present && !v.validTypeIdxInRecGroup(descriptor, gi) {
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
		hasInit := t.Init != nil
		if v.direct != nil {
			hasInit = i < len(v.direct.tableHasInit) && v.direct.tableHasInit[i]
			if hasInit {
				if err := v.validateConstExprDirect(v.direct.tableInits[i], RefVal(t.Type.Ref)); err != nil {
					return err
				}
			}
		} else if hasInit {
			if err := v.validateConstExpr(*t.Init, RefVal(t.Type.Ref)); err != nil {
				return err
			}
		}
		if !hasInit && !t.Type.Ref.Nullable() {
			return v.err(ErrTypeMismatch, "non-defaultable table requires an initializer")
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
	if uint64(v.m.MemCount()) > uint64(v.limits.MaxMemoriesPerModule) {
		return v.err(ErrResourceLimitExceeded, fmt.Sprintf("memory count %d exceeds configured limit %d", v.m.MemCount(), v.limits.MaxMemoriesPerModule))
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
			flags, ok := v.memoryProperties(uint32(d.Mode.Mem))
			if !ok {
				return v.err(ErrUnknownMemory, "data")
			}
			want := I32
			if flags&externTypeAddr64 != 0 {
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
		if err := fv.decodeDirectOp(&fv.rd, fixedMemargWidths(false), false, &op); err != nil {
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
		if v.funcTypeFromTypeIdx(et.FuncType()) == nil {
			return v.err(ErrUnknownType, "import func")
		}
	case ExternTable:
		if err := v.validateRefType(et.value.Ref()); err != nil {
			return err
		}
		if et.flags&externTypeAddr64 == 0 && (et.min > maxTable32Limit || et.flags&externTypeHasMax != 0 && et.max > maxTable32Limit) {
			return v.err(ErrInvalidLimitRange, "table32 limit out of range")
		}
		if et.flags&externTypeHasMax != 0 && et.max < et.min {
			return v.err(ErrInvalidLimitRange, "table max < min")
		}
	case ExternMem:
		hasMax := et.flags&externTypeHasMax != 0
		if et.flags&externTypeShared != 0 && !hasMax {
			return v.err(ErrInvalidSharedMemory, "")
		}
		if et.flags&externTypeAddr64 != 0 {
			if et.min > maxMemory64Pages || hasMax && et.max > maxMemory64Pages {
				return v.err(ErrInvalidLimitRange, "memory64 limit out of range")
			}
		} else if et.min > maxMemory32Pages || hasMax && et.max > maxMemory32Pages {
			return v.err(ErrInvalidLimitRange, "memory32 limit out of range")
		}
		if hasMax && et.max < et.min {
			return v.err(ErrInvalidLimitRange, "memory max < min")
		}
	case ExternGlobal:
		return v.validateValType(et.value)
	case ExternTag:
		return v.validateTagType(et.TagType(), "import tag")
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
		if tt.Limits.Min > maxTable32Limit || (tt.Limits.HasMax && tt.Limits.Max > maxTable32Limit) {
			return v.err(ErrInvalidLimitRange, "table32 limit out of range")
		}
	}
	if tt.Limits.HasMax && tt.Limits.Max < tt.Limits.Min {
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
	return v.validateStorageTypeInRecGroup(ft.Storage(), recGroup)
}

func (v *moduleValidator) validateStorageTypeInRecGroup(st StorageType, recGroup int) error {
	if st.Packed() {
		switch st.Pack() {
		case PackI8, PackI16:
			return nil
		default:
			return v.err(ErrUnknownType, "packed storage")
		}
	}
	return v.validateValTypeInRecGroup(st.Val(), recGroup)
}

func (v *moduleValidator) validateValType(t ValType) error {
	return v.validateValTypeInRecGroup(t, -1)
}

func (v *moduleValidator) validateValTypeInRecGroup(t ValType, recGroup int) error {
	switch t.Kind() {
	case ValNum, ValVec:
		return nil
	case ValRef:
		return v.validateRefTypeInRecGroup(t.Ref(), recGroup)
	default:
		return v.err(ErrUnknownType, "value type")
	}
}

func (v *moduleValidator) validateRefType(rt RefType) error {
	return v.validateRefTypeInRecGroup(rt, -1)
}

func (v *moduleValidator) validateRefTypeInRecGroup(rt RefType, recGroup int) error {
	return v.validateHeapTypeInRecGroup(rt.Heap(), recGroup)
}

func (v *moduleValidator) validateHeapType(ht HeapType) error {
	return v.validateHeapTypeInRecGroup(ht, -1)
}

func (v *moduleValidator) validateHeapTypeInRecGroup(ht HeapType, recGroup int) error {
	switch ht.Kind() {
	case HeapAbs:
		return nil
	case HeapTypeIndex:
		if !v.validTypeIdxInRecGroup(ht.Type(), recGroup) {
			return v.err(ErrUnknownType, "heap type")
		}
		return nil
	case HeapDefType:
		if _, _, _, valid := ht.Def(); !valid {
			return v.err(ErrUnknownType, "heap def type")
		}
		return nil
	default:
		return v.err(ErrUnknownType, "heap type")
	}
}
func (v *moduleValidator) validateMemType(mt MemType) error {
	if mt.Shared && !mt.Limits.HasMax {
		return v.err(ErrInvalidSharedMemory, "")
	}
	if mt.Limits.Addr64 {
		// Core 3 memory64 limits are bounded to 2^48 pages even though their
		// binary representation and the common Limits storage are uint64.
		if mt.Limits.Min > maxMemory64Pages || (mt.Limits.HasMax && mt.Limits.Max > maxMemory64Pages) {
			return v.err(ErrInvalidLimitRange, "memory64 limit out of range")
		}
	} else {
		// Memory32 limits are page counts bounded to the 4 GiB address space.
		// Reject values that only fit because the common Limits storage is uint64.
		if mt.Limits.Min > maxMemory32Pages || (mt.Limits.HasMax && mt.Limits.Max > maxMemory32Pages) {
			return v.err(ErrInvalidLimitRange, "memory32 limit out of range")
		}
	}
	if mt.Limits.HasMax && mt.Limits.Max < mt.Limits.Min {
		return v.err(ErrInvalidLimitRange, "memory max < min")
	}
	return nil
}
func (v *moduleValidator) funcType(idx uint32) (*CompType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternFunc {
			if n == idx {
				ft := v.funcTypeFromTypeIdx(im.Type.FuncType())
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

// globalType returns the resolved global type by value. Imported types are
// packed, so returning a pointer would force their reconstructed value to
// escape on every global.get/global.set validation.
func (v *moduleValidator) globalType(idx uint32) (GlobalType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternGlobal {
			if n == idx {
				return im.Type.GlobalType(), true
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

func (v *moduleValidator) globalProperties(idx uint32) (typ ValType, mutable bool, ok bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternGlobal {
			if n == idx {
				return im.Type.value, im.Type.flags&externTypeMutable != 0, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Globals) {
		return ValType{}, false, false
	}
	gt := v.m.Globals[local].Type
	return gt.Type, gt.Mutable, true
}
func (v *moduleValidator) tableType(idx uint32) (TableType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternTable {
			if n == idx {
				return im.Type.TableType(), true
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

// memoryType returns the resolved memory type by value. Imported types are
// packed, so returning a pointer would make reconstruction escape on every
// load/store validated by checkMemArg.
func (v *moduleValidator) memoryType(idx uint32) (MemType, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternMem {
			if n == idx {
				return im.Type.MemType(), true
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

func (v *moduleValidator) memoryProperties(idx uint32) (uint8, bool) {
	n := uint32(0)
	for i := range v.m.Imports {
		if im := &v.m.Imports[i]; im.Type.Kind == ExternMem {
			if n == idx {
				return im.Type.flags, true
			}
			n++
		}
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Memories) {
		return 0, false
	}
	mt := v.m.Memories[local]
	var flags uint8
	if mt.Limits.Addr64 {
		flags |= externTypeAddr64
	}
	if mt.Shared {
		flags |= externTypeShared
	}
	return flags, true
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
	return v.validateConstExprWithGlobalLimit(e, want, v.m.ImportedGlobalCount()+len(v.m.Globals))
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
		return Ref(false, AbsHeap(HeapFunc), false), nil
	case ElemFuncExprs:
		for _, ex := range e.Kind.Exprs {
			if err := v.validateConstExpr(ex, FuncRef); err != nil {
				return RefType{}, err
			}
		}
		return FuncRef.Ref(), nil
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
		return FuncRef.Ref(), nil
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
	ctrlTry
)

type ctrlFrame struct {
	in, out []ValType
	height  int
	// initHeight is the local-initialization log watermark at control entry.
	// Initializations performed inside a block do not escape that block; an
	// else arm likewise restarts from the if entry state.
	initHeight int

	// Byte-backed binary validation does not build nested If instruction bodies,
	// so it tracks then/else arms while streaming opcodes. ifThenHeight records
	// the operand-stack height at the end of the then-arm (after its results were
	// re-pushed) so the else-arm end can confirm both arms leave the same shape.
	ifThenHeight int
	// branchTableEpoch marks whether this exact label frame has already been
	// checked by the current br_table.
	branchTableEpoch uint32
	kind             ctrlKind
	unreachable      bool
	ifSeenElse       bool
}

type funcValidator struct {
	*moduleValidator
	funcIndex int
	vals      []val
	ctrls     []ctrlFrame
	// Small inline backing stores cover the common straight-line function and
	// const-expression cases without heap-allocating separate stack slices. Larger
	// or deeply nested functions still grow normally and reuse that capacity.
	valBuf      [2]val
	ctrlBuf     [1]ctrlFrame
	constResult [1]ValType
	localParams []ValType
	localRuns   []LocalRun
	localCount  uint64
	// Non-nullable reference locals have no default value. Track successful
	// local.set/local.tee operations sparsely and roll them back at structured
	// control boundaries. The map grows only with locals actually initialized by
	// the function body, not with an attacker-controlled declared local count.
	initializedLocals map[uint32]struct{}
	localInitLog      []uint32
	constGlobalLimit  int // globals below this absolute index are visible to a const expression
	// branchTableEpoch is packed before constOnly. Each
	// funcValidator owns its control frames, including under parallel validation.
	branchTableEpoch uint32
	constOnly        bool
	// rd is reused across bodies validated by this funcValidator so the byte
	// cursor is not heap-allocated per function/const-expression.
	rd reader
	// opExt is a scratch instruction-immediate payload reused across the streamed
	// opcodes of one body. decodeDirectOp fills it and stepDirectOp consumes it
	// immediately without retaining the pointer, so a single buffer avoids a heap
	// instrExt per memory/br_table/select/ref.null or payload-bearing SIMD
	// instruction.
	opExt instrExt
}

func (v *funcValidator) verr(c ValidationErrorCode, d string) error {
	return &ValidationError{Code: c, Func: v.funcIndex, Detail: d}
}

func (v *funcValidator) analysisFacts() *ValidatedFuncFacts {
	if v.moduleValidator == nil || v.analysis == nil {
		return nil
	}
	index := v.funcIndex - v.analysisFuncBase
	if index < 0 || index >= len(v.analysis.Funcs) {
		return nil
	}
	return &v.analysis.Funcs[index]
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
	if v.localCount > uint64(v.limits.MaxFunctionLocals) {
		return v.verr(ErrInvalidLimitRange, "parameter and local count exceeds configured limit")
	}
	for _, run := range fn.Locals.Runs {
		if err := v.validateValType(run.Type); err != nil {
			return err
		}
	}
	v.resetLocalInitialization()
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
func (v *funcValidator) push(t ValType) {
	v.vals = append(v.vals, val{t: t})
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
	v.ctrls = append(v.ctrls, ctrlFrame{kind: k, in: in, out: out, height: len(v.vals), initHeight: len(v.localInitLog)})
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
	v.restoreLocalInitialization(f.initHeight)
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

func (v *funcValidator) resetLocalInitialization() {
	if v.initializedLocals != nil {
		clear(v.initializedLocals)
	}
	v.localInitLog = v.localInitLog[:0]
}

func (v *funcValidator) localIsInitialized(idx uint32, t ValType) bool {
	// Function parameters are initialized by the caller. Numeric, vector, and
	// nullable reference locals have a default value at function entry.
	if uint64(idx) < uint64(len(v.localParams)) || t.Kind() != ValRef || t.Ref().Nullable() {
		return true
	}
	_, ok := v.initializedLocals[idx]
	return ok
}

func (v *funcValidator) initializeLocal(idx uint32, t ValType) {
	if uint64(idx) < uint64(len(v.localParams)) || t.Kind() != ValRef || t.Ref().Nullable() {
		return
	}
	if _, ok := v.initializedLocals[idx]; ok {
		return
	}
	if v.initializedLocals == nil {
		v.initializedLocals = make(map[uint32]struct{})
	}
	v.initializedLocals[idx] = struct{}{}
	v.localInitLog = append(v.localInitLog, idx)
}

func (v *funcValidator) restoreLocalInitialization(height int) {
	for len(v.localInitLog) > height {
		last := len(v.localInitLog) - 1
		delete(v.initializedLocals, v.localInitLog[last])
		v.localInitLog = v.localInitLog[:last]
	}
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
	if b.Kind() == ValBot || a.Kind() == ValBot {
		return true
	}
	if equalValType(a, b) {
		return true
	}
	if a.Kind() == ValRef && b.Kind() == ValRef {
		return v.refSubtype(a.Ref(), b.Ref())
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
	switch t.Kind() {
	case ValNum:
		switch t.Num() {
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
