package gc

import (
	"cmp"
	"fmt"
	"slices"
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
			if d.Elem != 0 || d.ElemSize != 0 {
				return fmt.Errorf("gc: struct descriptor %d has array metadata", i)
			}
			if d.Align == 0 || d.Align > 16 || d.Align&(d.Align-1) != 0 {
				return fmt.Errorf("gc: struct descriptor %d has invalid align %d", i, d.Align)
			}
			if _, err := StructSize(d); err != nil {
				return fmt.Errorf("gc: struct descriptor %d: %w", i, err)
			}
			var lastEnd uint32
			ordered := true
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
				if f.Offset < lastEnd {
					ordered = false
				}
				lastEnd = f.Offset + sz
				if isCollectorRefKind(f.Kind) {
					seenRefs = true
				}
			}
			maxEnd := lastEnd
			if !ordered {
				var err error
				maxEnd, err = validateUnorderedStructFields(i, d.Fields)
				if err != nil {
					return err
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

func validateUnorderedStructFields(desc int, fields []FieldDesc) (uint32, error) {
	if len(fields) > maxUnorderedStructFields {
		return 0, fmt.Errorf("gc: struct descriptor %d has too many unordered fields", desc)
	}
	order := make([]uint16, len(fields))
	for i := range order {
		order[i] = uint16(i)
	}
	slices.SortFunc(order, func(a, b uint16) int {
		return cmp.Compare(fields[a].Offset, fields[b].Offset)
	})
	var end uint32
	for _, i := range order {
		f := fields[i]
		_, sz, _ := storageLayout(f.Kind)
		if f.Offset < end {
			return 0, fmt.Errorf("gc: struct descriptor %d field %d overlaps another field", desc, i)
		}
		end = f.Offset + sz
	}
	return end, nil
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
				return fmt.Errorf("gc: descriptor %d has cyclic super chain through %d", i, cur)
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
