package wasm

// CollectorOperandRoots retains only typed collector operands at native call,
// allocation, and EH entry instructions. PCs include the local-declaration
// prefix, as in the native source maps. Function descriptors have a separate
// typed list; opaque extern carriers have their own conditional-root list.
type CollectorOperandRoots struct {
	sites map[uint32]collectorOperandRootSite
}

type collectorOperandRootSite struct {
	collector []uint32
	functions []uint32
	externs   []uint32
}

func (p *CollectorOperandRoots) At(pc uint32) ([]uint32, bool) {
	if p == nil {
		return nil, false
	}
	site, ok := p.sites[pc]
	return site.collector, ok
}

func (p *CollectorOperandRoots) FunctionsAt(pc uint32) ([]uint32, bool) {
	if p == nil {
		return nil, false
	}
	site, ok := p.sites[pc]
	return site.functions, ok
}

func (p *CollectorOperandRoots) ExternsAt(pc uint32) ([]uint32, bool) {
	if p == nil {
		return nil, false
	}
	site, ok := p.sites[pc]
	return site.externs, ok
}

func ExternReferenceType(typ ValType) bool {
	return typ.Kind() == ValRef && typ.Ref().Heap().Kind() == HeapAbs && (typ.Ref().Heap().Abs() == HeapExtern || typ.Ref().Heap().Abs() == HeapNoExtern)
}

func FunctionReferenceType(m *Module, typ ValType) bool {
	if typ.Kind() != ValRef {
		return false
	}
	heap := typ.Ref().Heap()
	if heap.Kind() == HeapAbs {
		return heap.Abs() == HeapFunc || heap.Abs() == HeapNoFunc
	}
	if heap.Kind() != HeapTypeIndex || heap.Type().Rec {
		return false
	}
	sub, ok := m.TypeSubType(heap.Type().Index)
	return ok && sub.Comp.Kind == CompFunc
}

func ModuleFunctionReferenceStorage(m *Module) bool {
	if m == nil {
		return false
	}
	field := func(value FieldType) bool {
		storage := value.Storage()
		return !storage.Packed() && FunctionReferenceType(m, storage.Val())
	}
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if sub.Comp.Kind == CompArray && field(sub.Comp.Array) {
				return true
			}
			if sub.Comp.Kind == CompStruct {
				for _, value := range sub.Comp.Fields {
					if field(value) {
						return true
					}
				}
			}
		}
	}
	return false
}

// ModuleFunctionReferenceCarriers identifies raw function words that can be
// live in a native GC frame. It grants no source or artifact authority. Field
// mutation still uses ModuleFunctionReferenceStorage and its exact type checks.
func ModuleFunctionReferenceCarriers(m *Module) bool {
	if m == nil {
		return false
	}
	if ModuleFunctionReferenceStorage(m) {
		return true
	}
	signature := func(ft *CompType) bool {
		if ft == nil {
			return false
		}
		for _, values := range [][]ValType{ft.Params, ft.Results} {
			for _, value := range values {
				if FunctionReferenceType(m, value) {
					return true
				}
			}
		}
		return false
	}
	for i := range m.Imports {
		im := &m.Imports[i]
		switch im.Type.Kind {
		case ExternFunc:
			ft, ok := m.ImportFuncType(i)
			if ok && signature(ft) {
				return true
			}
		case ExternTable:
			if FunctionReferenceType(m, RefVal(im.Type.TableType().Ref)) {
				return true
			}
		case ExternGlobal:
			if FunctionReferenceType(m, im.Type.GlobalType().Type) {
				return true
			}
		}
	}
	for _, table := range m.Tables {
		if FunctionReferenceType(m, RefVal(table.Type.Ref)) {
			return true
		}
	}
	for _, global := range m.Globals {
		if FunctionReferenceType(m, global.Type.Type) {
			return true
		}
	}
	classifier := NewModuleInstructionClassifier(m, true)
	imports := m.ImportedFuncCount()
	for i := range m.Code {
		ft, ok := m.FuncSignature(uint32(imports + i))
		if ok && signature(ft) {
			return true
		}
		for _, run := range m.Code[i].Locals.Runs {
			if run.Count != 0 && FunctionReferenceType(m, run.Type) {
				return true
			}
		}
		r := NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				break
			}
			if op == 0xd2 {
				return true
			}
			if op == 0xd0 {
				probe := reader{data: r.data, pos: r.pos}
				if heap, err := decodeHeapType(&probe); err == nil && FunctionReferenceType(m, RefVal(Ref(true, heap, false))) {
					return true
				}
			}
			var imm InstructionImmediate
			if classifier.ClassifyInto(r, op, &imm) != nil {
				break
			}
		}
	}
	return false
}

func FunctionStorageMutation(m *Module, kind InstrKind, typeIndex, fieldIndex uint32) bool {
	value, ok := mutableReferenceStorage(m, kind, typeIndex, fieldIndex)
	return ok && FunctionReferenceType(m, value)
}
func ExternStorageMutation(m *Module, kind InstrKind, typeIndex, fieldIndex uint32) bool {
	value, ok := mutableReferenceStorage(m, kind, typeIndex, fieldIndex)
	return ok && ExternReferenceType(value)
}
func mutableReferenceStorage(m *Module, kind InstrKind, typeIndex, fieldIndex uint32) (ValType, bool) {
	if m == nil {
		return ValType{}, false
	}
	switch kind {
	case InstrStructSet, InstrArraySet, InstrArrayFill, InstrArrayInitElem:
	default:
		return ValType{}, false
	}
	sub, ok := m.TypeSubType(typeIndex)
	if !ok {
		return ValType{}, false
	}
	var field FieldType
	if kind == InstrStructSet {
		if sub.Comp.Kind != CompStruct || uint64(fieldIndex) >= uint64(len(sub.Comp.Fields)) {
			return ValType{}, false
		}
		field = sub.Comp.Fields[fieldIndex]
	} else {
		if sub.Comp.Kind != CompArray {
			return ValType{}, false
		}
		field = sub.Comp.Array
	}
	return field.Storage().Val(), field.Mut() == Var && !field.Storage().Packed()
}

// Opaque storage can acquire a traced lifetime anchor at a native helper.
// Function and extern words retain distinct root kinds in the resulting map.
func OpaqueStorageMutation(m *Module, kind InstrKind, typeIndex, fieldIndex uint32) bool {
	return FunctionStorageMutation(m, kind, typeIndex, fieldIndex) || ExternStorageMutation(m, kind, typeIndex, fieldIndex) || ExternPersistentMutation(m, kind, typeIndex, fieldIndex)
}

// ExternPersistentMutation can create a traced anchor for an opaque host
// value before it publishes a native global or table cell.
func ExternPersistentMutation(m *Module, kind InstrKind, index, index2 uint32) bool {
	if m == nil {
		return false
	}
	if kind == InstrGlobalSet {
		g, ok := m.GlobalTypeByIndex(index)
		return ok && g.Mutable && ExternReferenceType(g.Type)
	}
	switch kind {
	case InstrTableInit:
		index = index2
	case InstrTableSet, InstrTableFill, InstrTableGrow, InstrTableCopy:
	default:
		return false
	}
	table, ok := m.TableType(index)
	return ok && ExternReferenceType(RefVal(table.Ref))
}

// FunctionElementSubtype accepts one function constant and checks its exact
// declared reference type. It does not admit global expressions or instructions.
func FunctionElementSubtype(m *Module, expr Expr, required RefType) (FuncrefElementExpr, bool) {
	if m == nil || required.Exact() || !FunctionReferenceType(m, RefVal(required)) || len(expr.Instrs) != 0 {
		return FuncrefElementExpr{}, false
	}
	r := reader{data: expr.BodyBytes}
	op, err := r.byte()
	if err != nil {
		return FuncrefElementExpr{}, false
	}
	var actual RefType
	var out FuncrefElementExpr
	switch op {
	case 0xd0:
		actual, err = decodeRefTypeForNull(&r)
		if err != nil || actual.Exact() || !FunctionReferenceType(m, RefVal(actual)) {
			return out, false
		}
		out.Null = true
	case 0xd2:
		function, err := r.u32()
		if err != nil {
			return out, false
		}
		index, ok := m.FuncTypeIndex(function)
		if !ok || index.Rec {
			return out, false
		}
		if _, ok := m.TypeFunc(index.Index); !ok {
			return out, false
		}
		actual = Ref(false, IndexedHeap(index), false)
		out.FuncIndex = function
	default:
		return out, false
	}
	end, err := r.byte()
	return out, err == nil && end == 0x0b && !r.has() && m.ReferenceTypeSubtype(actual, required)
}

func CollectorReferenceType(m *Module, typ ValType) bool {
	if typ.Kind() != ValRef {
		return false
	}
	heap := typ.Ref().Heap()
	if heap.Kind() == HeapAbs {
		switch heap.Abs() {
		case HeapAny, HeapEq, HeapI31, HeapStruct, HeapArray, HeapNone, HeapExn, HeapNoExn:
			return true
		default:
			return false
		}
	}
	if heap.Kind() != HeapTypeIndex || heap.Type().Rec {
		return false
	}
	sub, ok := m.TypeSubType(heap.Type().Index)
	return ok && (sub.Comp.Kind == CompStruct || sub.Comp.Kind == CompArray)
}

// BuildCollectorOperandRoots uses the ordinary direct type checker. It does not
// infer ownership from encoded reference widths or trust an external mask.
func BuildCollectorOperandRoots(m *Module, localFunction int) (*CollectorOperandRoots, error) {
	if m == nil || localFunction < 0 || localFunction >= len(m.Code) {
		return nil, &ValidationError{Code: ErrUnknownFunc, Func: localFunction}
	}
	m.invalidateTypeAnalysisCaches()
	v := moduleValidator{m: m, funcIndex: -1, features: ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true}, limits: defaultValidationLimits}
	v.ensureImportIndexes()
	if err := v.validateModule(); err != nil {
		return nil, err
	}
	rootCount := 0
	fv := funcValidator{moduleValidator: &v}
	return buildCollectorOperandRoots(&v, &fv, localFunction, &rootCount)
}

// BuildCollectorOperandRootModule validates declarations once and reuses the
// same direct type checker for the selected function bodies.
func BuildCollectorOperandRootModule(m *Module, selected []bool) ([]*CollectorOperandRoots, error) {
	if m == nil || len(selected) != len(m.Code) {
		return nil, &ValidationError{Code: ErrUnknownFunc, Func: -1}
	}
	m.invalidateTypeAnalysisCaches()
	v := moduleValidator{m: m, funcIndex: -1, features: ValidationFeatures{CompactImports: true, MultiMemory: true, ExtendedConstGlobals: true, GCConstExpr: true}, limits: defaultValidationLimits}
	v.ensureImportIndexes()
	if err := v.validateModule(); err != nil {
		return nil, err
	}
	plans := make([]*CollectorOperandRoots, len(m.Code))
	rootCount := 0
	fv := funcValidator{moduleValidator: &v}
	for function, needed := range selected {
		if !needed {
			continue
		}
		plan, err := buildCollectorOperandRoots(&v, &fv, function, &rootCount)
		if err != nil {
			return nil, err
		}
		plans[function] = plan
	}
	return plans, nil
}

func buildCollectorOperandRoots(v *moduleValidator, fv *funcValidator, localFunction int, rootCount *int) (*CollectorOperandRoots, error) {
	m := v.m
	function := len(v.importsOfKind(ExternFunc)) + localFunction
	ft, ok := v.funcType(uint32(function))
	if !ok {
		return nil, v.err(ErrUnknownType, "collector operand function")
	}
	fn := &m.Code[localFunction]
	if len(fn.BodyBytes) == 0 {
		return nil, v.err(ErrTypeMismatch, "collector operands need byte-backed code")
	}
	fv.beginFunc(function)
	fv.localParams, fv.localRuns = ft.Params, fn.Locals.Runs
	var overflow bool
	fv.localCount, overflow = LocalCount(ft.Params, fn.Locals.Runs)
	if overflow || fv.localCount > uint64(v.limits.MaxFunctionLocals) {
		return nil, fv.verr(ErrInvalidLimitRange, "collector operand local count")
	}
	for _, run := range fn.Locals.Runs {
		if err := fv.validateValType(run.Type); err != nil {
			return nil, err
		}
	}
	fv.prepareLocalLookup()
	fv.resetLocalInitialization()
	fv.pushCtrl(ctrlFunc, nil, ft.Results)
	fv.rd.reset(fn.BodyBytes)
	plan := &CollectorOperandRoots{sites: make(map[uint32]collectorOperandRootSite)}
	widths := moduleMemargWidths(m)
	var op directOp
	for len(fv.ctrls) != 0 {
		pc := uint32(fn.LocalDeclBytes) + uint32(fv.rd.off())
		if err := fv.decodeDirectOp(&fv.rd, widths, true, &op); err != nil {
			return nil, err
		}
		selected := op.kind == directTryTable
		if op.kind == directInstr {
			switch op.instr.Kind {
			case InstrCall, InstrCallIndirect, InstrCallRef, InstrReturnCall, InstrReturnCallIndirect, InstrReturnCallRef,
				InstrStructNew, InstrStructNewDefault, InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem, InstrAnyConvertExtern, InstrExternConvertAny:
				selected = true
			}
			selected = selected || OpaqueStorageMutation(m, op.instr.Kind, op.instr.Index, op.instr.Index2)
		}
		if selected {
			var roots, functions, externs []uint32
			for i, value := range fv.vals {
				if !value.unknown && CollectorReferenceType(m, value.t) {
					roots = append(roots, uint32(i))
					(*rootCount)++
				} else if !value.unknown && FunctionReferenceType(m, value.t) {
					functions = append(functions, uint32(i))
					(*rootCount)++
				} else if !value.unknown && ExternReferenceType(value.t) {
					externs = append(externs, uint32(i))
					(*rootCount)++
				}
			}
			if *rootCount > 1<<20 {
				return nil, v.err(ErrInvalidLimitRange, "collector operand root budget")
			}
			plan.sites[pc] = collectorOperandRootSite{collector: roots, functions: functions, externs: externs}
		}
		if err := fv.stepDirectOp(&op); err != nil {
			return nil, err
		}
	}
	if fv.rd.has() {
		return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: fv.rd.off()}
	}
	return plan, nil
}
