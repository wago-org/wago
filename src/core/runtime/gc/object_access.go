package gc

import (
	"encoding/binary"
	"errors"
)

func (c *Collector) NewStruct(typeID TypeID) (Ref, error) { return c.NewStructDefault(typeID) }
func (c *Collector) NewStructDefault(typeID TypeID) (Ref, error) {
	return c.NewStructDefaultWithRoots(typeID, nil)
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
func (c *Collector) NewArrayWithRoots(typeID TypeID, length uint32, init Value, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if err := checkValueCompatible(d.Elem, init); err != nil {
		return Null(), err
	}
	if isRefKind(d.Elem) {
		if err := c.validateStoredRef(init.Ref, d.Elem == StorageRefNull); err != nil {
			return Null(), err
		}
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	var initRoot *Root
	if isRefKind(d.Elem) && init.Ref.IsObj() {
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
	if !isRefKind(d.Elem) || init == nil {
		return Null(), errors.New("gc: reference array initializer root required")
	}
	value := RefValue(init.GetRef())
	if err := c.validateStoredRef(value.Ref, d.Elem == StorageRefNull); err != nil {
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
	if isRefKind(f.Kind) {
		if err := c.validateStoredRef(value.Ref, f.Kind == StorageRefNull); err != nil {
			return err
		}
		childIsNursery := c.isNurseryRef(value.Ref)
		c.WriteBarrierObject(ref, value.Ref)
		if err := c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value); err != nil {
			return err
		}
		if !childIsNursery {
			c.pruneRememberedHandleUnlessNursery(handleOf(ref))
		}
		return nil
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
// any write. It does not allocate or collect. Reference writes use the ordinary
// object/card barrier per element and the post-write bulk barrier for the range.
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
	for i := uint32(0); i < length; i++ {
		if err := c.storeArrayValue(ref, d, start+i, value); err != nil {
			return err
		}
	}
	if isRefKind(d.Elem) {
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
	if d.Kind != KindArray || isRefKind(d.Elem) {
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
	valueKind := d.Elem
	if valueKind == StorageI8 || valueKind == StorageI16 {
		valueKind = StorageI32
	}
	for i := uint32(0); i < length; i++ {
		off := uint64(srcStart) + uint64(i)*uint64(d.ElemSize)
		var bits uint64
		switch d.ElemSize {
		case 1:
			bits = uint64(data[off])
		case 2:
			bits = uint64(binary.LittleEndian.Uint16(data[off : off+2]))
		case 4:
			bits = uint64(binary.LittleEndian.Uint32(data[off : off+4]))
		case 8:
			bits = binary.LittleEndian.Uint64(data[off : off+8])
		default:
			return errors.New("gc: array.init_data element width is unsupported")
		}
		if err := c.storeArrayValue(dst, d, dstStart+i, Value{Kind: valueKind, Bits: bits}); err != nil {
			return err
		}
	}
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
	for i, bits := range words {
		if err := c.storeValue(dst, d, uint64(PayloadOffset)+uint64(dstStart+uint32(i))*uint64(d.ElemSize), d.Elem, Value{Kind: StorageI64, Bits: bits}); err != nil {
			return err
		}
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
	if isRefKind(dstDesc.Elem) {
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
	if isRefKind(dstDesc.Elem) {
		c.PostBulkWriteBarrier(dst, dstStart, length)
	}
	return nil
}

func (c *Collector) validateArrayStore(d TypeDesc, value Value) error {
	if err := checkValueCompatible(d.Elem, value); err != nil {
		return err
	}
	if isRefKind(d.Elem) {
		return c.validateStoredRef(value.Ref, d.Elem == StorageRefNull)
	}
	return nil
}

func (c *Collector) storeArrayValue(ref Ref, d TypeDesc, index uint32, value Value) error {
	if isRefKind(d.Elem) {
		childIsNursery := c.isNurseryRef(value.Ref)
		c.WriteBarrierObject(ref, value.Ref)
		c.CardMarkArray(ref, index)
		if err := c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value); err != nil {
			return err
		}
		if !childIsNursery {
			c.pruneRememberedHandleUnlessNursery(handleOf(ref))
		}
		return nil
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

func arrayStorageCopyCompatible(dst, src StorageKind) bool {
	if dst == src {
		return true
	}
	return dst == StorageRefNull && src == StorageRef
}
