package gc

import (
	"encoding/binary"
	"errors"
)

func (c *Collector) NewStruct(typeID TypeID) (Ref, error) { return c.NewStructDefault(typeID) }
func (c *Collector) NewStructDefault(typeID TypeID) (Ref, error) {
	return c.NewStructDefaultWithRoots(typeID, nil)
}

// NewStructWithRoots allocates and initializes every field atomically from the
// caller's values. Reference operands are published as temporary mutable roots
// across a collection-triggering allocation and reread before object stores.
func (c *Collector) NewStructWithRoots(typeID TypeID, values []Value, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindStruct || len(values) != len(d.Fields) {
		return Null(), errors.New("gc: struct initializer shape mismatch")
	}
	hasRefs := false
	for i, field := range d.Fields {
		if err := checkValueCompatible(field.Kind, values[i]); err != nil {
			return Null(), err
		}
		if isCollectorRefKind(field.Kind) {
			if err := c.validateStoredRef(values[i].Ref, isNullableReferenceStorage(field.Kind)); err != nil {
				return Null(), err
			}
			hasRefs = true
		}
	}
	if hasRefs {
		roots = combineRootSets(roots, valueRootSet{values: values, fields: d.Fields})
	}
	sz, err := StructSize(d)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, 0, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	for i, field := range d.Fields {
		if err := c.storeValue(r, d, uint64(PayloadOffset+field.Offset), field.Kind, values[i]); err != nil {
			return Null(), err
		}
	}
	return r, nil
}

func (c *Collector) NewStructDefaultWithRoots(typeID TypeID, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkDefaultable(d); err != nil {
		return Null(), err
	}
	sz, err := StructSize(d)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, 0, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	return r, nil
}
func (c *Collector) NewArray(typeID TypeID, length uint32, init Value) (Ref, error) {
	return c.NewArrayWithRoots(typeID, length, init, nil)
}

// NewArrayFixedWithRoots allocates an array initialized from one value per
// element. Reference operands are rooted across allocation and reread before
// stores, matching the atomic operand lifetime required by array.new_fixed.
func (c *Collector) NewArrayFixedWithRoots(typeID TypeID, values []Value, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindArray {
		return Null(), errors.New("gc: not array")
	}
	for i := range values {
		if err := c.validateArrayStore(d, values[i]); err != nil {
			return Null(), err
		}
	}
	if isCollectorRefKind(d.Elem) && len(values) != 0 {
		roots = combineRootSets(roots, valueRootSet{values: values, all: true})
	}
	sz, err := ArraySize(d, uint32(len(values)))
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, uint32(len(values)), roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	for i := range values {
		if err := c.storeArrayValue(r, d, uint32(i), values[i]); err != nil {
			return Null(), err
		}
	}
	return r, nil
}
func (c *Collector) NewArrayWithRoots(typeID TypeID, length uint32, init Value, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkValueCompatible(d.Elem, init); err != nil {
		return Null(), err
	}
	if isCollectorRefKind(d.Elem) {
		if err := c.validateStoredRef(init.Ref, isNullableReferenceStorage(d.Elem)); err != nil {
			return Null(), err
		}
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	var initRoot *Root
	if isCollectorRefKind(d.Elem) && init.Ref.IsObj() {
		root := Root(init.Ref)
		initRoot = &root
		roots = withExtraRoot(roots, initRoot)
	}
	r, err := c.alloc(d, sz, length, roots)
	if err != nil {
		return Null(), err
	}
	if initRoot != nil {
		init.Ref = Ref(*initRoot)
	}
	for i := uint32(0); i < length; i++ {
		if err := c.storeValue(r, d, uint64(PayloadOffset)+uint64(i)*uint64(d.ElemSize), d.Elem, init); err != nil {
			return Null(), err
		}
	}
	return r, nil
}

// NewRefArrayWithRoots allocates a reference array initialized from a caller-
// owned mutable root slot. The slot must also be present in roots when allocation
// may collect; it is reread after allocation so moving collectors can rewrite it.
func (c *Collector) NewRefArrayWithRoots(typeID TypeID, length uint32, init RootSlot, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if !isCollectorRefKind(d.Elem) || init == nil {
		return Null(), errors.New("gc: reference array initializer root required")
	}
	value := RefValue(init.GetRef())
	if err := c.validateStoredRef(value.Ref, isNullableReferenceStorage(d.Elem)); err != nil {
		return Null(), err
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, length, roots)
	if err != nil {
		return Null(), err
	}
	value.Ref = init.GetRef()
	for i := uint32(0); i < length; i++ {
		if err := c.storeValue(r, d, uint64(PayloadOffset)+uint64(i)*uint64(d.ElemSize), d.Elem, value); err != nil {
			return Null(), err
		}
	}
	return r, nil
}

func (c *Collector) NewArrayDefault(typeID TypeID, length uint32) (Ref, error) {
	return c.NewArrayDefaultWithRoots(typeID, length, nil)
}
func (c *Collector) NewArrayDefaultWithRoots(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if length != 0 {
		if err := checkDefaultable(d); err != nil {
			return Null(), err
		}
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, sz, length, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	return r, nil
}

// ObjectType returns the exact runtime descriptor id for a live object ref.
// Null, i31, stale, forged, or closed-collector references reject.
func (c *Collector) ObjectType(ref Ref) (TypeID, error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return 0, err
	}
	return d.ID, nil
}

func (c *Collector) ArrayLen(ref Ref) (uint32, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return 0, e
	}
	if d.Kind != KindArray {
		return 0, errors.New("gc: not array")
	}
	return c.header(ref).Aux, nil
}
func (c *Collector) StructGet(ref Ref, field uint32) (Value, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return Value{}, e
	}
	if d.Kind != KindStruct {
		return Value{}, errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return Value{}, errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	return c.loadValue(ref, uint64(PayloadOffset+f.Offset), f.Kind)
}
func (c *Collector) StructSet(ref Ref, field uint32, value Value) error {
	d, e := c.refDesc(ref)
	if e != nil {
		return e
	}
	if d.Kind != KindStruct {
		return errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	if err := checkValueCompatible(f.Kind, value); err != nil {
		return err
	}
	if isCollectorRefKind(f.Kind) {
		if err := c.validateStoredRef(value.Ref, isNullableReferenceStorage(f.Kind)); err != nil {
			return err
		}
		c.WriteBarrierObject(ref, value.Ref)
		return c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value)
	}
	return c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value)
}
func (c *Collector) ArrayGet(ref Ref, index uint32) (Value, error) {
	d, e := c.refDesc(ref)
	if e != nil {
		return Value{}, e
	}
	if d.Kind != KindArray {
		return Value{}, errors.New("gc: not array")
	}
	ln := c.header(ref).Aux
	if index >= ln {
		return Value{}, errors.New("gc: index out of range")
	}
	return c.loadValue(ref, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem)
}
func (c *Collector) ArraySet(ref Ref, index uint32, value Value) error {
	d, e := c.refDesc(ref)
	if e != nil {
		return e
	}
	if d.Kind != KindArray {
		return errors.New("gc: not array")
	}
	ln := c.header(ref).Aux
	if index >= ln {
		return errors.New("gc: index out of range")
	}
	if err := c.validateArrayStore(d, value); err != nil {
		return err
	}
	return c.storeArrayValue(ref, d, index, value)
}

// ArrayFill preflights the complete destination range and value before making
// any write. Throughput collectors mutate the compact payload directly and run
// one post-write range barrier. Tiny retains the scalar barrier while marking or
// sweeping because its incremental tri-color invariant is per published edge.
func (c *Collector) ArrayFill(ref Ref, start uint32, value Value, length uint32) error {
	d, err := c.refDesc(ref)
	if err != nil {
		return err
	}
	if d.Kind != KindArray {
		return errors.New("gc: not array")
	}
	arrayLen := c.header(ref).Aux
	if uint64(start)+uint64(length) > uint64(arrayLen) {
		return errRange
	}
	if err := c.validateArrayStore(d, value); err != nil {
		return err
	}
	if length == 0 {
		return nil
	}
	if isCollectorRefKind(d.Elem) && c.cfg.Profile == ProfileTiny {
		for i := uint32(0); i < length; i++ {
			if err := c.storeArrayValue(ref, d, start+i, value); err != nil {
				return err
			}
		}
		return nil
	}
	payload := c.bytes(ref)[PayloadOffset:]
	lo := uint64(start) * uint64(d.ElemSize)
	hi := lo + uint64(length)*uint64(d.ElemSize)
	rangeBytes := payload[lo:hi]
	var pattern [8]byte
	binary.LittleEndian.PutUint64(pattern[:], value.Bits)
	if isCollectorRefKind(d.Elem) {
		binary.LittleEndian.PutUint32(pattern[:4], uint32(value.Ref))
	}
	copy(rangeBytes, pattern[:d.ElemSize])
	for filled := int(d.ElemSize); filled < len(rangeBytes); {
		n := copy(rangeBytes[filled:], rangeBytes[:filled])
		filled += n
	}
	if isCollectorRefKind(d.Elem) {
		c.PostBulkWriteBarrier(ref, start, length)
	}
	return nil
}

// ArrayCopy preflights both ranges and the complete reference payload before
// mutation. Same-array overlap is copied in memmove order without allocating a
// temporary buffer. It does not allocate or collect.
// ArrayInitData preflights the complete destination element range and source
// byte range before mutation. It does not allocate or collect. Source bytes are
// decoded little-endian according to the destination element width.
func (c *Collector) ArrayInitData(dst Ref, dstStart uint32, data []byte, srcStart uint32, length uint32) error {
	d, err := c.refDesc(dst)
	if err != nil {
		return err
	}
	if d.Kind != KindArray || !isNumericStorage(d.Elem) {
		return errors.New("gc: array.init_data destination is not numeric")
	}
	dstLen := c.header(dst).Aux
	if uint64(dstStart)+uint64(length) > uint64(dstLen) {
		return errRange
	}
	byteLength := uint64(length) * uint64(d.ElemSize)
	sourceEnd := uint64(srcStart) + byteLength
	if sourceEnd > uint64(len(data)) {
		return errors.New("gc: data source out of range")
	}
	switch d.ElemSize {
	case 1, 2, 4, 8:
	default:
		return errors.New("gc: array.init_data element width is unsupported")
	}
	dstOffset := uint64(PayloadOffset) + uint64(dstStart)*uint64(d.ElemSize)
	copy(c.bytes(dst)[dstOffset:dstOffset+byteLength], data[uint64(srcStart):sourceEnd])
	return nil
}

// ArrayInitWords preflights an i64 array destination before storing the
// caller-provided words. It does not allocate, collect, scan, or barrier: the
// words are non-collector identities owned by the caller's exact product.
func (c *Collector) ArrayInitWords(dst Ref, dstStart uint32, words []uint64) error {
	d, err := c.refDesc(dst)
	if err != nil {
		return err
	}
	if d.Kind != KindArray || d.Elem != StorageI64 {
		return errors.New("gc: array word destination is not i64")
	}
	dstLen := c.header(dst).Aux
	if uint64(dstStart)+uint64(len(words)) > uint64(dstLen) {
		return errRange
	}
	b := c.bytes(dst)
	off := uint64(PayloadOffset) + uint64(dstStart)*8
	for _, bits := range words {
		binary.LittleEndian.PutUint64(b[off:off+8], bits)
		off += 8
	}
	return nil
}

func (c *Collector) ArrayCopy(dst Ref, dstStart uint32, src Ref, srcStart uint32, length uint32) error {
	dstDesc, err := c.refDesc(dst)
	if err != nil {
		return err
	}
	srcDesc, err := c.refDesc(src)
	if err != nil {
		return err
	}
	if dstDesc.Kind != KindArray || srcDesc.Kind != KindArray {
		return errors.New("gc: not array")
	}
	if !arrayStorageCopyCompatible(dstDesc.Elem, srcDesc.Elem) {
		return errors.New("gc: array element types do not match")
	}
	dstLen, srcLen := c.header(dst).Aux, c.header(src).Aux
	if uint64(dstStart)+uint64(length) > uint64(dstLen) || uint64(srcStart)+uint64(length) > uint64(srcLen) {
		return errRange
	}
	if isCollectorRefKind(dstDesc.Elem) && (c.cfg.VerifyAfterCollect || c.cfg.StressBarriers) {
		// Valid source arrays already satisfy their storage invariant. Repeat the
		// expensive element integrity check only in explicit hardening modes.
		for i := uint32(0); i < length; i++ {
			value, err := c.loadValue(src, uint64(PayloadOffset)+uint64(srcStart+i)*uint64(srcDesc.ElemSize), srcDesc.Elem)
			if err != nil {
				return err
			}
			if err := c.validateArrayStore(dstDesc, value); err != nil {
				return err
			}
		}
	}
	if length == 0 {
		return nil
	}
	if isCollectorRefKind(dstDesc.Elem) && c.cfg.Profile == ProfileTiny {
		copyOne := func(dstIndex, srcIndex uint32) error {
			value, err := c.loadValue(src, uint64(PayloadOffset)+uint64(srcIndex)*uint64(srcDesc.ElemSize), srcDesc.Elem)
			if err != nil {
				return err
			}
			return c.storeArrayValue(dst, dstDesc, dstIndex, value)
		}
		if dst == src && dstStart > srcStart && uint64(dstStart) < uint64(srcStart)+uint64(length) {
			for i := length; i > 0; i-- {
				if err := copyOne(dstStart+i-1, srcStart+i-1); err != nil {
					return err
				}
			}
		} else {
			for i := uint32(0); i < length; i++ {
				if err := copyOne(dstStart+i, srcStart+i); err != nil {
					return err
				}
			}
		}
		return nil
	}
	width := uint64(dstDesc.ElemSize)
	dstOff := uint64(PayloadOffset) + uint64(dstStart)*width
	srcOff := uint64(PayloadOffset) + uint64(srcStart)*uint64(srcDesc.ElemSize)
	byteLen := uint64(length) * width
	copy(c.bytes(dst)[dstOff:dstOff+byteLen], c.bytes(src)[srcOff:srcOff+byteLen])
	if isCollectorRefKind(dstDesc.Elem) {
		c.PostBulkWriteBarrier(dst, dstStart, length)
	}
	return nil
}

func (c *Collector) validateArrayStore(d TypeDesc, value Value) error {
	if err := checkValueCompatible(d.Elem, value); err != nil {
		return err
	}
	if isCollectorRefKind(d.Elem) {
		return c.validateStoredRef(value.Ref, isNullableReferenceStorage(d.Elem))
	}
	return nil
}

func (c *Collector) storeArrayValue(ref Ref, d TypeDesc, index uint32, value Value) error {
	if isCollectorRefKind(d.Elem) {
		c.WriteBarrierObject(ref, value.Ref)
		c.CardMarkArray(ref, index)
		return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

func arrayStorageCopyCompatible(dst, src StorageKind) bool {
	if isAnyReferenceStorage(dst) || isAnyReferenceStorage(src) {
		return referenceStorageCompatible(dst, src)
	}
	return dst == src && isNumericStorage(dst)
}
