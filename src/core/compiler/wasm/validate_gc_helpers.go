package wasm

func (v *moduleValidator) subtypeByTypeIdx(idx TypeIdx) (*SubType, bool) {
	st, _, ok := v.subtypeByTypeIdxWithRecGroup(idx)
	return st, ok
}

func (v *moduleValidator) subtypeByTypeIdxWithRecGroup(idx TypeIdx) (*SubType, int, bool) {
	if idx.Rec {
		return nil, 0, false
	}
	return v.subtypeByFlatTypeIdx(int(idx.Index))
}

func (v *moduleValidator) subtypeByFlatTypeIdx(idx int) (*SubType, int, bool) {
	if idx < 0 {
		return nil, 0, false
	}
	want := idx
	for gi := range v.m.Types {
		rt := &v.m.Types[gi]
		if want < len(rt.SubTypes) {
			return &rt.SubTypes[want], gi, true
		}
		want -= len(rt.SubTypes)
	}
	return nil, 0, false
}

func (v *moduleValidator) validTypeIdx(idx TypeIdx) bool {
	_, ok := v.subtypeByTypeIdx(idx)
	return ok
}

func (v *moduleValidator) subtypeByTypeIdxInRecGroup(idx TypeIdx, recGroup int) (*SubType, bool) {
	if !idx.Rec {
		if recGroup >= 0 {
			// An absolute index inside a type definition may only name a type
			// declared before the current recursive group. References to members
			// of the current group are decoded as Rec indexes; later groups are
			// out of scope even though they exist in the flattened type section.
			base := 0
			for gi := 0; gi < recGroup && gi < len(v.m.Types); gi++ {
				base += len(v.m.Types[gi].SubTypes)
			}
			if int(idx.Index) >= base {
				return nil, false
			}
		}
		return v.subtypeByTypeIdx(idx)
	}
	if recGroup < 0 || recGroup >= len(v.m.Types) || idx.Index >= uint32(len(v.m.Types[recGroup].SubTypes)) {
		return nil, false
	}
	return &v.m.Types[recGroup].SubTypes[idx.Index], true
}

func (v *moduleValidator) validTypeIdxInRecGroup(idx TypeIdx, recGroup int) bool {
	_, ok := v.subtypeByTypeIdxInRecGroup(idx, recGroup)
	return ok
}

type moduleSubTypeRef struct {
	st       *SubType
	recGroup int
}

func (v *moduleValidator) flattenedSubTypeRefs() []moduleSubTypeRef {
	var flat []moduleSubTypeRef
	for gi := range v.m.Types {
		for si := range v.m.Types[gi].SubTypes {
			flat = append(flat, moduleSubTypeRef{st: &v.m.Types[gi].SubTypes[si], recGroup: gi})
		}
	}
	return flat
}

func (v *moduleValidator) flatTypeIdxInRecGroup(idx TypeIdx, recGroup int) (int, bool) {
	if !idx.Rec {
		if !v.validTypeIdx(idx) {
			return 0, false
		}
		return int(idx.Index), true
	}
	if recGroup < 0 || recGroup >= len(v.m.Types) || idx.Index >= uint32(len(v.m.Types[recGroup].SubTypes)) {
		return 0, false
	}
	abs := 0
	for gi := 0; gi < recGroup; gi++ {
		abs += len(v.m.Types[gi].SubTypes)
	}
	return abs + int(idx.Index), true
}

func (v *moduleValidator) validateSubtypeMetadata() error {
	flat := v.flattenedSubTypeRefs()
	for _, cur := range flat {
		for _, supIdx := range cur.st.Supers {
			sup, ok := v.subtypeByTypeIdxInRecGroup(supIdx, cur.recGroup)
			if !ok {
				return v.err(ErrUnknownType, "supertype")
			}
			if sup.Final {
				return v.err(ErrTypeMismatch, "final supertype")
			}
			if cur.st.Comp.Kind != sup.Comp.Kind {
				return v.err(ErrTypeMismatch, "supertype kind")
			}
		}
	}
	state := make([]uint8, len(flat))
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case 1:
			return v.err(ErrTypeMismatch, "cyclic supertype chain")
		case 2:
			return nil
		}
		state[i] = 1
		for _, supIdx := range flat[i].st.Supers {
			sup, ok := v.flatTypeIdxInRecGroup(supIdx, flat[i].recGroup)
			if !ok {
				return v.err(ErrUnknownType, "supertype")
			}
			if err := visit(sup); err != nil {
				return err
			}
		}
		state[i] = 2
		return nil
	}
	for i := range flat {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

func (v *moduleValidator) funcTypeFromTypeIdx(idx TypeIdx) *CompType {
	ct, ok := v.resolvedCompType(idx)
	if !ok || ct.Kind != CompFunc {
		return nil
	}
	return ct
}

func (v *moduleValidator) compTypeFromTypeIdx(idx TypeIdx) (*CompType, bool) {
	return v.resolvedCompType(idx)
}

// resolvedCompType returns the rec-index-resolved CompType for a type index,
// memoized by flat index. The returned pointer is shared and must be treated as
// read-only. Recursive (in-rec-group) indexes have no flat index and resolve to
// no subtype here, exactly as subtypeByTypeIdxWithRecGroup did before caching.
func (v *moduleValidator) resolvedCompType(idx TypeIdx) (*CompType, bool) {
	if idx.Rec {
		return nil, false
	}
	if e, hit := v.compCache[idx.Index]; hit {
		return e.ct, e.ok
	}
	st, recGroup, ok := v.subtypeByTypeIdxWithRecGroup(idx)
	entry := compCacheEntry{ok: ok}
	if ok {
		ct := v.resolveCompTypeRecIndexes(st.Comp, recGroup)
		entry.ct = &ct
	}
	if !v.compCacheFrozen {
		if v.compCache == nil {
			v.compCache = make(map[uint32]compCacheEntry)
		}
		v.compCache[idx.Index] = entry
	}
	return entry.ct, entry.ok
}

func (v *moduleValidator) structFields(idx TypeIdx) ([]FieldType, *SubType, bool) {
	st, recGroup, ok := v.subtypeByTypeIdxWithRecGroup(idx)
	if !ok || st.Comp.Kind != CompStruct {
		return nil, nil, false
	}
	fields := make([]FieldType, len(st.Comp.Fields))
	for i, f := range st.Comp.Fields {
		fields[i] = v.resolveFieldTypeRecIndexes(f, recGroup)
	}
	return fields, st, true
}

func (v *moduleValidator) arrayField(idx TypeIdx) (FieldType, *SubType, bool) {
	st, recGroup, ok := v.subtypeByTypeIdxWithRecGroup(idx)
	if !ok || st.Comp.Kind != CompArray {
		return FieldType{}, nil, false
	}
	return v.resolveFieldTypeRecIndexes(st.Comp.Array, recGroup), st, true
}

func (v *moduleValidator) resolveCompTypeRecIndexes(ct CompType, recGroup int) CompType {
	switch ct.Kind {
	case CompFunc:
		if len(ct.Params) > 0 {
			params := make([]ValType, len(ct.Params))
			for i, t := range ct.Params {
				params[i] = v.resolveValTypeRecIndexes(t, recGroup)
			}
			ct.Params = params
		}
		if len(ct.Results) > 0 {
			results := make([]ValType, len(ct.Results))
			for i, t := range ct.Results {
				results[i] = v.resolveValTypeRecIndexes(t, recGroup)
			}
			ct.Results = results
		}
	case CompStruct:
		if len(ct.Fields) > 0 {
			fields := make([]FieldType, len(ct.Fields))
			for i, f := range ct.Fields {
				fields[i] = v.resolveFieldTypeRecIndexes(f, recGroup)
			}
			ct.Fields = fields
		}
	case CompArray:
		ct.Array = v.resolveFieldTypeRecIndexes(ct.Array, recGroup)
	}
	return ct
}

func (v *moduleValidator) resolveFieldTypeRecIndexes(ft FieldType, recGroup int) FieldType {
	if ft.Storage.Packed {
		return ft
	}
	ft.Storage.Val = v.resolveValTypeRecIndexes(ft.Storage.Val, recGroup)
	return ft
}

func (v *moduleValidator) resolveValTypeRecIndexes(t ValType, recGroup int) ValType {
	if t.Kind != ValRef {
		return t
	}
	t.Ref = v.resolveRefTypeRecIndexes(t.Ref, recGroup)
	return t
}

func (v *moduleValidator) resolveRefTypeRecIndexes(rt RefType, recGroup int) RefType {
	if rt.Heap.Kind != HeapTypeIndex {
		return rt
	}
	rt.Heap.Type = v.resolveTypeIdxRecIndex(rt.Heap.Type, recGroup)
	return rt
}

func (v *moduleValidator) resolveTypeIdxRecIndex(idx TypeIdx, recGroup int) TypeIdx {
	flat, ok := v.flatTypeIdxInRecGroup(idx, recGroup)
	if !ok {
		return idx
	}
	return TypeIdx{Index: uint32(flat)}
}

func storageValType(st StorageType, signedGet bool) ValType {
	if !st.Packed {
		return st.Val
	}
	_ = signedGet
	return I32
}

func valTypeDefaultable(t ValType) bool {
	return t.Kind != ValRef || t.Ref.Nullable
}

func (v *moduleValidator) descriptorTargetRefType(nullable bool, ht HeapType, exact bool) (ValType, bool) {
	if ht.Kind == HeapTypeIndex {
		if _, ok := v.subtypeByTypeIdx(ht.Type); !ok {
			return ValType{}, false
		}
	}
	return RefVal(Ref(nullable, ht, exact)), true
}

func (v *moduleValidator) typeIdxSuperSubtype(a, b TypeIdx) bool {
	aFlat, aok := v.flatTypeIdxInRecGroup(a, -1)
	bFlat, bok := v.flatTypeIdxInRecGroup(b, -1)
	if !aok || !bok {
		return false
	}
	seen := map[int]bool{}
	var visit func(int) bool
	visit = func(cur int) bool {
		if cur == bFlat {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		st, recGroup, ok := v.subtypeByFlatTypeIdx(cur)
		if !ok {
			return false
		}
		for _, sup := range st.Supers {
			supFlat, ok := v.flatTypeIdxInRecGroup(sup, recGroup)
			if ok && visit(supFlat) {
				return true
			}
		}
		return false
	}
	return visit(aFlat)
}

type refTestFamily uint8

const (
	refTestFamilyData refTestFamily = iota + 1
	refTestFamilyFunc
	refTestFamilyExtern
	refTestFamilyExn
	refTestFamilyString
)

// refTestCompatible implements the cast-hierarchy match used by ref.test.
// Defined siblings remain valid test operands even when neither is a subtype of
// the other: the dynamic result is simply false. Disjoint top-level reference
// hierarchies (for example func and i31/data) remain validation errors.
func (v *moduleValidator) refTestCompatible(a, b RefType) bool {
	af, aok := v.refTestHeapFamily(a.Heap)
	bf, bok := v.refTestHeapFamily(b.Heap)
	return aok && bok && af == bf
}

func (v *moduleValidator) refTestHeapFamily(h HeapType) (refTestFamily, bool) {
	if h.Kind == HeapAbs {
		switch h.Abs {
		case HeapNone, HeapI31, HeapStruct, HeapArray, HeapEq, HeapAny:
			return refTestFamilyData, true
		case HeapNoFunc, HeapFunc:
			return refTestFamilyFunc, true
		case HeapNoExtern, HeapExtern:
			return refTestFamilyExtern, true
		case HeapNoExn, HeapExn:
			return refTestFamilyExn, true
		case HeapString:
			return refTestFamilyString, true
		default:
			return 0, false
		}
	}
	var kind CompTypeKind
	switch h.Kind {
	case HeapTypeIndex:
		ct, ok := v.compTypeFromTypeIdx(h.Type)
		if !ok {
			return 0, false
		}
		kind = ct.Kind
	case HeapDefType:
		if h.Def == nil || h.Def.Index >= uint32(len(h.Def.Rec.SubTypes)) {
			return 0, false
		}
		kind = h.Def.Rec.SubTypes[h.Def.Index].Comp.Kind
	default:
		return 0, false
	}
	switch kind {
	case CompStruct, CompArray:
		return refTestFamilyData, true
	case CompFunc:
		return refTestFamilyFunc, true
	default:
		return 0, false
	}
}

func (v *moduleValidator) descriptorCompatible(a, b RefType) bool {
	if a.Heap.Kind == HeapAbs && b.Heap.Kind == HeapAbs {
		return absHeapSubtype(a.Heap.Abs, b.Heap.Abs) || absHeapSubtype(b.Heap.Abs, a.Heap.Abs)
	}
	if a.Exact && b.Exact && a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapTypeIndex {
		return v.typeIdxEquivalent(a.Heap.Type, b.Heap.Type)
	}
	return v.refSubtype(a, b) || v.refSubtype(b, a)
}

func (v *moduleValidator) refSubtype(a, b RefType) bool {
	if !b.Nullable && a.Nullable {
		return false
	}
	if v.heapTypeEquivalent(a.Heap, b.Heap) {
		if b.Exact && !a.Exact {
			return false
		}
		return true
	}
	if a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapTypeIndex {
		return !b.Exact && v.typeIdxSuperSubtype(a.Heap.Type, b.Heap.Type)
	}
	if a.Heap.Kind == HeapAbs && b.Heap.Kind == HeapTypeIndex {
		ct, ok := v.compTypeFromTypeIdx(b.Heap.Type)
		if !ok {
			return false
		}
		switch ct.Kind {
		case CompFunc:
			return a.Heap.Abs == HeapNoFunc
		case CompStruct, CompArray:
			return a.Heap.Abs == HeapNone
		}
	}
	if a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapAbs {
		ct, ok := v.compTypeFromTypeIdx(a.Heap.Type)
		if !ok {
			return false
		}
		switch ct.Kind {
		case CompStruct:
			return absHeapSubtype(HeapStruct, b.Heap.Abs)
		case CompArray:
			return absHeapSubtype(HeapArray, b.Heap.Abs)
		case CompFunc:
			return absHeapSubtype(HeapFunc, b.Heap.Abs)
		}
	}
	if a.Heap.Kind == HeapAbs && b.Heap.Kind == HeapAbs {
		return absHeapSubtype(a.Heap.Abs, b.Heap.Abs)
	}
	return false
}

func (v *moduleValidator) heapSubtype(a, b HeapType) bool {
	if v.heapTypeEquivalent(a, b) {
		return true
	}
	return v.refSubtype(Ref(false, a, false), Ref(false, b, false))
}

// typeIdxEquivalent implements the Core 3.0 structural equivalence relation for
// defined types. The pair-state map makes recursive comparison coinductive and
// bounds work by the number of type pairs reachable from the two roots.
func (v *moduleValidator) typeIdxEquivalent(a, b TypeIdx) bool {
	aFlat, aok := v.flatTypeIdxInRecGroup(a, -1)
	bFlat, bok := v.flatTypeIdxInRecGroup(b, -1)
	if !aok || !bok {
		return false
	}
	type pair struct{ a, b int }
	state := make(map[pair]uint8)
	var eqType func(int, int) bool
	var eqVal func(ValType, ValType, int, int) bool
	var eqHeap func(HeapType, HeapType, int, int) bool
	eqHeap = func(x, y HeapType, xGroup, yGroup int) bool {
		if x.Kind != y.Kind {
			return false
		}
		switch x.Kind {
		case HeapAbs:
			return x.Abs == y.Abs
		case HeapTypeIndex:
			xi, xok := v.flatTypeIdxInRecGroup(x.Type, xGroup)
			yi, yok := v.flatTypeIdxInRecGroup(y.Type, yGroup)
			return xok && yok && eqType(xi, yi)
		case HeapDefType:
			if x.Def == nil || y.Def == nil {
				return x.Def == y.Def
			}
			return x.Def.GroupIndex == y.Def.GroupIndex && x.Def.Index == y.Def.Index
		default:
			return false
		}
	}
	eqVal = func(x, y ValType, xGroup, yGroup int) bool {
		if x.Kind != y.Kind || x.Num != y.Num {
			return false
		}
		if x.Kind != ValRef {
			return true
		}
		return x.Ref.Nullable == y.Ref.Nullable && x.Ref.Exact == y.Ref.Exact && eqHeap(x.Ref.Heap, y.Ref.Heap, xGroup, yGroup)
	}
	eqStorage := func(x, y StorageType, xGroup, yGroup int) bool {
		if x.Packed != y.Packed || x.Pack != y.Pack {
			return false
		}
		return x.Packed || eqVal(x.Val, y.Val, xGroup, yGroup)
	}
	eqField := func(x, y FieldType, xGroup, yGroup int) bool {
		return x.Mut == y.Mut && eqStorage(x.Storage, y.Storage, xGroup, yGroup)
	}
	groupLocation := func(flat int) (group, local, base int, ok bool) {
		if flat < 0 {
			return 0, 0, 0, false
		}
		base = 0
		for gi := range v.m.Types {
			n := len(v.m.Types[gi].SubTypes)
			if flat < base+n {
				return gi, flat - base, base, true
			}
			base += n
		}
		return 0, 0, 0, false
	}
	eqType = func(x, y int) bool {
		if x == y {
			return true
		}
		xGroupLoc, xLocal, xBase, xLocOK := groupLocation(x)
		yGroupLoc, yLocal, yBase, yLocOK := groupLocation(y)
		if !xLocOK || !yLocOK || xLocal != yLocal || len(v.m.Types[xGroupLoc].SubTypes) != len(v.m.Types[yGroupLoc].SubTypes) {
			return false
		}
		p := pair{x, y}
		switch state[p] {
		case 1, 2:
			return true
		case 3:
			return false
		}
		state[p] = 1
		xs, xGroup, xok := v.subtypeByFlatTypeIdx(x)
		ys, yGroup, yok := v.subtypeByFlatTypeIdx(y)
		ok := xok && yok && xs.Final == ys.Final && len(xs.Supers) == len(ys.Supers) && xs.Comp.Kind == ys.Comp.Kind
		// Recursive type equivalence is defined over whole groups, not only
		// the graph reachable from one projection. A projected type from a
		// two-member group is therefore not equivalent to an identical
		// singleton implicit function type.
		if ok {
			for i := range v.m.Types[xGroupLoc].SubTypes {
				if !eqType(xBase+i, yBase+i) {
					ok = false
					break
				}
			}
		}
		eqOptionalType := func(x, y *TypeIdx) bool {
			if x == nil || y == nil {
				return x == nil && y == nil
			}
			xi, xok := v.flatTypeIdxInRecGroup(*x, xGroup)
			yi, yok := v.flatTypeIdxInRecGroup(*y, yGroup)
			return xok && yok && eqType(xi, yi)
		}
		if ok {
			ok = eqOptionalType(xs.Metadata.Describes, ys.Metadata.Describes) && eqOptionalType(xs.Metadata.Descriptor, ys.Metadata.Descriptor)
		}
		if ok {
			for i := range xs.Supers {
				xi, xok := v.flatTypeIdxInRecGroup(xs.Supers[i], xGroup)
				yi, yok := v.flatTypeIdxInRecGroup(ys.Supers[i], yGroup)
				if !xok || !yok || !eqType(xi, yi) {
					ok = false
					break
				}
			}
		}
		if ok {
			x, y := xs.Comp, ys.Comp
			switch x.Kind {
			case CompFunc:
				ok = len(x.Params) == len(y.Params) && len(x.Results) == len(y.Results)
				for i := 0; ok && i < len(x.Params); i++ {
					ok = eqVal(x.Params[i], y.Params[i], xGroup, yGroup)
				}
				for i := 0; ok && i < len(x.Results); i++ {
					ok = eqVal(x.Results[i], y.Results[i], xGroup, yGroup)
				}
			case CompStruct:
				ok = len(x.Fields) == len(y.Fields)
				for i := 0; ok && i < len(x.Fields); i++ {
					ok = eqField(x.Fields[i], y.Fields[i], xGroup, yGroup)
				}
			case CompArray:
				ok = eqField(x.Array, y.Array, xGroup, yGroup)
			default:
				ok = false
			}
		}
		if ok {
			state[p] = 2
		} else {
			state[p] = 3
		}
		return ok
	}
	return eqType(aFlat, bFlat)
}

func (v *moduleValidator) heapTypeEquivalent(a, b HeapType) bool {
	if equalHeapType(a, b) {
		return true
	}
	return a.Kind == HeapTypeIndex && b.Kind == HeapTypeIndex && v.typeIdxEquivalent(a.Type, b.Type)
}
