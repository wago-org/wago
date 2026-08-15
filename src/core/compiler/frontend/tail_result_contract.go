package frontend

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TailResultSite identifies one proper-tail result contract. Caller is a global
// function index. Target is a global function index for return_call and a type
// index for return_call_indirect/return_call_ref. Table is meaningful only for
// return_call_indirect. Ordinal is the site's stable order within Caller.
type TailResultSite struct {
	Caller        uint32
	Ordinal       uint32
	Kind          wasm.InstrKind
	Target        uint32
	Table         uint32
	KnownFunction uint32
	Known         bool
}

// AnalyzeTailResultSites returns every proper-tail site in deterministic module
// and instruction order. A return_call_ref site is marked Known only for the
// deliberately narrow, mechanically evident ref.func-immediately-before-call
// shape. Other reference producers remain dynamic and conservative.
func AnalyzeTailResultSites(m *wasm.Module) ([]TailResultSite, error) {
	return analyzeTailResultSitesWithFeatures(m, wasm.ValidationFeatures{})
}

func analyzeTailResultSitesWithFeatures(m *wasm.Module, features wasm.ValidationFeatures) ([]TailResultSite, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	imports := m.ImportedFuncCount()
	memarg64 := moduleMemargOffset64(m)
	var sites []TailResultSite
	for local := range m.Code {
		caller := uint32(imports + local)
		ordinal := uint32(0)
		fn := &m.Code[local]
		if len(fn.BodyBytes) != 0 {
			if err := scanTailResultByteBody(fn.BodyBytes, caller, &ordinal, &sites, nil, memarg64, features.MultiMemory); err != nil {
				return nil, fmt.Errorf("function %d tail-result scan: %w", caller, err)
			}
			continue
		}
		scanTailResultInstrs(fn.Body.Instrs, caller, &ordinal, &sites, nil)
	}
	return sites, nil
}

// ValidateTailResultRewrite is the post-transform fence for optimizations that
// alter function result signatures while retaining the same proper-tail sites.
// It validates the transformed module and then enforces the target-knowledge
// boundary that ordinary Wasm validation cannot express:
//   - return_call has one statically known target;
//   - return_call_ref may change its selected result contract only when the
//     callee is the immediately preceding ref.func in both modules;
//   - return_call_indirect may change its selected result contract only for a
//     private immutable local table whose complete non-null target set is known,
//     local, unchanged, and rewritten to the selected type.
//
// Dynamic/imported/exported/mutable target sets fail closed. Callers may widen a
// result contract without a target proof when the selected target/type contract
// itself is unchanged; ValidateModule proves the resulting covariance.
func ValidateTailResultRewrite(before, after *wasm.Module) error {
	return ValidateTailResultRewriteWithFeatures(before, after, wasm.ValidationFeatures{})
}

// ValidateTailResultRewriteWithFeatures is ValidateTailResultRewrite under the
// caller's exact staged validation feature set. Signature-changing transforms in
// the production pipeline must pass through the same feature set used before the
// transform so unrelated multi-memory or constant-expression syntax is not
// rejected by the compatibility default.
func ValidateTailResultRewriteWithFeatures(before, after *wasm.Module, features wasm.ValidationFeatures) error {
	if before == nil || after == nil {
		return fmt.Errorf("tail-result rewrite requires non-nil modules")
	}
	if before == after {
		return fmt.Errorf("tail-result rewrite requires a distinct retained source module")
	}
	if err := wasm.ValidateModuleWithFeatures(before, features); err != nil {
		return fmt.Errorf("tail-result rewrite source is invalid: %w", err)
	}
	if err := wasm.ValidateModuleWithFeatures(after, features); err != nil {
		return fmt.Errorf("tail-result rewrite result is invalid: %w", err)
	}
	if before.FuncCount() != after.FuncCount() || len(before.Code) != len(after.Code) || before.ImportedFuncCount() != after.ImportedFuncCount() {
		return fmt.Errorf("tail-result rewrite changed the function index space")
	}
	if err := validateObservableTailRewriteSignatures(before, after); err != nil {
		return err
	}

	beforeSites, err := analyzeTailResultSitesWithFeatures(before, features)
	if err != nil {
		return err
	}
	afterSites, err := analyzeTailResultSitesWithFeatures(after, features)
	if err != nil {
		return err
	}
	if !slices.Equal(beforeSites, afterSites) {
		return fmt.Errorf("tail-result rewrite changed proper-tail site identity or target")
	}

	for _, site := range beforeSites {
		if !functionParameterContractEqual(before, site.Caller, after, site.Caller) || !tailTargetParameterContractEqual(before, after, site) {
			return fmt.Errorf("tail-result rewrite changed parameters at caller %d site %d", site.Caller, site.Ordinal)
		}
		// A caller-only result change is fully checked by ValidateModule when the
		// complete selected target contract, including referenced recursive/GC
		// type graphs, remains structurally identical.
		if tailTargetContractEqual(before, after, site) {
			continue
		}

		switch site.Kind {
		case wasm.InstrReturnCall:
			// The target is exact and after-validation checks the coordinated edge.
			continue
		case wasm.InstrReturnCallRef:
			if !site.Known || site.KnownFunction < uint32(before.ImportedFuncCount()) {
				return fmt.Errorf("return_call_ref at caller %d site %d has a dynamic or imported target", site.Caller, site.Ordinal)
			}
			if !functionFlowsToType(before, site.KnownFunction, site.Target) || !functionFlowsToType(after, site.KnownFunction, site.Target) {
				return fmt.Errorf("return_call_ref at caller %d site %d did not rewrite its known target with type %d", site.Caller, site.Ordinal, site.Target)
			}
		case wasm.InstrReturnCallIndirect:
			beforeTargets, ok := immutableLocalTableTargets(before, site.Table, features.MultiMemory)
			if !ok {
				return fmt.Errorf("return_call_indirect at caller %d site %d has an unknown target set", site.Caller, site.Ordinal)
			}
			afterTargets, ok := immutableLocalTableTargets(after, site.Table, features.MultiMemory)
			if !ok || !slices.Equal(beforeTargets, afterTargets) {
				return fmt.Errorf("return_call_indirect at caller %d site %d changed or lost its proven target set", site.Caller, site.Ordinal)
			}
			if len(beforeTargets) == 0 {
				return fmt.Errorf("return_call_indirect at caller %d site %d has no proven non-null target", site.Caller, site.Ordinal)
			}
			for _, target := range beforeTargets {
				if target < uint32(before.ImportedFuncCount()) || !functionExactlyMatchesType(before, target, site.Target) || !functionExactlyMatchesType(after, target, site.Target) {
					return fmt.Errorf("return_call_indirect at caller %d site %d did not rewrite local target %d with type %d", site.Caller, site.Ordinal, target, site.Target)
				}
			}
		default:
			return fmt.Errorf("caller %d site %d is not a proper tail", site.Caller, site.Ordinal)
		}
	}
	return nil
}

func validateObservableTailRewriteSignatures(before, after *wasm.Module) error {
	for i := 0; i < before.ImportedFuncCount(); i++ {
		if !functionSignatureEqual(before, uint32(i), after, uint32(i)) {
			return fmt.Errorf("tail-result rewrite changed imported function %d", i)
		}
	}
	if len(before.Exports) != len(after.Exports) {
		return fmt.Errorf("tail-result rewrite changed exports")
	}
	for i := range before.Exports {
		beforeExport, afterExport := before.Exports[i], after.Exports[i]
		if beforeExport != afterExport {
			return fmt.Errorf("tail-result rewrite changed export %d", i)
		}
		if beforeExport.Index.Kind != wasm.ExternFunc {
			continue
		}
		if !functionSignatureEqual(before, beforeExport.Index.Index, after, afterExport.Index.Index) {
			return fmt.Errorf("tail-result rewrite changed exported function %q", beforeExport.Name)
		}
	}
	if (before.Start == nil) != (after.Start == nil) {
		return fmt.Errorf("tail-result rewrite changed start function")
	}
	if before.Start != nil {
		if *before.Start != *after.Start {
			return fmt.Errorf("tail-result rewrite changed start function")
		}
		if !functionSignatureEqual(before, uint32(*before.Start), after, uint32(*after.Start)) {
			return fmt.Errorf("tail-result rewrite changed start function signature")
		}
	}
	if moduleImportsFunctionReferences(before) || moduleImportsFunctionReferences(after) {
		imports := before.ImportedFuncCount()
		for local := range before.Code {
			function := uint32(imports + local)
			if !functionSignatureEqual(before, function, after, function) {
				return fmt.Errorf("tail-result rewrite changed function %d observable through imported function references", function)
			}
		}
	}
	if moduleExportsFunctionReferences(before) || moduleExportsFunctionReferences(after) {
		imports := before.ImportedFuncCount()
		for local := range before.Code {
			function := uint32(imports + local)
			if !functionSignatureEqual(before, function, after, function) {
				return fmt.Errorf("tail-result rewrite changed function %d observable through exported function references", function)
			}
		}
	}
	return nil
}

func moduleImportsFunctionReferences(m *wasm.Module) bool {
	for i := range m.Imports {
		im := m.Imports[i].Type
		switch im.Kind {
		case wasm.ExternTable:
			if refTypeMayContainFunction(m, im.TableType().Ref) {
				return true
			}
		case wasm.ExternGlobal:
			// Even an immutable imported reference may name a host-owned GC object
			// with mutable fields into which this module can store a local function.
			// Scalar immutable funcrefs are harmless by themselves, but separating
			// them from interior-mutable and externalized values requires flow and
			// heap-shape analysis; fail closed for every function-capable reference.
			if valTypeMayContainFunction(m, im.GlobalType().Type) {
				return true
			}
		case wasm.ExternFunc:
			typeIndex := im.FuncType()
			if typeIndex.Rec {
				return true
			}
			sig, ok := m.ResolvedTypeFunc(typeIndex.Index)
			if !ok || functionBoundaryMayContainReferences(m, sig) {
				return true
			}
		case wasm.ExternTag:
			// Imported exception tags are host-owned payload sinks. Treat every
			// imported tag as observable rather than depending on payload flow
			// analysis in this transform-only fence.
			return true
		}
	}
	return false
}

func moduleExportsFunctionReferences(m *wasm.Module) bool {
	for _, ex := range m.Exports {
		switch ex.Index.Kind {
		case wasm.ExternTable:
			table, ok := m.TableType(ex.Index.Index)
			if !ok || refTypeMayContainFunction(m, table.Ref) {
				return true
			}
		case wasm.ExternGlobal:
			global, ok := m.GlobalTypeByIndex(ex.Index.Index)
			if !ok || valTypeMayContainFunction(m, global.Type) {
				return true
			}
		case wasm.ExternFunc:
			typeIndex, ok := m.FuncTypeIndex(ex.Index.Index)
			if !ok || typeIndex.Rec {
				return true
			}
			sig, ok := m.ResolvedTypeFunc(typeIndex.Index)
			if !ok || functionBoundaryMayContainReferences(m, sig) {
				return true
			}
		case wasm.ExternTag:
			// An exported exception payload may carry a function reference. Tag
			// lookup is intentionally avoided here: treating every exported tag as
			// an escape keeps this transform-only fence conservative.
			return true
		}
	}
	return false
}

func functionBoundaryMayContainReferences(m *wasm.Module, sig *wasm.CompType) bool {
	for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
		for _, value := range values {
			if valTypeMayContainFunction(m, value) {
				return true
			}
		}
	}
	return false
}

func valTypeMayContainFunction(m *wasm.Module, t wasm.ValType) bool {
	return t.Kind() == wasm.ValRef && refTypeMayContainFunction(m, t.Ref())
}

func refTypeMayContainFunction(_ *wasm.Module, ref wasm.RefType) bool {
	heap := ref.Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapExtern, wasm.HeapNoExtern, wasm.HeapAny, wasm.HeapEq, wasm.HeapStruct, wasm.HeapArray, wasm.HeapExn:
			return true
		default:
			return false
		}
	case wasm.HeapTypeIndex, wasm.HeapDefType:
		// A defined function is directly callable, while a defined struct or
		// array may carry a function reference in a nested field. Proving the
		// absence of such fields would require whole-value flow analysis, so the
		// transform fence deliberately treats every defined reference as capable
		// of carrying an observable function.
		return true
	default:
		return true
	}
}

func functionSignatureEqual(a *wasm.Module, aFunction uint32, b *wasm.Module, bFunction uint32) bool {
	aType, ok := a.FuncTypeIndex(aFunction)
	if !ok || aType.Rec {
		return false
	}
	bType, ok := b.FuncTypeIndex(bFunction)
	if !ok || bType.Rec {
		return false
	}
	aKey, ok := a.StructuralTypeKeyChecked(aType.Index)
	if !ok {
		return false
	}
	bKey, ok := b.StructuralTypeKeyChecked(bType.Index)
	return ok && aKey == bKey
}

func tailTargetContractEqual(before, after *wasm.Module, site TailResultSite) bool {
	if site.Kind == wasm.InstrReturnCall {
		return functionSignatureEqual(before, site.Target, after, site.Target)
	}
	beforeKey, beforeOK := before.StructuralTypeKeyChecked(site.Target)
	afterKey, afterOK := after.StructuralTypeKeyChecked(site.Target)
	return beforeOK && afterOK && beforeKey == afterKey
}

func functionParameterContractEqual(a *wasm.Module, aFunction uint32, b *wasm.Module, bFunction uint32) bool {
	aType, ok := a.FuncTypeIndex(aFunction)
	if !ok || aType.Rec {
		return false
	}
	bType, ok := b.FuncTypeIndex(bFunction)
	return ok && !bType.Rec && typeParameterContractEqual(a, aType.Index, b, bType.Index)
}

func tailTargetParameterContractEqual(before, after *wasm.Module, site TailResultSite) bool {
	if site.Kind == wasm.InstrReturnCall {
		return functionParameterContractEqual(before, site.Target, after, site.Target)
	}
	return typeParameterContractEqual(before, site.Target, after, site.Target)
}

func typeParameterContractEqual(a *wasm.Module, aType uint32, b *wasm.Module, bType uint32) bool {
	aKey, ok := parameterOnlyTypeKey(a, aType)
	if !ok {
		return false
	}
	bKey, ok := parameterOnlyTypeKey(b, bType)
	return ok && aKey == bKey
}

type parameterContractKey struct {
	root   uint64
	nested uint64
}

func parameterOnlyTypeKey(m *wasm.Module, typeIndex uint32) (parameterContractKey, bool) {
	// The root projection preserves subtype metadata and the complete parameter
	// graph while erasing function results. It intentionally erases every result
	// list so a coordinated root result rewrite does not perturb recursive-group
	// digests through an unrelated sibling.
	types := make([]wasm.RecType, len(m.Types))
	flatCount := uint32(0)
	for group := range m.Types {
		types[group] = m.Types[group]
		types[group].SubTypes = append([]wasm.SubType(nil), m.Types[group].SubTypes...)
		flatCount += uint32(len(types[group].SubTypes))
		for member := range types[group].SubTypes {
			if types[group].SubTypes[member].Comp.Kind == wasm.CompFunc {
				types[group].SubTypes[member].Comp.Results = nil
			}
		}
	}
	rootProjection := wasm.Module{Types: types}
	rootKey, ok := rootProjection.StructuralTypeKeyChecked(typeIndex)
	if !ok {
		return parameterContractKey{}, false
	}

	// A synthetic result-less wrapper points at the original, unmodified type
	// graph. This keeps result lists of function types reachable from parameters
	// observable, including a recursive reference back to the selected root.
	// Moving the wrapper to a fresh group requires flattened parameter indexes.
	sig, ok := m.ResolvedTypeFunc(typeIndex)
	if !ok {
		return parameterContractKey{}, false
	}
	wrapper := wasm.SubType{Final: true, Comp: wasm.CompType{
		Kind:   wasm.CompFunc,
		Params: append([]wasm.ValType(nil), sig.Params...),
	}}
	fullTypes := append([]wasm.RecType(nil), m.Types...)
	fullTypes = append(fullTypes, wasm.RecType{SubTypes: []wasm.SubType{wrapper}})
	nestedProjection := wasm.Module{Types: fullTypes}
	nestedKey, ok := nestedProjection.StructuralTypeKeyChecked(flatCount)
	return parameterContractKey{root: rootKey, nested: nestedKey}, ok
}

func functionExactlyMatchesType(m *wasm.Module, function, typeIndex uint32) bool {
	declared, ok := m.FuncTypeIndex(function)
	if !ok || declared.Rec {
		return false
	}
	functionKey, ok := m.StructuralTypeKeyChecked(declared.Index)
	if !ok {
		return false
	}
	typeKey, ok := m.StructuralTypeKeyChecked(typeIndex)
	return ok && functionKey == typeKey
}

func functionFlowsToType(m *wasm.Module, function, typeIndex uint32) bool {
	declared, ok := m.FuncTypeIndex(function)
	if !ok || declared.Rec {
		return false
	}
	actual := wasm.Ref(false, wasm.IndexedHeap(declared), false)
	required := wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false)
	return m.ReferenceTypeSubtype(actual, required)
}

func scanTailResultByteBody(body []byte, caller uint32, ordinal *uint32, sites *[]TailResultSite, mutatedTables map[uint32]bool, memarg64, multiMemory bool) error {
	r := wasm.NewReader(body)
	var previous wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		var imm wasm.InstructionImmediate
		if err := wasm.ClassifyInstructionImmediateIntoWithFeatures(r, op, &imm, memarg64, multiMemory); err != nil {
			return err
		}
		markMutatedTable(imm.Kind, uint32(imm.Index), uint32(imm.Index2), mutatedTables)
		if sites != nil {
			switch imm.Kind {
			case wasm.InstrReturnCall:
				*sites = append(*sites, TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: imm.Kind, Target: uint32(imm.Index), Known: true, KnownFunction: uint32(imm.Index)})
				(*ordinal)++
			case wasm.InstrReturnCallIndirect:
				*sites = append(*sites, TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: imm.Kind, Target: uint32(imm.Index), Table: uint32(imm.Index2)})
				(*ordinal)++
			case wasm.InstrReturnCallRef:
				site := TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: imm.Kind, Target: uint32(imm.Index)}
				if previous.Kind == wasm.InstrRefFunc {
					site.Known = true
					site.KnownFunction = uint32(previous.Index)
				}
				*sites = append(*sites, site)
				(*ordinal)++
			}
		}
		previous = imm
	}
	return nil
}

func scanTailResultInstrs(instrs []wasm.Instruction, caller uint32, ordinal *uint32, sites *[]TailResultSite, mutatedTables map[uint32]bool) {
	var previous wasm.Instruction
	for i := range instrs {
		in := instrs[i]
		markMutatedTable(in.Kind, in.Index, in.Index2, mutatedTables)
		if sites != nil {
			switch in.Kind {
			case wasm.InstrReturnCall:
				*sites = append(*sites, TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: in.Kind, Target: in.Index, Known: true, KnownFunction: in.Index})
				(*ordinal)++
			case wasm.InstrReturnCallIndirect:
				*sites = append(*sites, TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: in.Kind, Target: in.Index, Table: in.Index2})
				(*ordinal)++
			case wasm.InstrReturnCallRef:
				site := TailResultSite{Caller: caller, Ordinal: *ordinal, Kind: in.Kind, Target: in.Index}
				if previous.Kind == wasm.InstrRefFunc {
					site.Known = true
					site.KnownFunction = previous.Index
				}
				*sites = append(*sites, site)
				(*ordinal)++
			}
		}
		scanTailResultInstrs(in.Body().Instrs, caller, ordinal, sites, mutatedTables)
		scanTailResultInstrs(in.Then(), caller, ordinal, sites, mutatedTables)
		scanTailResultInstrs(in.Else(), caller, ordinal, sites, mutatedTables)
		previous = in
	}
}

func markMutatedTable(kind wasm.InstrKind, index, index2 uint32, mutated map[uint32]bool) {
	if mutated == nil {
		return
	}
	switch kind {
	case wasm.InstrTableSet, wasm.InstrTableGrow, wasm.InstrTableFill:
		mutated[index] = true
	case wasm.InstrTableInit:
		mutated[index2] = true
	case wasm.InstrTableCopy:
		// Conservatively treat both tables as mutable; one is the destination and
		// rejecting the source as well keeps the proof independent of immediate
		// field naming conventions.
		mutated[index] = true
		mutated[index2] = true
	}
}

func immutableLocalTableTargets(m *wasm.Module, table uint32, multiMemory bool) ([]uint32, bool) {
	imports := uint32(m.ImportedTableCount())
	if table < imports || int(table) >= m.TableCount() {
		return nil, false
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind == wasm.ExternTable && ex.Index.Index == table {
			return nil, false
		}
	}
	mutated := make(map[uint32]bool)
	memarg64 := moduleMemargOffset64(m)
	for i := range m.Code {
		fn := &m.Code[i]
		ordinal := uint32(0)
		if len(fn.BodyBytes) != 0 {
			if err := scanTailResultByteBody(fn.BodyBytes, uint32(m.ImportedFuncCount()+i), &ordinal, nil, mutated, memarg64, multiMemory); err != nil {
				return nil, false
			}
		} else {
			scanTailResultInstrs(fn.Body.Instrs, uint32(m.ImportedFuncCount()+i), &ordinal, nil, mutated)
		}
	}
	if mutated[table] {
		return nil, false
	}

	seen := make(map[uint32]struct{})
	add := func(index uint32) { seen[index] = struct{}{} }
	localTable := int(table - imports)
	if localTable < 0 || localTable >= len(m.Tables) {
		return nil, false
	}
	if init := m.Tables[localTable].Init; init != nil {
		if !collectConstRefFuncs(*init, add) {
			return nil, false
		}
	}
	for i := range m.Elements {
		elem := &m.Elements[i]
		if elem.Mode.Kind != wasm.ElemActive || uint32(elem.Mode.Table) != table {
			continue
		}
		if elem.Kind.Kind == wasm.ElemFuncs {
			for _, index := range elem.Kind.Funcs {
				add(uint32(index))
			}
			continue
		}
		for _, expr := range elem.Kind.Exprs {
			if !collectConstRefFuncs(expr, add) {
				return nil, false
			}
		}
	}
	targets := make([]uint32, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	return targets, true
}

func moduleMemargOffset64(m *wasm.Module) bool {
	if m == nil || m.MemCount() != 1 {
		return false
	}
	memory, ok := m.MemoryType(0)
	return ok && memory.Limits.Addr64
}

func collectConstRefFuncs(expr wasm.Expr, add func(uint32)) bool {
	if len(expr.BodyBytes) == 0 {
		for i := range expr.Instrs {
			switch expr.Instrs[i].Kind {
			case wasm.InstrRefFunc:
				add(expr.Instrs[i].Index)
			case wasm.InstrRefNull:
			default:
				// Imported global.get and future reference-producing constant
				// instructions make the target identity dynamic.
				return false
			}
		}
		return true
	}
	r := wasm.NewReader(expr.BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x0b {
			return r.BytesLeft() == 0
		}
		var imm wasm.InstructionImmediate
		if wasm.ClassifyInstructionImmediateInto(r, op, &imm) != nil {
			return false
		}
		switch imm.Kind {
		case wasm.InstrRefFunc:
			add(uint32(imm.Index))
		case wasm.InstrRefNull:
		default:
			return false
		}
	}
	return false
}
