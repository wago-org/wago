package gc

import "fmt"

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
			if d.Elem != 0 || d.ElemSize != 0 {
				return fmt.Errorf("gc: struct descriptor %d has array metadata", i)
			}
			if d.Align == 0 || d.Align > 8 || d.Align&(d.Align-1) != 0 {
				return fmt.Errorf("gc: struct descriptor %d has invalid align %d", i, d.Align)
			}
			if _, err := StructSize(d); err != nil {
				return fmt.Errorf("gc: struct descriptor %d: %w", i, err)
			}
			var maxEnd uint32
			seenRefs := false
			for j, f := range d.Fields {
				a, sz, err := storageLayout(f.Kind)
				if err != nil {
					return fmt.Errorf("gc: struct descriptor %d field %d: %w", i, j, err)
				}
				if f.Offset%a != 0 {
					return fmt.Errorf("gc: struct descriptor %d field %d offset %d is not aligned to %d", i, j, f.Offset, a)
				}
				if f.Offset > ^uint32(0)-sz || f.Offset+sz > d.Size {
					return fmt.Errorf("gc: struct descriptor %d field %d out of bounds", i, j)
				}
				if f.Offset+sz > maxEnd {
					maxEnd = f.Offset + sz
				}
				if isRefKind(f.Kind) {
					seenRefs = true
				}
			}
			if d.Size != align(maxEnd, d.Align) {
				return fmt.Errorf("gc: struct descriptor %d size %d does not match fields", i, d.Size)
			}
			if d.HasRefs != seenRefs {
				return fmt.Errorf("gc: struct descriptor %d HasRefs mismatch", i)
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
			if d.HasRefs != isRefKind(d.Elem) {
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

func validateSuperRelations(descs []TypeDesc) error {
	for i, d := range descs {
		if !d.HasSuper {
			continue
		}
		s := descs[d.Super]
		if d.Kind != s.Kind {
			return fmt.Errorf("gc: descriptor %d kind %d cannot extend super %d kind %d", i, d.Kind, d.Super, s.Kind)
		}
		if s.Final {
			return fmt.Errorf("gc: descriptor %d cannot extend final super %d", i, d.Super)
		}
	}
	return validateSuperAcyclic(descs)
}

func validateSuperAcyclic(descs []TypeDesc) error {
	for i := range descs {
		seen := make([]bool, len(descs))
		for d := descs[i]; d.HasSuper; d = descs[d.Super] {
			if seen[d.Super] {
				return fmt.Errorf("gc: descriptor %d has cyclic super chain through %d", i, d.Super)
			}
			seen[d.Super] = true
		}
	}
	return nil
}
