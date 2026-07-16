package gc

import "fmt"

type TypeID uint32

type TypeKind uint8

const (
	// KindFunc preserves flattened Wasm type indexes for function types. It is
	// not a heap-object layout and must not be allocated as a GC object.
	KindFunc TypeKind = iota + 1
	KindStruct
	KindArray
)

type StorageKind uint8

const (
	StorageI8 StorageKind = iota + 1
	StorageI16
	StorageI32
	StorageI64
	StorageF32
	StorageF64
	StorageRef
	StorageRefNull
	// Function and extern references use stable 64-bit runtime tokens and are
	// deliberately not traced as compact collector object handles.
	StorageFuncRef
	StorageFuncRefNull
	StorageExternRef
	StorageExternRefNull
)

type FieldDesc struct {
	Kind   StorageKind
	Offset uint32
}

type TypeDesc struct {
	ID       TypeID
	Kind     TypeKind
	Fields   []FieldDesc
	Elem     StorageKind
	Size     uint32
	ElemSize uint32
	Align    uint32
	HasRefs  bool
	Final    bool
	Super    TypeID
	HasSuper bool
}

func NewStructDesc(id TypeID, fields []StorageKind) (TypeDesc, error) {
	return newStructDescLayout(id, fields, 0)
}

func newStructDescLayout(id TypeID, fields []StorageKind, initialOffset uint32) (TypeDesc, error) {
	d := TypeDesc{ID: id, Kind: KindStruct, Align: 1, Final: true}
	d.Fields = make([]FieldDesc, len(fields))
	off := initialOffset
	for i, k := range fields {
		a, sz, err := storageLayout(k)
		if err != nil {
			return TypeDesc{}, err
		}
		off, err = alignChecked(off, a)
		if err != nil {
			return TypeDesc{}, err
		}
		d.Fields[i] = FieldDesc{Kind: k, Offset: off}
		off, err = addChecked(off, sz)
		if err != nil {
			return TypeDesc{}, err
		}
		if a > d.Align {
			d.Align = a
		}
		if isRefKind(k) {
			d.HasRefs = true
		}
	}
	var err error
	d.Size, err = alignChecked(off, d.Align)
	if err != nil {
		return TypeDesc{}, err
	}
	return d, nil
}

func NewArrayDesc(id TypeID, elem StorageKind) (TypeDesc, error) {
	a, sz, err := storageLayout(elem)
	if err != nil {
		return TypeDesc{}, err
	}
	return TypeDesc{ID: id, Kind: KindArray, Elem: elem, ElemSize: sz, Align: a, HasRefs: isRefKind(elem), Final: true}, nil
}

func (d TypeDesc) PointerFree() bool          { return !d.HasRefs }
func (d TypeDesc) ArrayElementsAreRefs() bool { return d.Kind == KindArray && isRefKind(d.Elem) }
func (d TypeDesc) StructRefOffsets() []uint32 {
	var out []uint32
	for _, f := range d.Fields {
		if isRefKind(f.Kind) {
			out = append(out, f.Offset)
		}
	}
	return out
}

func storageLayout(k StorageKind) (alignBytes, size uint32, err error) {
	switch k {
	case StorageI8:
		return 1, 1, nil
	case StorageI16:
		return 2, 2, nil
	case StorageI32, StorageF32, StorageRef, StorageRefNull:
		return 4, 4, nil
	case StorageI64, StorageF64, StorageFuncRef, StorageFuncRefNull, StorageExternRef, StorageExternRefNull:
		return 8, 8, nil
	default:
		return 0, 0, fmt.Errorf("gc: unknown storage kind %d", k)
	}
}

func isRefKind(k StorageKind) bool { return k == StorageRef || k == StorageRefNull }
func align(v, a uint32) uint32 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) &^ (a - 1)
}

func alignChecked(v, a uint32) (uint32, error) {
	if a <= 1 {
		return v, nil
	}
	if v > ^uint32(0)-(a-1) {
		return 0, fmt.Errorf("gc: struct layout overflow")
	}
	aligned := align(v, a)
	if aligned < v {
		return 0, fmt.Errorf("gc: struct layout overflow")
	}
	return aligned, nil
}

func addChecked(v, n uint32) (uint32, error) {
	if v > ^uint32(0)-n {
		return 0, fmt.Errorf("gc: struct layout overflow")
	}
	return v + n, nil
}
