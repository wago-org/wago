package gc

import (
	"errors"
	"fmt"
)

// Keep temporary index memory at most 32 KiB for untrusted field order.
const maxUnorderedStructFields = 1 << 14

// HasHeapObjectTypes reports whether the descriptor table contains any GC heap
// object layouts. Function sentinels preserve TypeIdx indexes but do not need an
// instance collector by themselves.
func HasHeapObjectTypes(descs []TypeDesc) bool {
	for _, d := range descs {
		if d.Kind == KindStruct || d.Kind == KindArray {
			return true
		}
	}
	return false
}

// ValidateTypeDescs checks the compact descriptor table before it is stored in
// compiled metadata or used to create a Collector. The descriptor slice is
// indexed by TypeID; function sentinels preserve Wasm TypeIdx order but are not
// heap-object layouts. Supertype metadata must be same-kind, non-final, and
// acyclic so serialized .wago blobs cannot inject malformed subtype chains.
func ValidateTypeDescs(descs []TypeDesc) error {
	for i, d := range descs {
		if d.ID != TypeID(i) {
			return fmt.Errorf("gc: descriptor %d has id %d", i, d.ID)
		}
		if d.HasSuper {
			if int(d.Super) >= len(descs) {
				return fmt.Errorf("gc: descriptor %d has invalid super %d", i, d.Super)
			}
			if d.Super == d.ID {
				return fmt.Errorf("gc: descriptor %d cannot be its own super", i)
			}
		}
		switch d.Kind {
		case KindFunc:
			if len(d.Fields) != 0 || d.Elem != 0 || d.Size != 0 || d.ElemSize != 0 || d.Align != 0 || d.HasRefs {
				return fmt.Errorf("gc: function descriptor %d has heap layout metadata", i)
			}
		case KindStruct:
			if err := validateStructDesc(d); err != nil {
				return fmt.Errorf("gc: struct %d: %w", i, err)
			}
		case KindArray:
			if len(d.Fields) != 0 || d.Size != 0 {
				return fmt.Errorf("gc: array descriptor %d has struct metadata", i)
			}
			a, sz, err := storageLayout(d.Elem)
			if err != nil {
				return fmt.Errorf("gc: array descriptor %d: %w", i, err)
			}
			if d.Align != a || d.ElemSize != sz {
				return fmt.Errorf("gc: array descriptor %d elem layout mismatch", i)
			}
			if d.HasRefs != isCollectorRefKind(d.Elem) {
				return fmt.Errorf("gc: array descriptor %d HasRefs mismatch", i)
			}
		default:
			return fmt.Errorf("gc: descriptor %d has unknown kind %d", i, d.Kind)
		}
	}
	if err := validateSuperRelations(descs); err != nil {
		return err
	}
	return nil
}

// Keep struct-layout diagnostics behind one descriptor-context wrapper. Static
// reasons avoid repeated formatting code in size-constrained TinyGo builds.
func validateStructDesc(d TypeDesc) error {
	if d.Elem != 0 || d.ElemSize != 0 {
		return errors.New("array metadata")
	}
	if d.Align == 0 || d.Align > 16 || d.Align&(d.Align-1) != 0 {
		return errors.New("invalid alignment")
	}
	if _, err := StructSize(d); err != nil {
		return err
	}
	var maxEnd uint32
	ordered := true
	seenRefs := false
	for _, f := range d.Fields {
		a, sz, err := storageLayout(f.Kind)
		if err != nil {
			return err
		}
		if d.Align < a || f.Offset%a != 0 || uint64(f.Offset)+uint64(sz) > uint64(d.Size) {
			return errors.New("invalid field layout")
		}
		if f.Offset < maxEnd {
			ordered = false
		}
		if f.Offset+sz > maxEnd {
			maxEnd = f.Offset + sz
		}
		if isCollectorRefKind(f.Kind) {
			seenRefs = true
		}
	}
	if !ordered {
		if err := validateUnorderedStructFields(d.Fields); err != nil {
			return err
		}
	}
	if d.Size != align(maxEnd, d.Align) {
		return errors.New("size mismatch")
	}
	if d.HasRefs != seenRefs {
		return errors.New("HasRefs mismatch")
	}
	return nil
}

func validateUnorderedStructFields(fields []FieldDesc) error {
	if len(fields) > maxUnorderedStructFields {
		return errors.New("unordered field limit")
	}
	order := make([]uint16, len(fields))
	for i := range order {
		order[i] = uint16(i)
	}
	// A min-heap visits fields in offset order without changing field indexes,
	// which are part of the access ABI. Work is bounded by O(n log n).
	for root := len(order)/2 - 1; root >= 0; root-- {
		siftFieldOrder(order, fields, root)
	}
	var end uint32
	for len(order) > 0 {
		f := fields[order[0]]
		_, sz, _ := storageLayout(f.Kind) // already checked by the caller
		if f.Offset < end {
			return errors.New("overlapping fields")
		}
		end = f.Offset + sz
		order[0] = order[len(order)-1]
		order = order[:len(order)-1]
		if len(order) > 0 {
			siftFieldOrder(order, fields, 0)
		}
	}
	return nil
}

func siftFieldOrder(order []uint16, fields []FieldDesc, root int) {
	value := order[root]
	offset := fields[value].Offset
	for child := root*2 + 1; child < len(order); child = root*2 + 1 {
		if child+1 < len(order) && fields[order[child]].Offset > fields[order[child+1]].Offset {
			child++
		}
		if offset <= fields[order[child]].Offset {
			break
		}
		order[root] = order[child]
		root = child
	}
	order[root] = value
}

func validateSuperRelations(descs []TypeDesc) error {
	for _, d := range descs {
		if !d.HasSuper {
			continue
		}
		s := descs[d.Super]
		if d.Kind != s.Kind {
			return errors.New("gc: super kind mismatch")
		}
		if s.Final {
			return errors.New("gc: cannot extend final super")
		}
		badLayout := false
		if d.Kind == KindStruct {
			badLayout = len(d.Fields) < len(s.Fields)
			if !badLayout {
				for field, inherited := range s.Fields {
					actual := d.Fields[field]
					if actual.Offset != inherited.Offset || !inheritedStorageCompatible(actual.Kind, inherited.Kind) {
						badLayout = true
						break
					}
				}
			}
		} else if d.Kind == KindArray {
			badLayout = !inheritedStorageCompatible(d.Elem, s.Elem)
		}
		if badLayout {
			return errors.New("gc: incompatible subtype layout")
		}
	}
	return validateSuperAcyclic(descs)
}

func inheritedStorageCompatible(actual, inherited StorageKind) bool {
	return actual == inherited || referenceStorageCompatible(inherited, actual)
}

func validateSuperAcyclic(descs []TypeDesc) error {
	const (
		white uint8 = iota
		gray
		black
	)
	state := make([]uint8, len(descs))
	path := make([]int, 0, len(descs))
	for i := range descs {
		if state[i] != white {
			continue
		}
		path = path[:0]
		for cur := i; ; cur = int(descs[cur].Super) {
			switch state[cur] {
			case black:
				for _, p := range path {
					state[p] = black
				}
				goto next
			case gray:
				return errors.New("gc: cyclic super chain")
			}
			state[cur] = gray
			path = append(path, cur)
			if !descs[cur].HasSuper {
				for _, p := range path {
					state[p] = black
				}
				goto next
			}
		}
	next:
	}
	return nil
}
