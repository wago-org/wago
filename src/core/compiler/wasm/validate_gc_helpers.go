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
	v.ensureTypeIndex()
	if idx < 0 || idx >= len(v.flatSubTypes) {
		return nil, 0, false
	}
	ref := v.flatSubTypes[idx]
	return ref.st, ref.recGroup, true
}

func (v *moduleValidator) validTypeIdx(idx TypeIdx) bool {
	_, ok := v.subtypeByTypeIdx(idx)
	return ok
}

func (v *moduleValidator) subtypeByTypeIdxInRecGroup(idx TypeIdx, recGroup int) (*SubType, bool) {
	if !idx.Rec {
		if recGroup >= 0 {
			if recGroup >= len(v.m.Types) {
				return nil, false
			}
			// An absolute index inside a type definition may only name a type
			// declared before the current recursive group. References to members
			// of the current group are decoded as Rec indexes; later groups are
			// out of scope even though they exist in the flattened type section.
			v.ensureTypeIndex()
			base := v.typeGroupBases[recGroup]
			if uint(idx.Index) >= uint(base) {
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
	v.ensureTypeIndex()
	return v.flatSubTypes
}

func (v *moduleValidator) ensureTypeIndex() {
	if v.typeIndexReady {
		return
	}
	v.typeIndexReady = true
	// Tiny modules use bounded direct lookups outside validation. Keep their
	// validator index local instead of allocating an unused shared directory.
	if len(v.m.Types) <= 8 {
		total := 0
		for _, group := range v.m.Types {
			total += len(group.SubTypes)
		}
		if total <= 8 {
			v.typeGroupBases = make([]int, len(v.m.Types)+1)
			v.flatSubTypes = make([]moduleSubTypeRef, 0, total)
			for group := range v.m.Types {
				v.typeGroupBases[group] = len(v.flatSubTypes)
				for member := range v.m.Types[group].SubTypes {
					v.flatSubTypes = append(v.flatSubTypes, moduleSubTypeRef{st: &v.m.Types[group].SubTypes[member], recGroup: group})
				}
			}
			v.typeGroupBases[len(v.m.Types)] = total
			return
		}
	}
	directory := v.m.typeIndex()
	v.typeGroupBases = directory.bases
	v.flatSubTypes = directory.flat
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
	v.ensureTypeIndex()
	return v.typeGroupBases[recGroup] + int(idx.Index), true
}

func (v *moduleValidator) validateSubtypeMetadata() error {
	flat := v.flattenedSubTypeRefs()
	for flatIdx, cur := range flat {
		member := flatIdx - v.typeGroupBases[cur.recGroup]
		// Most modules have no custom descriptors. Keep their validation path to
		// two inline bit tests instead of calling the full metadata checker.
		if cur.st.Metadata.Describes.Present() || cur.st.Metadata.Descriptor.Present() {
			if err := v.validateDescriptorMetadata(cur.st, cur.recGroup, member); err != nil {
				return err
			}
		}
		for _, supIdx := range cur.st.Supers {
			supFlat, ok := v.flatTypeIdxInRecGroup(supIdx, cur.recGroup)
			if !ok {
				return v.err(ErrUnknownType, "supertype")
			}
			sup, supGroup, ok := v.subtypeByFlatTypeIdx(supFlat)
			if !ok {
				return v.err(ErrUnknownType, "supertype")
			}
			if sup.Final {
				return v.err(ErrTypeMismatch, "final supertype")
			}
			if cur.st.Comp.Kind != sup.Comp.Kind {
				return v.err(ErrTypeMismatch, "supertype kind")
			}
			if err := v.validateDescriptorSubtypeMetadata(cur.st, cur.recGroup, sup, supGroup); err != nil {
				return err
			}
			if !v.compTypeSubtype(cur.st.Comp, cur.recGroup, sup.Comp, supGroup) {
				return v.err(ErrTypeMismatch, "subtype does not match supertype")
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

func (v *moduleValidator) validateDescriptorMetadata(st *SubType, recGroup, member int) error {
	describes, hasDescribes := st.Metadata.Describes.Get()
	descriptor, hasDescriptor := st.Metadata.Descriptor.Get()
	if !hasDescribes && !hasDescriptor {
		return nil
	}
	if st.Comp.Kind != CompStruct {
		return v.err(ErrTypeMismatch, "descriptor metadata requires struct type")
	}

	// Descriptor edges are reciprocal within one recursion group. Requiring
	// describes to point backward also orients descriptor chains without cycles.
	self := TypeIdx{Index: uint32(member), Rec: true}
	checkLink := func(idx TypeIdx, describesEdge bool) error {
		if !idx.Rec || idx.Index >= uint32(len(v.m.Types[recGroup].SubTypes)) {
			return v.err(ErrTypeMismatch, "descriptor metadata crosses recursion group")
		}
		if describesEdge && idx.Index >= uint32(member) {
			return v.err(ErrTypeMismatch, "describes must name an earlier type")
		}
		other := &v.m.Types[recGroup].SubTypes[idx.Index]
		if other.Comp.Kind != CompStruct {
			return v.err(ErrTypeMismatch, "descriptor type mismatch")
		}
		// Keep both ends equally extensible. If only one were final, the complete-
		// square rule would make the declared-open end impossible to subtype.
		if st.Final != other.Final {
			return v.err(ErrTypeMismatch, "descriptor metadata finality mismatch")
		}
		var reverse OptionalTypeIdx
		if describesEdge {
			reverse = other.Metadata.Descriptor
		} else {
			reverse = other.Metadata.Describes
		}
		back, present := reverse.Get()
		if !present || back != self {
			return v.err(ErrTypeMismatch, "descriptor metadata is not reciprocal")
		}
		return nil
	}
	if hasDescribes {
		if err := checkLink(describes, true); err != nil {
			return err
		}
	}
	if hasDescriptor {
		if err := checkLink(descriptor, false); err != nil {
			return err
		}
	}
	return nil
}

func (v *moduleValidator) validateDescriptorSubtypeMetadata(sub *SubType, subGroup int, sup *SubType, supGroup int) error {
	// Each declared subtype edge must be mirrored by its descriptor and
	// describes edges. This is the custom-descriptor "complete square" rule.
	if err := v.validateDescriptorSubtypeEdge(sub.Metadata.Descriptor, subGroup, sup.Metadata.Descriptor, supGroup); err != nil {
		return err
	}
	return v.validateDescriptorSubtypeEdge(sub.Metadata.Describes, subGroup, sup.Metadata.Describes, supGroup)
}

func (v *moduleValidator) validateDescriptorSubtypeEdge(sub OptionalTypeIdx, subGroup int, sup OptionalTypeIdx, supGroup int) error {
	subIdx, subPresent := sub.Get()
	supIdx, supPresent := sup.Get()
	if subPresent != supPresent {
		return v.err(ErrTypeMismatch, "descriptor metadata differs from supertype")
	}
	if !subPresent {
		return nil
	}
	subFlat, subOK := v.flatTypeIdxInRecGroup(subIdx, subGroup)
	supFlat, supOK := v.flatTypeIdxInRecGroup(supIdx, supGroup)
	if !subOK || !supOK {
		return v.err(ErrUnknownType, "descriptor subtype")
	}
	descriptor, descriptorGroup, ok := v.subtypeByFlatTypeIdx(subFlat)
	if !ok || len(descriptor.Supers) != 1 {
		return v.err(ErrTypeMismatch, "incomplete descriptor subtype square")
	}
	descriptorSuper, ok := v.flatTypeIdxInRecGroup(descriptor.Supers[0], descriptorGroup)
	if !ok {
		return v.err(ErrUnknownType, "descriptor supertype")
	}
	if descriptorSuper != supFlat {
		return v.err(ErrTypeMismatch, "incomplete descriptor subtype square")
	}
	return nil
}

func (v *moduleValidator) compTypeSubtype(sub CompType, subGroup int, sup CompType, supGroup int) bool {
	if sub.Kind != sup.Kind {
		return false
	}
	subVal := func(a ValType, aGroup int, b ValType, bGroup int) bool {
		a = v.resolveValTypeRecIndexes(a, aGroup)
		b = v.resolveValTypeRecIndexes(b, bGroup)
		if a.Kind() == ValBot {
			return true
		}
		if a.Kind() != b.Kind() || a.Num() != b.Num() {
			return false
		}
		return a.Kind() != ValRef || v.refSubtype(a.Ref(), b.Ref())
	}
	storageSubtype := func(a StorageType, aGroup int, b StorageType, bGroup int) bool {
		if a.Packed() || b.Packed() {
			return a.Packed() == b.Packed() && a.Pack() == b.Pack()
		}
		return subVal(a.Val(), aGroup, b.Val(), bGroup)
	}
	fieldSubtype := func(a FieldType, aGroup int, b FieldType, bGroup int) bool {
		if a.Mut() != b.Mut() {
			return false
		}
		if !storageSubtype(a.Storage(), aGroup, b.Storage(), bGroup) {
			return false
		}
		return a.Mut() == Const || storageSubtype(b.Storage(), bGroup, a.Storage(), aGroup)
	}
	switch sub.Kind {
	case CompFunc:
		if len(sub.Params) != len(sup.Params) || len(sub.Results) != len(sup.Results) {
			return false
		}
		for i := range sub.Params {
			if !subVal(sup.Params[i], supGroup, sub.Params[i], subGroup) {
				return false
			}
		}
		for i := range sub.Results {
			if !subVal(sub.Results[i], subGroup, sup.Results[i], supGroup) {
				return false
			}
		}
		return true
	case CompStruct:
		if len(sub.Fields) < len(sup.Fields) {
			return false
		}
		for i := range sup.Fields {
			if !fieldSubtype(sub.Fields[i], subGroup, sup.Fields[i], supGroup) {
				return false
			}
		}
		return true
	case CompArray:
		return fieldSubtype(sub.Array, subGroup, sup.Array, supGroup)
	default:
		return false
	}
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

func (v *moduleValidator) structFields(idx TypeIdx) ([]FieldType, *SubType, int, bool) {
	st, recGroup, ok := v.subtypeByTypeIdxWithRecGroup(idx)
	if !ok || st.Comp.Kind != CompStruct {
		return nil, nil, 0, false
	}
	fields := make([]FieldType, len(st.Comp.Fields))
	for i, f := range st.Comp.Fields {
		fields[i] = v.resolveFieldTypeRecIndexes(f, recGroup)
	}
	return fields, st, recGroup, true
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
	storage := ft.Storage()
	if storage.Packed() {
		return ft
	}
	return NewFieldType(StorageVal(v.resolveValTypeRecIndexes(storage.Val(), recGroup)), ft.Mut())
}

func (v *moduleValidator) resolveValTypeRecIndexes(t ValType, recGroup int) ValType {
	if t.Kind() != ValRef {
		return t
	}
	return RefVal(v.resolveRefTypeRecIndexes(t.Ref(), recGroup))
}

func (v *moduleValidator) resolveRefTypeRecIndexes(rt RefType, recGroup int) RefType {
	heap := rt.Heap()
	if heap.Kind() != HeapTypeIndex {
		return rt
	}
	return rt.WithHeap(IndexedHeap(v.resolveTypeIdxRecIndex(heap.Type(), recGroup)))
}

func (v *moduleValidator) resolveTypeIdxRecIndex(idx TypeIdx, recGroup int) TypeIdx {
	flat, ok := v.flatTypeIdxInRecGroup(idx, recGroup)
	if !ok {
		return idx
	}
	return TypeIdx{Index: uint32(flat)}
}

func storageValType(st StorageType, signedGet bool) ValType {
	if !st.Packed() {
		return st.Val()
	}
	_ = signedGet
	return I32
}

func packedFieldGet(kind InstrKind) bool {
	switch kind {
	case InstrStructGetS, InstrStructGetU, InstrStructAtomicGetS, InstrStructAtomicGetU,
		InstrArrayGetS, InstrArrayGetU:
		return true
	default:
		return false
	}
}

func valTypeDefaultable(t ValType) bool {
	return t.Kind() != ValRef || t.Ref().Nullable()
}

func (v *moduleValidator) descriptorTargetRefType(nullable bool, ht HeapType, exact bool) (ValType, bool) {
	if ht.Kind() == HeapTypeIndex {
		if _, ok := v.subtypeByTypeIdx(ht.Type()); !ok {
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
		if cur == bFlat || v.typeIdxEquivalent(TypeIdx{Index: uint32(cur)}, TypeIdx{Index: uint32(bFlat)}) {
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
	if a.Heap().Kind() == heapBottom || b.Heap().Kind() == heapBottom {
		return true
	}
	af, aok := v.refTestHeapFamily(a.Heap())
	bf, bok := v.refTestHeapFamily(b.Heap())
	return aok && bok && af == bf
}

func (v *moduleValidator) refTestHeapFamily(h HeapType) (refTestFamily, bool) {
	if h.Kind() == HeapAbs {
		switch h.Abs() {
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
	switch h.Kind() {
	case HeapTypeIndex:
		ct, ok := v.compTypeFromTypeIdx(h.Type())
		if !ok {
			return 0, false
		}
		kind = ct.Kind
	case HeapDefType:
		defKind, valid := h.DefCompKind()
		if !valid {
			return 0, false
		}
		kind = defKind
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
	if a.Heap().Kind() == HeapAbs && b.Heap().Kind() == HeapAbs {
		return absHeapSubtype(a.Heap().Abs(), b.Heap().Abs()) || absHeapSubtype(b.Heap().Abs(), a.Heap().Abs())
	}
	if a.Exact() && b.Exact() && a.Heap().Kind() == HeapTypeIndex && b.Heap().Kind() == HeapTypeIndex {
		return v.typeIdxEquivalent(a.Heap().Type(), b.Heap().Type())
	}
	return v.refSubtype(a, b) || v.refSubtype(b, a)
}

func (v *moduleValidator) refSubtype(a, b RefType) bool {
	if !b.Nullable() && a.Nullable() {
		return false
	}
	if a.Heap().Kind() == heapBottom {
		return true
	}
	if v.heapTypeEquivalent(a.Heap(), b.Heap()) {
		if b.Exact() && !a.Exact() {
			return false
		}
		return true
	}
	if a.Heap().Kind() == HeapTypeIndex && b.Heap().Kind() == HeapTypeIndex {
		return !b.Exact() && v.typeIdxSuperSubtype(a.Heap().Type(), b.Heap().Type())
	}
	if a.Heap().Kind() == HeapAbs && b.Heap().Kind() == HeapTypeIndex {
		ct, ok := v.compTypeFromTypeIdx(b.Heap().Type())
		if !ok {
			return false
		}
		switch ct.Kind {
		case CompFunc:
			return a.Heap().Abs() == HeapNoFunc
		case CompStruct, CompArray:
			return a.Heap().Abs() == HeapNone
		}
	}
	if a.Heap().Kind() == HeapTypeIndex && b.Heap().Kind() == HeapAbs {
		ct, ok := v.compTypeFromTypeIdx(a.Heap().Type())
		if !ok {
			return false
		}
		switch ct.Kind {
		case CompStruct:
			return absHeapSubtype(HeapStruct, b.Heap().Abs())
		case CompArray:
			return absHeapSubtype(HeapArray, b.Heap().Abs())
		case CompFunc:
			return absHeapSubtype(HeapFunc, b.Heap().Abs())
		}
	}
	if a.Heap().Kind() == HeapAbs && b.Heap().Kind() == HeapAbs {
		return absHeapSubtype(a.Heap().Abs(), b.Heap().Abs())
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
type moduleTypePair struct{ a, b int }

func (v *moduleValidator) typeIdxEquivalent(a, b TypeIdx) bool {
	return v.typeIdxEquivalentWithState(a, b, make(map[moduleTypePair]uint8))
}

// typeIdxEquivalentWithState permits a batch of comparisons to share the
// proven results for recursive-group member pairs. Each top-level comparison
// is a transaction: nested successes may depend on active recursive assumptions,
// so a failed root discards every pair introduced by that transaction. Only a
// successful root proves all its reachable pairs and commits them for reuse.
func (v *moduleValidator) typeIdxEquivalentWithState(a, b TypeIdx, state map[moduleTypePair]uint8) bool {
	aFlat, aok := v.flatTypeIdxInRecGroup(a, -1)
	bFlat, bok := v.flatTypeIdxInRecGroup(b, -1)
	if !aok || !bok {
		return false
	}
	var firstPair [1]moduleTypePair
	introduced := firstPair[:0]
	var eqType func(int, int) bool
	var eqVal func(ValType, ValType, int, int) bool
	eqHeap := func(x, y HeapType, xGroup, yGroup int) bool {
		if x.Kind() != y.Kind() {
			return false
		}
		switch x.Kind() {
		case HeapAbs:
			return x.Abs() == y.Abs()
		case HeapTypeIndex:
			if x.Type().Rec != y.Type().Rec {
				return false
			}
			xi, xok := v.flatTypeIdxInRecGroup(x.Type(), xGroup)
			yi, yok := v.flatTypeIdxInRecGroup(y.Type(), yGroup)
			return xok && yok && eqType(xi, yi)
		case HeapDefType:
			xg, xi, _, xv := x.Def()
			yg, yi, _, yv := y.Def()
			return xv == yv && (!xv || xg == yg && xi == yi)
		default:
			return false
		}
	}
	eqVal = func(x, y ValType, xGroup, yGroup int) bool {
		if x.Kind() != y.Kind() || x.Num() != y.Num() {
			return false
		}
		if x.Kind() != ValRef {
			return true
		}
		return x.Ref().Nullable() == y.Ref().Nullable() && x.Ref().Exact() == y.Ref().Exact() && eqHeap(x.Ref().Heap(), y.Ref().Heap(), xGroup, yGroup)
	}
	eqStorage := func(x, y StorageType, xGroup, yGroup int) bool {
		if x.Packed() != y.Packed() || x.Pack() != y.Pack() {
			return false
		}
		return x.Packed() || eqVal(x.Val(), y.Val(), xGroup, yGroup)
	}
	eqField := func(x, y FieldType, xGroup, yGroup int) bool {
		return x.Mut() == y.Mut() && eqStorage(x.Storage(), y.Storage(), xGroup, yGroup)
	}
	groupLocation := func(flat int) (group, local, base int, ok bool) {
		v.ensureTypeIndex()
		if flat < 0 || flat >= len(v.flatSubTypes) {
			return 0, 0, 0, false
		}
		group = v.flatSubTypes[flat].recGroup
		if group < 0 || group+1 >= len(v.typeGroupBases) {
			return 0, 0, 0, false
		}
		base = v.typeGroupBases[group]
		return group, flat - base, base, true
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
		p := moduleTypePair{x, y}
		switch state[p] {
		case 1, 2:
			return true
		case 3:
			return false
		}
		xs, xGroup, xok := v.subtypeByFlatTypeIdx(x)
		ys, yGroup, yok := v.subtypeByFlatTypeIdx(y)
		// Reject known mismatches before opening a recursive assumption. These
		// checks cannot depend on another pair and need no rollback bookkeeping.
		if !xok || !yok || xs.Final != ys.Final || len(xs.Supers) != len(ys.Supers) || xs.Comp.Kind != ys.Comp.Kind {
			return false
		}
		// The first member pair is a marker for a scan of this whole group pair.
		// Read it before recording p, which may itself be that first member.
		// A non-first entry can start one redundant scan through the first
		// member; all later members then reuse the first member's active state.
		scanGroup := len(v.m.Types[xGroupLoc].SubTypes) > 1 && state[moduleTypePair{xBase, yBase}] == 0
		introduced = append(introduced, p)
		state[p] = 1
		ok := true
		// Recursive type equivalence is defined over whole groups, not only
		// the graph reachable from one projection. A projected type from a
		// two-member group is therefore not equivalent to an identical
		// singleton implicit function type.
		if scanGroup {
			for i := range v.m.Types[xGroupLoc].SubTypes {
				if !eqType(xBase+i, yBase+i) {
					ok = false
					break
				}
			}
		}
		eqOptionalType := func(x, y OptionalTypeIdx) bool {
			xv, xpresent := x.Get()
			yv, ypresent := y.Get()
			if !xpresent || !ypresent {
				return xpresent == ypresent
			}
			if xv.Rec != yv.Rec {
				return false
			}
			xi, xok := v.flatTypeIdxInRecGroup(xv, xGroup)
			yi, yok := v.flatTypeIdxInRecGroup(yv, yGroup)
			return xok && yok && eqType(xi, yi)
		}
		if ok {
			ok = eqOptionalType(xs.Metadata.Describes, ys.Metadata.Describes) && eqOptionalType(xs.Metadata.Descriptor, ys.Metadata.Descriptor)
		}
		if ok {
			for i := range xs.Supers {
				if xs.Supers[i].Rec != ys.Supers[i].Rec {
					ok = false
					break
				}
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
	ok := eqType(aFlat, bFlat)
	if !ok {
		for _, pair := range introduced {
			delete(state, pair)
		}
	}
	return ok
}

func (v *moduleValidator) heapTypeEquivalent(a, b HeapType) bool {
	if equalHeapType(a, b) {
		return true
	}
	return a.Kind() == HeapTypeIndex && b.Kind() == HeapTypeIndex && v.typeIdxEquivalent(a.Type(), b.Type())
}
