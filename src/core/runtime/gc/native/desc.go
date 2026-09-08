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
	// StorageV128 is a pointer-free 16-byte SIMD value. Keep it appended so
	// existing serialized storage-kind values retain their numeric identity.
	StorageV128
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
	b := newStructDescBuilder(id, len(fields), initialOffset)
	for _, k := range fields {
		if err := b.Add(k); err != nil {
			return TypeDesc{}, err
		}
	}
	return b.Finish()
}

// StructDescBuilder lays out a known number of fields directly into a runtime
// descriptor. It lets compiler lowering avoid an intermediate []StorageKind.
type StructDescBuilder struct {
	desc  TypeDesc
	off   uint32
	index int
}

func NewStructDescBuilder(id TypeID, fieldCount int) StructDescBuilder {
	return newStructDescBuilder(id, fieldCount, 0)
}

func newStructDescBuilder(id TypeID, fieldCount int, initialOffset uint32) StructDescBuilder {
	return StructDescBuilder{
		desc: TypeDesc{ID: id, Kind: KindStruct, Align: 1, Final: true, Fields: make([]FieldDesc, fieldCount)},
		off:  initialOffset,
	}
}

func (b *StructDescBuilder) Add(k StorageKind) error {
	if b.index >= len(b.desc.Fields) {
		return fmt.Errorf("gc: too many struct fields")
	}
	a, sz, err := storageLayout(k)
	if err != nil {
		return err
	}
	off, err := alignChecked(b.off, a)
	if err != nil {
		return err
	}
	b.desc.Fields[b.index] = FieldDesc{Kind: k, Offset: off}
	off, err = addChecked(off, sz)
	if err != nil {
		return err
	}
	if a > b.desc.Align {
		b.desc.Align = a
	}
	if isCollectorRefKind(k) {
		b.desc.HasRefs = true
	}
	b.off = off
	b.index++
	return nil
}

func (b *StructDescBuilder) Finish() (TypeDesc, error) {
	if b.index != len(b.desc.Fields) {
		return TypeDesc{}, fmt.Errorf("gc: got %d struct fields, want %d", b.index, len(b.desc.Fields))
	}
	var err error
	b.desc.Size, err = alignChecked(b.off, b.desc.Align)
	if err != nil {
		return TypeDesc{}, err
	}
	return b.desc, nil
}

func NewArrayDesc(id TypeID, elem StorageKind) (TypeDesc, error) {
	a, sz, err := storageLayout(elem)
	if err != nil {
		return TypeDesc{}, err
	}
	return TypeDesc{ID: id, Kind: KindArray, Elem: elem, ElemSize: sz, Align: a, HasRefs: isCollectorRefKind(elem), Final: true}, nil
}

func (d TypeDesc) PointerFree() bool { return !d.HasRefs }
func (d TypeDesc) ArrayElementsAreRefs() bool {
	return d.Kind == KindArray && isCollectorRefKind(d.Elem)
}
func (d TypeDesc) StructRefOffsets() []uint32 {
	var out []uint32
	for _, f := range d.Fields {
		if isCollectorRefKind(f.Kind) {
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
	case StorageV128:
		return 16, 16, nil
	default:
		return 0, 0, fmt.Errorf("gc: unknown storage kind %d", k)
	}
}

func isCollectorRefKind(k StorageKind) bool { return k == StorageRef || k == StorageRefNull }

func isOpaqueRefKind(k StorageKind) bool {
	return k == StorageFuncRef || k == StorageFuncRefNull || k == StorageExternRef || k == StorageExternRefNull
}

func isAnyReferenceStorage(k StorageKind) bool { return isCollectorRefKind(k) || isOpaqueRefKind(k) }

func isNumericStorage(k StorageKind) bool {
	switch k {
	case StorageI8, StorageI16, StorageI32, StorageI64, StorageF32, StorageF64, StorageV128:
		return true
	default:
		return false
	}
}

func isNullableReferenceStorage(k StorageKind) bool {
	return k == StorageRefNull || k == StorageFuncRefNull || k == StorageExternRefNull
}

func referenceStorageCompatible(dst, src StorageKind) bool {
	if dst == src {
		return isAnyReferenceStorage(dst)
	}
	switch dst {
	case StorageRefNull:
		return src == StorageRef
	case StorageFuncRefNull:
		return src == StorageFuncRef
	case StorageExternRefNull:
		return src == StorageExternRef
	default:
		return false
	}
}
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
