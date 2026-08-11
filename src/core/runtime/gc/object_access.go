package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) NewStruct(typeID TypeID) (Ref, error) { return c.NewStructDefault(typeID) }
func (c *Collector) NewStructDefault(typeID TypeID) (Ref, error) {
	return c.NewStructDefaultWithRoots(typeID, nil)
}

// NewStructWithRoots allocates and initializes every field atomically from the
// caller's values. Reference operands are published as temporary mutable roots
// across a collection-triggering allocation and reread before object stores.
func (c *Collector) NewStructWithRoots(typeID TypeID, values []Value, roots RootSet) (Ref, error) {
	return c.newStructWithRoots(typeID, values, roots, nil)
}

// NewStructWithRootScratch is NewStructWithRoots with reusable caller-owned root
// composition storage. The scratch must not be shared across concurrent calls.
func (c *Collector) NewStructWithRootScratch(typeID TypeID, values []Value, roots RootSet, scratch *InitializerRootScratch) (Ref, error) {
	return c.newStructWithRoots(typeID, values, roots, scratch)
}

// NewStructWordsPrevalidatedWithRootScratch constructs directly from helper ABI
// words after exact field validation. The mutable words are rooted and reread if
// allocation moves an initializer object, avoiding an intermediate []Value walk.
func (c *Collector) NewStructWordsPrevalidatedWithRootScratch(typeID TypeID, words []uint64, roots RootSet, scratch *InitializerWordRootScratch) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindStruct {
		return Null(), errors.New("gc: struct initializer shape mismatch")
	}
	cursor := 0
	hasObjectRefs := false
	for _, field := range d.Fields {
		if cursor >= len(words) {
			return Null(), errors.New("gc: struct initializer word shape mismatch")
		}
		if isCollectorRefKind(field.Kind) {
			hasObjectRefs = hasObjectRefs || Ref(uint32(words[cursor])).IsObj()
		}
		cursor++
		if field.Kind == StorageV128 {
			cursor++
		}
	}
	if cursor != len(words) {
		return Null(), errors.New("gc: struct initializer word shape mismatch")
	}
	if hasObjectRefs {
		if !scratch.prepare(roots, words, d.Fields) {
			return Null(), errors.New("gc: struct initializer word root scratch is already in use")
		}
		defer scratch.clear()
		roots = scratch
	}
	sz := Align8(HeaderSize + d.Size)
	r, err := c.alloc(d, sz, 0, roots)
	if err != nil {
		return Null(), err
	}
	payload := c.bytes(r)[PayloadOffset:]
	cursor = 0
	for _, field := range d.Fields {
		slots := 1
		if field.Kind == StorageV128 {
			slots = 2
		}
		storeWordsUnchecked(payload, uint64(field.Offset), field.Kind, words[cursor:cursor+slots])
		cursor += slots
	}
	if d.HasRefs && c.cfg.Profile != ProfileTiny {
		h := handleOf(r)
		if (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) && c.handleContainsNurseryRef(h) {
			c.remember(h)
			c.markWholeObjectCard(h)
		}
	}
	return r, nil
}

func (c *Collector) newStructWithRoots(typeID TypeID, values []Value, roots RootSet, scratch *InitializerRootScratch) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindStruct || len(values) != len(d.Fields) {
		return Null(), errors.New("gc: struct initializer shape mismatch")
	}
	hasRefs := d.HasRefs
	hasObjectRefs := false
	for i, field := range d.Fields {
		if err := checkValueCompatible(field.Kind, values[i]); err != nil {
			return Null(), err
		}
		if isCollectorRefKind(field.Kind) {
			if err := c.validateStoredRef(values[i].Ref, isNullableReferenceStorage(field.Kind)); err != nil {
				return Null(), err
			}
			hasObjectRefs = hasObjectRefs || values[i].Ref.IsObj()
		}
	}
	if hasObjectRefs {
		if scratch != nil {
			if !scratch.prepare(roots, values, d.Fields) {
				return Null(), errors.New("gc: struct initializer root scratch is already in use")
			}
			defer scratch.clear()
			roots = scratch
		} else {
			roots = combineRootSets(roots, valueRootSet{values: values, fields: d.Fields})
		}
	}
	// Collector construction validates every descriptor and padded size, so this
	// initialized-constructor hot path need not repeat overflow arithmetic.
	sz := Align8(HeaderSize + d.Size)
	r, err := c.alloc(d, sz, 0, roots)
	if err != nil {
		return Null(), err
	}
	// Every field is written below; padding is not observable or scanned.
	payload := c.bytes(r)[PayloadOffset:]
	for i, field := range d.Fields {
		// Shape, kind, compact-reference ownership, nullability, and complete
		// object size were preflighted before allocation. Store directly so large
		// fixed-shape constructors do not repeat those checks for every field.
		storeValueUnchecked(payload, uint64(field.Offset), field.Kind, values[i])
	}
	// Initialization publishes the complete payload at once. A large allocation
	// is born outside the nursery, so reconcile one remembered-set entry after all
	// direct stores instead of paying a barrier per field.
	if hasRefs && c.cfg.Profile != ProfileTiny {
		h := handleOf(r)
		if (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) && c.handleContainsNurseryRef(h) {
			c.remember(h)
			c.markWholeObjectCard(h)
		}
	}
	return r, nil
}

// NewStructUninitializedWithRoots allocates a zeroed reconstruction object
// without applying Wasm's defaultability rule. The object must remain in roots
// and every non-null field must be initialized before it becomes observable or
// before roots are released. Cross-domain graph cloning uses this bounded
// two-pass primitive to restore cycles.
func (c *Collector) NewStructUninitializedWithRoots(typeID TypeID, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
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

// ReserveDeadStructAllocation performs the allocation side effect of a dropped
// struct.new without populating its unreachable fields. It returns the real
// compact allocation so payload-safe nested reservations can root the result
// across later allocations. A caller must not use the zeroed result as a nested
// substitute for a reference-initialized object, because that would remove its
// transitive child roots. Unlike NewStructDefaultWithRoots, this intentionally
// does not apply defaultability: struct.new operands have already been validated
// and evaluated by the caller.
func (c *Collector) ReserveDeadStructAllocation(typeID TypeID, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
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

// CheckArrayAllocation validates the deterministic type/size traps of an array
// constructor without allocating collector state. Dead-constructor lowering uses
// it after all Wasm operands have executed so dynamic size overflow is preserved.
func (c *Collector) CheckArrayAllocation(typeID TypeID, length uint32) error {
	d, err := c.desc(typeID)
	if err != nil {
		return err
	}
	size, err := ArraySize(d, length)
	if err != nil {
		return err
	}
	if c.cfg.Profile == ProfileTiny {
		if uint64(size) > uint64(len(c.tiny.mem)) {
			return errTinyHeapExhausted
		}
		return nil
	}
	// Throughput rounds every physical allocation to sixteen bytes. ArraySize's
	// eight-byte object ABI rounding can still leave this final round overflowing.
	if size > ^uint32(0)-15 {
		return ErrAllocationTooLarge
	}
	if uint64(Align16(size)) > uint64(c.throughput.limit) {
		return errThroughputHeapExhausted
	}
	return nil
}

// ReserveDeadArrayAllocation performs the allocation side effect of a dropped
// dynamic constructor without populating its unreachable payload. Unlike
// CheckArrayAllocation, this preserves exhaustion, collection, handle-table,
// allocation-counter, and future-capacity behavior under an already occupied
// bounded heap. The returned compact allocation may be retained by payload-safe
// nested reservations; reference-initialized intermediate arrays must use their
// full constructor so transitive child roots remain visible. The zero payload
// keeps verification and accidental diagnostics safe until the unreachable
// object is collected.
func (c *Collector) ReserveDeadArrayAllocation(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	size, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	r, err := c.alloc(d, size, length, roots)
	if err != nil {
		return Null(), err
	}
	c.zeroObjectPayload(r)
	return r, nil
}

// ReserveDeadDefaultArrayAllocation additionally preserves the defaultability
// check required by array.new_default before reserving its dropped allocation.
func (c *Collector) ReserveDeadDefaultArrayAllocation(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if length != 0 {
		if err := checkDefaultable(d); err != nil {
			return Null(), err
		}
	}
	return c.ReserveDeadArrayAllocation(typeID, length, roots)
}

// NewArrayFixedWithRoots allocates an array initialized from one value per
// element. Reference operands are rooted across allocation and reread before
// stores, matching the atomic operand lifetime required by array.new_fixed.
func (c *Collector) NewArrayFixedWithRoots(typeID TypeID, values []Value, roots RootSet) (Ref, error) {
	return c.newArrayFixedWithRoots(typeID, values, roots, false, false)
}

// NewArrayFixedPrevalidatedWithRoots is the runtime-helper counterpart after
// exact element kind, collector ownership, subtype, and nullability validation.
func (c *Collector) NewArrayFixedPrevalidatedWithRoots(typeID TypeID, values []Value, roots RootSet) (Ref, error) {
	return c.newArrayFixedWithRoots(typeID, values, roots, true, false)
}

// NewArrayFixedWithRootScratch removes interface-composition allocations for
// repeated reference-array construction. scratch must be caller-owned and must
// not be used concurrently.
func (c *Collector) NewArrayFixedWithRootScratch(typeID TypeID, values []Value, roots RootSet, scratch *ArrayInitializerRootScratch) (Ref, error) {
	return c.newArrayFixedWithRootScratch(typeID, values, roots, scratch, false)
}

// NewArrayFixedPrevalidatedWithRootScratch combines the validated runtime-helper
// path with reusable root composition.
func (c *Collector) NewArrayFixedPrevalidatedWithRootScratch(typeID TypeID, values []Value, roots RootSet, scratch *ArrayInitializerRootScratch) (Ref, error) {
	return c.newArrayFixedWithRootScratch(typeID, values, roots, scratch, true)
}

func (c *Collector) newArrayFixedWithRootScratch(typeID TypeID, values []Value, roots RootSet, scratch *ArrayInitializerRootScratch, prevalidated bool) (Ref, error) {
	if scratch == nil {
		return c.newArrayFixedWithRoots(typeID, values, roots, prevalidated, false)
	}
	if !scratch.prepareFixed(roots, values) {
		return Null(), errors.New("gc: array initializer root scratch is already active")
	}
	defer scratch.clear()
	return c.newArrayFixedWithRoots(typeID, values, scratch, prevalidated, true)
}

func (c *Collector) newArrayFixedWithRoots(typeID TypeID, values []Value, roots RootSet, prevalidated, valuesRooted bool) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindArray {
		return Null(), errors.New("gc: not array")
	}
	hasObjectRefs := false
	for i := range values {
		if !prevalidated {
			if err := c.validateArrayStore(d, values[i]); err != nil {
				return Null(), err
			}
		}
		hasObjectRefs = hasObjectRefs || (isCollectorRefKind(d.Elem) && values[i].Ref.IsObj())
	}
	if hasObjectRefs && !valuesRooted {
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
	payload := c.bytes(r)[PayloadOffset:]
	for i := range values {
		storeValueUnchecked(payload, uint64(i)*uint64(d.ElemSize), d.Elem, values[i])
	}
	if isCollectorRefKind(d.Elem) {
		c.PostBulkWriteBarrier(r, 0, uint32(len(values)))
	}
	return r, nil
}

// NewArrayWithRootScratch is the allocation-free repeated-construction form of
// NewArrayWithRoots for reference arrays. Numeric arrays ignore scratch.
func (c *Collector) NewArrayWithRootScratch(typeID TypeID, length uint32, init Value, roots RootSet, scratch *ArrayInitializerRootScratch) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if !isCollectorRefKind(d.Elem) || !init.Ref.IsObj() || scratch == nil {
		return c.NewArrayWithRoots(typeID, length, init, roots)
	}
	if err := checkValueCompatible(d.Elem, init); err != nil {
		return Null(), err
	}
	if err := c.validateStoredRef(init.Ref, isNullableReferenceStorage(d.Elem)); err != nil {
		return Null(), err
	}
	sz, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	if !scratch.prepareUniform(roots, init.Ref) {
		return Null(), errors.New("gc: array initializer root scratch is already active")
	}
	defer scratch.clear()
	r, err := c.alloc(d, sz, length, scratch)
	if err != nil {
		return Null(), err
	}
	init.Ref = scratch.uniform
	if err := c.fillArrayPayload(r, d, 0, init, length); err != nil {
		return Null(), err
	}
	c.PostBulkWriteBarrier(r, 0, length)
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
	if err := c.fillArrayPayload(r, d, 0, init, length); err != nil {
		return Null(), err
	}
	if isCollectorRefKind(d.Elem) {
		c.PostBulkWriteBarrier(r, 0, length)
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
	if err := c.fillArrayPayload(r, d, 0, value, length); err != nil {
		return Null(), err
	}
	c.PostBulkWriteBarrier(r, 0, length)
	return r, nil
}

func (c *Collector) NewArrayDefault(typeID TypeID, length uint32) (Ref, error) {
	return c.NewArrayDefaultWithRoots(typeID, length, nil)
}

// NewArrayUninitializedWithRoots is the array counterpart of
// NewStructUninitializedWithRoots for transactional graph reconstruction.
func (c *Collector) NewArrayUninitializedWithRoots(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
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
	c.zeroObjectPayload(r)
	return r, nil
}

func (c *Collector) NewArrayDefaultWithRoots(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	return c.newArrayDefaultWithRoots(typeID, length, roots, false)
}

// NewArrayDefaultPrevalidatedWithRoots skips the repeated defaultability walk
// after validated Wasm array.new_default lowering has already proved it.
func (c *Collector) NewArrayDefaultPrevalidatedWithRoots(typeID TypeID, length uint32, roots RootSet) (Ref, error) {
	return c.newArrayDefaultWithRoots(typeID, length, roots, true)
}

func (c *Collector) newArrayDefaultWithRoots(typeID TypeID, length uint32, roots RootSet, prevalidated bool) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if !prevalidated && length != 0 {
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

// ArrayLenTyped combines the dynamic type check and length load after resolving
// the compact object handle once. exact is valid for final required types.
func (c *Collector) ArrayLenTyped(ref Ref, required TypeID, exact bool) (length uint32, actual TypeID, matched bool, err error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return 0, 0, false, err
	}
	actual = d.ID
	matched, err = c.typeDescSubtype(d, required, exact)
	if err != nil || !matched {
		return 0, actual, matched, err
	}
	if d.Kind != KindArray {
		return 0, actual, true, errors.New("gc: not array")
	}
	return c.header(ref).Aux, actual, true, nil
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

// StructGetTyped combines dynamic type checking and field access after resolving
// the compact object handle once. exact is valid for final required types; open
// types retain declared-super traversal. actual is returned for precise caller
// diagnostics when matched is false.
func (c *Collector) StructGetTyped(ref Ref, required TypeID, exact bool, field uint32) (value Value, actual TypeID, matched bool, err error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return Value{}, 0, false, err
	}
	actual = d.ID
	matched, err = c.typeDescSubtype(d, required, exact)
	if err != nil || !matched {
		return Value{}, actual, matched, err
	}
	if d.Kind != KindStruct {
		return Value{}, actual, true, errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return Value{}, actual, true, errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	value, err = c.loadValue(ref, uint64(PayloadOffset+f.Offset), f.Kind)
	return value, actual, true, err
}

// StructGetFinalRef is the exact-final reference-field counterpart of
// StructGetTyped. It validates the required canonical descriptor once by pointer,
// resolves the compact handle once, and loads the four-byte collector reference
// directly. It deliberately does not perform declared-super traversal: callers
// may use it only for statically final required struct types.
func (c *Collector) StructGetFinalRef(ref Ref, required TypeID, field uint32) (value Ref, matched bool, err error) {
	if err := c.errIfClosed(); err != nil {
		return Null(), false, err
	}
	if int(required) >= len(c.types) {
		return Null(), false, fmt.Errorf("gc: unknown type id %d", required)
	}
	requiredDesc := &c.types[required]
	if !requiredDesc.Final || requiredDesc.Kind != KindStruct {
		return Null(), false, errors.New("gc: final reference access requires final struct type")
	}
	if field >= uint32(len(requiredDesc.Fields)) {
		return Null(), false, errors.New("gc: field out of range")
	}
	f := requiredDesc.Fields[field]
	if !isCollectorRefKind(f.Kind) {
		return Null(), false, errors.New("gc: field is not collector reference")
	}
	if !ref.IsObj() {
		return Null(), false, errors.New("gc: ref is not object")
	}
	h := handleOf(ref)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return Null(), false, errors.New("gc: invalid object ref")
	}
	b := c.bytes(ref)
	if len(b) < int(HeaderSize) {
		return Null(), false, errors.New("gc: object header out of bounds")
	}
	if TypeID(binary.LittleEndian.Uint32(b)) != required {
		return Null(), false, nil
	}
	off := uint64(PayloadOffset) + uint64(f.Offset)
	if off > uint64(len(b)) || uint64(len(b))-off < 4 {
		return Null(), true, errors.New("gc: load out of bounds")
	}
	return Ref(binary.LittleEndian.Uint32(b[off:])), true, nil
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
		if err := c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value); err != nil {
			return err
		}
		if sp := c.entry(ref).space; c.cfg.Profile == ProfileTiny || sp == spaceOld || sp == spaceLarge {
			c.writeBarrierObjectRange(ref, value.Ref, f.Offset, f.Offset+3)
		}
		return nil
	}
	return c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value)
}

// StructSetTyped is StructGetTyped's mutation counterpart. It performs the
// exact/open type test, value validation, write barrier, and store from one
// resolved descriptor.
func (c *Collector) StructSetTyped(ref Ref, required TypeID, exact bool, field uint32, value Value) (actual TypeID, matched bool, err error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return 0, false, err
	}
	actual = d.ID
	matched, err = c.typeDescSubtype(d, required, exact)
	if err != nil || !matched {
		return actual, matched, err
	}
	if d.Kind != KindStruct {
		return actual, true, errors.New("gc: not struct")
	}
	if field >= uint32(len(d.Fields)) {
		return actual, true, errors.New("gc: field out of range")
	}
	f := d.Fields[field]
	if err := checkValueCompatible(f.Kind, value); err != nil {
		return actual, true, err
	}
	if isCollectorRefKind(f.Kind) {
		if err := c.validateStoredRef(value.Ref, isNullableReferenceStorage(f.Kind)); err != nil {
			return actual, true, err
		}
		if err := c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value); err != nil {
			return actual, true, err
		}
		if sp := c.entry(ref).space; c.cfg.Profile == ProfileTiny || sp == spaceOld || sp == spaceLarge {
			c.writeBarrierObjectRange(ref, value.Ref, f.Offset, f.Offset+3)
		}
		return actual, true, nil
	}
	return actual, true, c.storeValue(ref, d, uint64(PayloadOffset+f.Offset), f.Kind, value)
}

func (c *Collector) typeDescSubtype(dynamic TypeDesc, required TypeID, exact bool) (bool, error) {
	if exact {
		if dynamic.ID == required {
			return true, nil
		}
		_, err := c.desc(required)
		return false, err
	}
	want, err := c.desc(required)
	if err != nil {
		return false, err
	}
	if dynamic.Kind != want.Kind {
		return false, nil
	}
	return c.typeSubtypeIDs(dynamic.ID, required)
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

// ArrayGetTyped combines the dynamic array type test, bounds check, and element
// load after one compact-handle resolution.
func (c *Collector) ArrayGetTyped(ref Ref, required TypeID, exact bool, index uint32) (value Value, actual TypeID, matched bool, err error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return Value{}, 0, false, err
	}
	actual = d.ID
	matched, err = c.typeDescSubtype(d, required, exact)
	if err != nil || !matched {
		return Value{}, actual, matched, err
	}
	if d.Kind != KindArray {
		return Value{}, actual, true, errors.New("gc: not array")
	}
	if index >= c.header(ref).Aux {
		return Value{}, actual, true, errors.New("gc: index out of range")
	}
	value, err = c.loadValue(ref, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem)
	return value, actual, true, err
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

// ArraySetDeferredBarrier stores one preflighted bulk element without publishing
// a Throughput barrier. The caller must have validated every value before the
// first mutation and must invoke PostBulkWriteBarrier for the exact completed
// destination range before returning to Wasm. Tiny rejects this API because its
// incremental invariant requires each newly published edge to shade immediately.
func (c *Collector) ArraySetDeferredBarrier(ref Ref, index uint32, value Value) error {
	if c.cfg.Profile == ProfileTiny {
		return errors.New("gc: deferred array barrier is unavailable for Tiny")
	}
	d, err := c.refDesc(ref)
	if err != nil {
		return err
	}
	if d.Kind != KindArray || index >= c.header(ref).Aux {
		return errRange
	}
	if err := checkValueCompatible(d.Elem, value); err != nil {
		return err
	}
	// Production callers completed ownership/nullability preflight for the full
	// range. Hardening modes intentionally repeat that expensive check so tests can
	// catch misuse without charging every retained element twice in release paths.
	if isCollectorRefKind(d.Elem) && (c.cfg.VerifyAfterCollect || c.cfg.StressBarriers) {
		if err := c.validateStoredRef(value.Ref, isNullableReferenceStorage(d.Elem)); err != nil {
			return err
		}
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

// ArraySetTyped is ArrayGetTyped's mutation counterpart and retains reference
// validation and write-barrier behavior through storeArrayValue.
func (c *Collector) ArraySetTyped(ref Ref, required TypeID, exact bool, index uint32, value Value) (actual TypeID, matched bool, err error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return 0, false, err
	}
	actual = d.ID
	matched, err = c.typeDescSubtype(d, required, exact)
	if err != nil || !matched {
		return actual, matched, err
	}
	if d.Kind != KindArray {
		return actual, true, errors.New("gc: not array")
	}
	if index >= c.header(ref).Aux {
		return actual, true, errors.New("gc: index out of range")
	}
	if err := c.validateArrayStore(d, value); err != nil {
		return actual, true, err
	}
	return actual, true, c.storeArrayValue(ref, d, index, value)
}

// ArrayFill preflights the complete destination range and value before making
// any write. Throughput collectors mutate the compact payload directly and run
// one post-write range barrier. Tiny retains the scalar barrier while marking or
// sweeping because its incremental tri-color invariant is per published edge.
func (c *Collector) ArrayFill(ref Ref, start uint32, value Value, length uint32) error {
	return c.arrayFill(ref, start, value, length, true)
}

// ArrayFillNoBarrier performs the same complete preflight and atomic mutation
// as ArrayFill, but accepts only stores that cannot create a collector edge.
// It is the runtime guardrail for late compiler NoBarrier selection.
func (c *Collector) ArrayFillNoBarrier(ref Ref, start uint32, value Value, length uint32) error {
	return c.arrayFill(ref, start, value, length, false)
}

func (c *Collector) arrayFill(ref Ref, start uint32, value Value, length uint32, barrier bool) error {
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
	if !barrier && isCollectorRefKind(d.Elem) && value.Ref.IsObj() {
		return errors.New("gc: barrier-free array.fill cannot store an object reference")
	}
	if !barrier && isCollectorRefKind(d.Elem) {
		c.noteBarrierState(runtimeBarrierNoBarrier)
	}
	if length == 0 {
		return nil
	}
	if err := c.fillArrayPayload(ref, d, start, value, length); err != nil {
		return err
	}
	if barrier && isCollectorRefKind(d.Elem) {
		c.PostBulkWriteBarrier(ref, start, length)
	}
	return nil
}

func (c *Collector) fillArrayPayload(ref Ref, d TypeDesc, start uint32, value Value, length uint32) error {
	if length == 0 {
		return nil
	}
	payload := c.bytes(ref)[PayloadOffset:]
	lo := uint64(start) * uint64(d.ElemSize)
	hi := lo + uint64(length)*uint64(d.ElemSize)
	if lo > uint64(len(payload)) || hi > uint64(len(payload)) || hi < lo {
		return errRange
	}
	rangeBytes := payload[lo:hi]
	var pattern [16]byte
	binary.LittleEndian.PutUint64(pattern[:8], value.Bits)
	if d.Elem == StorageV128 {
		binary.LittleEndian.PutUint64(pattern[8:], value.BitsHi)
	}
	if isCollectorRefKind(d.Elem) {
		binary.LittleEndian.PutUint32(pattern[:4], uint32(value.Ref))
	}
	copy(rangeBytes, pattern[:d.ElemSize])
	for filled := int(d.ElemSize); filled < len(rangeBytes); {
		filled += copy(rangeBytes[filled:], rangeBytes[:filled])
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
	case 1, 2, 4, 8, 16:
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
		if c.cfg.Profile == ProfileTiny && c.tinySweepActive() {
			if err := c.validateStoredRef(value.Ref, isNullableReferenceStorage(d.Elem)); err != nil {
				return err
			}
		}
		if err := c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value); err != nil {
			return err
		}
		off := index * d.ElemSize
		if sp := c.entry(ref).space; c.cfg.Profile == ProfileTiny || sp == spaceOld || sp == spaceLarge {
			c.writeBarrierObjectRange(ref, value.Ref, off, off+d.ElemSize-1)
		}
		return nil
	}
	return c.storeValue(ref, d, uint64(PayloadOffset)+uint64(index)*uint64(d.ElemSize), d.Elem, value)
}

func arrayStorageCopyCompatible(dst, src StorageKind) bool {
	if isAnyReferenceStorage(dst) || isAnyReferenceStorage(src) {
		return referenceStorageCompatible(dst, src)
	}
	return dst == src && isNumericStorage(dst)
}
