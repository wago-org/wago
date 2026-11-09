package wago

import "sync/atomic"

// definedTypesClosed reports whether every descriptor has a composite kind and
// every type index it names is in range. On such a graph definedTypeEquivalent
// of a type with itself always succeeds: each of its comparisons sees identical
// inputs, and its only other failures are an out-of-range index or an unknown
// kind. The check is stricter than needed; it never admits more.
func definedTypesClosed(types []DefinedTypeDescriptor) bool {
	n := uint64(len(types))
	value := func(v ValueTypeDescriptor) bool {
		return v.Kind != ValueTypeReference || !v.Ref.Heap.Defined || uint64(v.Ref.Heap.TypeIndex) < n
	}
	field := func(f FieldTypeDescriptor) bool { return f.Storage.Packed || value(f.Storage.Value) }
	for i := range types {
		d := &types[i]
		for _, super := range d.Supers {
			if uint64(super) >= n {
				return false
			}
		}
		if d.HasDescribes && uint64(d.Describes) >= n || d.HasDescriptor && uint64(d.Descriptor) >= n {
			return false
		}
		switch d.Kind {
		case CompositeTypeFunction:
			for _, v := range d.Params {
				if !value(v) {
					return false
				}
			}
			for _, v := range d.Results {
				if !value(v) {
					return false
				}
			}
		case CompositeTypeStruct:
			for _, f := range d.Fields {
				if !field(f) {
					return false
				}
			}
		case CompositeTypeArray:
			if !field(d.Array) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// executionTypesClosed caches definedTypesClosed for the execution snapshot,
// whose Types are a private copy fixed when it is frozen. Other views,
// including copies of the snapshot, report false and keep the full check.
func (c *Compiled) executionTypesClosed() bool {
	memo := c.loadValidateMemo()
	if memo == nil || memo.executionView() != c {
		return false
	}
	switch atomic.LoadUint32(&memo.executionTypesClosed) {
	case 1:
		return false
	case 2:
		return true
	}
	closed := definedTypesClosed(c.Types)
	state := uint32(1)
	if closed {
		state = 2
	}
	atomic.StoreUint32(&memo.executionTypesClosed, state)
	return closed
}

// executionValueTypeSubtype is valueTypeSubtype(actual, actualTypes, required,
// c.Types). When both references name the same defined type of this snapshot's
// own closed graph their heaps are equivalent, so only referenceTypeSubtype's
// nullability and exactness rules remain to decide.
func (c *Compiled) executionValueTypeSubtype(actual ValueTypeDescriptor, actualTypes []DefinedTypeDescriptor, required ValueTypeDescriptor) bool {
	if actual.Kind == ValueTypeReference && required.Kind == ValueTypeReference &&
		actual.Ref.Heap.Defined && required.Ref.Heap.Defined && actual.Ref.Heap.TypeIndex == required.Ref.Heap.TypeIndex &&
		uint(actual.Ref.Heap.TypeIndex) < uint(len(c.Types)) && len(actualTypes) == len(c.Types) && &actualTypes[0] == &c.Types[0] &&
		c.executionTypesClosed() {
		return (!actual.Ref.Nullable || required.Ref.Nullable) && (!required.Ref.Exact || actual.Ref.Exact)
	}
	return valueTypeSubtype(actual, actualTypes, required, c.Types)
}
