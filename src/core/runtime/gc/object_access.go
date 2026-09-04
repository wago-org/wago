package gc

func (c *Collector) NewStruct(id TypeID) (Ref, error) { return c.NewStructDefault(id) }
func (c *Collector) NewStructDefault(id TypeID) (Ref, error) {
	return c.NewStructDefaultWithRoots(id, nil)
}
func (c *Collector) NewStructDefaultWithRoots(id TypeID, roots RootSet) (Ref, error) {
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewStructDefaultWithRoots(id, r))
}
func (c *Collector) NewStructWithRoots(id TypeID, values []Value, roots RootSet) (Ref, error) {
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	input, err := c.inputs(values)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewStructWithRoots(id, input, r))
}
func (c *Collector) NewArray(id TypeID, length uint32, init Value) (Ref, error) {
	return c.NewArrayWithRoots(id, length, init, nil)
}
func (c *Collector) NewArrayWithRoots(id TypeID, length uint32, init Value, roots RootSet) (Ref, error) {
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	input, err := c.input(init)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewArrayWithRoots(id, length, input, r))
}
func (c *Collector) NewArrayFixedWithRoots(id TypeID, values []Value, roots RootSet) (Ref, error) {
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	input, err := c.inputs(values)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewArrayFixedWithRoots(id, input, r))
}
func (c *Collector) NewArrayDefault(id TypeID, length uint32) (Ref, error) {
	return c.NewArrayDefaultWithRoots(id, length, nil)
}
func (c *Collector) NewArrayDefaultWithRoots(id TypeID, length uint32, roots RootSet) (Ref, error) {
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewArrayDefaultWithRoots(id, length, r))
}
func (c *Collector) NewRefArrayWithRoots(id TypeID, length uint32, initial RootSlot, roots RootSet) (Ref, error) {
	if err := c.available(); err != nil {
		return Ref{}, err
	}
	if initial == nil {
		return Ref{}, ErrInvalidReference
	}
	value := initial.GetRef()
	r, err := c.roots(roots)
	if err != nil {
		return Ref{}, err
	}
	input, err := c.input(RefValue(value))
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.NewArrayWithRoots(id, length, input, r))
}

func (c *Collector) ObjectType(ref Ref) (TypeID, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return 0, err
	}
	return c.heap.ObjectType(r)
}
func (c *Collector) ArrayLen(ref Ref) (uint32, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return 0, err
	}
	return c.heap.ArrayLen(r)
}
func (c *Collector) StructGet(ref Ref, field uint32) (Value, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return Value{}, err
	}
	return c.output(c.heap.StructGet(r, field))
}
func (c *Collector) ArrayGet(ref Ref, index uint32) (Value, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return Value{}, err
	}
	return c.output(c.heap.ArrayGet(r, index))
}
func (c *Collector) StructSet(ref Ref, field uint32, value Value) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	v, err := c.input(value)
	if err != nil {
		return err
	}
	return c.heap.StructSet(r, field, v)
}
func (c *Collector) ArraySet(ref Ref, index uint32, value Value) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	v, err := c.input(value)
	if err != nil {
		return err
	}
	return c.heap.ArraySet(r, index, v)
}
func (c *Collector) ArrayFill(ref Ref, start uint32, value Value, length uint32) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	v, err := c.input(value)
	if err != nil {
		return err
	}
	return c.heap.ArrayFill(r, start, v, length)
}
func (c *Collector) ArrayCopy(dst Ref, dstStart uint32, src Ref, srcStart, length uint32) error {
	d, err := c.unwrap(dst)
	if err != nil {
		return err
	}
	s, err := c.unwrap(src)
	if err != nil {
		return err
	}
	return c.heap.ArrayCopy(d, dstStart, s, srcStart, length)
}
func (c *Collector) ArrayInitData(dst Ref, dstStart uint32, data []byte, srcStart, length uint32) error {
	d, err := c.unwrap(dst)
	if err != nil {
		return err
	}
	return c.heap.ArrayInitData(d, dstStart, data, srcStart, length)
}
func (c *Collector) ForcePromote(ref Ref) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	return c.heap.ForcePromote(r)
}
func (c *Collector) CheckArrayAllocation(id TypeID, length uint32) error {
	if err := c.available(); err != nil {
		return err
	}
	return c.heap.CheckArrayAllocation(id, length)
}

func (c *Collector) RefTest(ref Ref, target RefTestTarget) (bool, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return false, err
	}
	return c.heap.RefTest(r, target)
}
func (c *Collector) RefCast(ref Ref, target RefTestTarget) (Ref, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.RefCast(r, target))
}
func (c *Collector) RefTestCanonical(ref Ref, target RefTestTarget, canonical *TypeCanonicalization) (bool, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return false, err
	}
	return c.heap.RefTestCanonical(r, target, canonical)
}
func (c *Collector) RefCastCanonical(ref Ref, target RefTestTarget, canonical *TypeCanonicalization) (Ref, error) {
	r, err := c.unwrap(ref)
	if err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.RefCastCanonical(r, target, canonical))
}
func (c *Collector) NewTypeCanonicalization(types []TypeID) (*TypeCanonicalization, error) {
	if err := c.available(); err != nil {
		return nil, err
	}
	return c.heap.NewTypeCanonicalization(types)
}
func (c *Collector) TypeSubtype(actual, required TypeID) (bool, error) {
	if err := c.available(); err != nil {
		return false, err
	}
	return c.heap.TypeSubtype(actual, required)
}

func (c *Collector) NewCheckedGlobalSlot(initial Ref) (uint32, error) {
	r, err := c.unwrap(initial)
	if err != nil {
		return 0, err
	}
	return c.heap.NewCheckedGlobalSlot(r)
}
func (c *Collector) NewCheckedClassifiedGlobalSlot(initial Ref, class RootClass) (uint32, error) {
	r, err := c.unwrap(initial)
	if err != nil {
		return 0, err
	}
	return c.heap.NewCheckedClassifiedGlobalSlot(r, class)
}
func (c *Collector) SetGlobalSlot(index uint32, ref Ref) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	return c.heap.SetGlobalSlot(index, r)
}
func (c *Collector) CheckedGlobalSlot(index uint32) (Ref, error) {
	if err := c.available(); err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.CheckedGlobalSlot(index))
}
func (c *Collector) NewCheckedTableSlot(initial Ref) (uint32, error) {
	r, err := c.unwrap(initial)
	if err != nil {
		return 0, err
	}
	return c.heap.NewCheckedTableSlot(r)
}
func (c *Collector) SetTableSlot(index uint32, ref Ref) error {
	r, err := c.unwrap(ref)
	if err != nil {
		return err
	}
	return c.heap.SetTableSlot(index, r)
}
func (c *Collector) CheckedTableSlot(index uint32) (Ref, error) {
	if err := c.available(); err != nil {
		return Ref{}, err
	}
	return c.wrap(c.heap.CheckedTableSlot(index))
}
