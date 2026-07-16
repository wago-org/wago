package gc

import "errors"

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
	if err := checkValueCompatible(d.Elem, value); err != nil {
		return err
	}
	if isRefKind(d.Elem) {
		if err := c.validateStoredRef(value.Ref, d.Elem == StorageRefNull); err != nil {
			return err
		}
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
