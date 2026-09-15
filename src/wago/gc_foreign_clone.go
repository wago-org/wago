package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Foreign Runtime transfer uses explicit transactional graph cloning rather
// than sharing compact collector handles. These bounds cap temporary metadata,
// reconstruction roots, and denial-of-service exposure per call.
const (
	maxForeignGCCloneObjects = 1024
	maxForeignGCCloneValues  = 65536
	maxForeignGCCloneBytes   = 1 << 20
)

// gcCloneRef is a collector-independent reference used while copying an object
// graph. Object indexes are one-based so the zero value is null.
type gcCloneRef struct {
	kind  uint8
	value uint32
}

const (
	gcCloneRefNull uint8 = iota
	gcCloneRefI31
	gcCloneRefObject
)

type gcCloneValue struct {
	kind   gc.StorageKind
	bits   uint64
	bitsHi uint64
	ref    gcCloneRef
}

type gcCloneObject struct {
	typeID   gc.TypeID
	arrayLen uint32
	values   []gcCloneValue
}

func cloneGCDescriptor(c *Compiled, id gc.TypeID) (gc.TypeDesc, bool) {
	if c == nil || uint64(id) >= uint64(len(c.GCTypeDescs)) {
		return gc.TypeDesc{}, false
	}
	d := c.GCTypeDescs[id]

	return d, d.ID == id
}

// CloneGCRefFrom copies one host-retained WasmGC object graph from source's
// Runtime into target's distinct Runtime collector domain. Object identity,
// cycles, and internal sharing are preserved within the cloned graph, but the
// clone intentionally has a new identity and no aliasing with the source.
//
// Only structurally equivalent target-defined struct/array types, compact GC
// references, i31 immediates, numeric/v128 payloads, and null opaque references
// are transferable. Non-null funcref/externref payloads reject because they have
// independent store ownership. The returned token belongs to target and must be
// released with target.ReleaseGCRef. Cloning fails with ErrPermissionDenied while
// either instance has callback-scoped guest storage borrowed.
func (target *Instance) CloneGCRefFrom(source *Instance, value GCRef) (GCRef, error) {
	if target == nil || source == nil || target == source {
		return GCRef{}, fmt.Errorf("GC graph clone requires distinct source and target instances")
	}
	if value.token == 0 {
		return GCRef{}, nil
	}
	if source.guestStorageBorrowed() || target.guestStorageBorrowed() {
		return GCRef{}, fmt.Errorf("GC graph clone is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
	}
	if target.refStore == nil || source.refStore == nil || target.gc == nil || source.gc == nil || target.c == nil || source.c == nil {
		return GCRef{}, fmt.Errorf("GC graph clone requires live collector-backed instances")
	}
	if target.refStore == source.refStore {
		return GCRef{}, fmt.Errorf("GC graph clone requires distinct Runtime stores; use ValueGCRef inside one Runtime domain")
	}
	objects, root, err := captureForeignGCGraph(source, value.token, target)
	if err != nil {
		return GCRef{}, err
	}
	ref, localType, err := restoreForeignGCGraph(target, objects, root)
	if err != nil {
		return GCRef{}, err
	}
	required := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: localType},
	}}
	token, err := target.refStore.issueGCRef(target, ref, required)
	clearForeignCloneRoot(target, err != nil)
	if err != nil {
		return GCRef{}, fmt.Errorf("retain cloned GC graph: %w", err)
	}
	return GCRef{token: token}, nil
}

func captureForeignGCGraph(source *Instance, token uint64, target *Instance) ([]gcCloneObject, gcCloneRef, error) {
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := source.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := source.existingPublicGCState()
	if state == nil {
		return nil, gcCloneRef{}, fmt.Errorf("source GC token state is unavailable")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	source.refStore.mu.Lock()
	entry, ok := source.refStore.gcByToken[token]
	_, registered := source.refStore.instances[source]
	source.refStore.mu.Unlock()
	if !ok || entry.owner != source || !registered || entry.ownerIndex >= state.resultRootsMade || state.resultToken(entry.ownerIndex) != token || state.resultRootSlot(entry.ownerIndex) != entry.slot {
		return nil, gcCloneRef{}, fmt.Errorf("invalid, stale, or foreign source GC reference token")
	}
	rootRef := source.gc.GlobalSlot(entry.slot)
	if !rootRef.IsObj() {
		return nil, gcCloneRef{}, fmt.Errorf("source GC reference token has no live object root")
	}

	typeMap := make(map[uint32]uint32)
	mapType := func(sourceLocal uint32) (uint32, error) {
		if mapped, ok := typeMap[sourceLocal]; ok {
			return mapped, nil
		}
		if int(sourceLocal) >= len(source.c.Types) {
			return 0, fmt.Errorf("source object type %d is unavailable", sourceLocal)
		}
		for candidate := range target.c.Types {
			if definedTypeEquivalent(sourceLocal, source.c.Types, uint32(candidate), target.c.Types) {
				typeMap[sourceLocal] = uint32(candidate)
				return uint32(candidate), nil
			}
		}
		return 0, fmt.Errorf("target Runtime has no structurally equivalent type for source type %d", sourceLocal)
	}

	ids := make(map[gc.Ref]uint32)
	queue := make([]gc.Ref, 0, 16)
	encodeRef := func(ref gc.Ref) (gcCloneRef, error) {
		if ref.IsNull() {
			return gcCloneRef{}, nil
		}
		if ref.IsI31() {
			return gcCloneRef{kind: gcCloneRefI31, value: uint32(ref)}, nil
		}
		if id, exists := ids[ref]; exists {
			return gcCloneRef{kind: gcCloneRefObject, value: id}, nil
		}
		if len(queue) >= maxForeignGCCloneObjects {
			return gcCloneRef{}, fmt.Errorf("GC graph exceeds clone object bound %d", maxForeignGCCloneObjects)
		}
		if _, err := source.gc.ObjectType(ref); err != nil {
			return gcCloneRef{}, fmt.Errorf("source graph reference %#x: %w", uint32(ref), err)
		}
		id := uint32(len(queue) + 1)
		ids[ref] = id
		queue = append(queue, ref)
		return gcCloneRef{kind: gcCloneRefObject, value: id}, nil
	}
	root, err := encodeRef(rootRef)
	if err != nil {
		return nil, gcCloneRef{}, err
	}
	objects := make([]gcCloneObject, 0, 16)
	valueCount, payloadBytes := 0, uint64(0)
	for pos := 0; pos < len(queue); pos++ {
		ref := queue[pos]
		domainType, err := source.gc.ObjectType(ref)
		if err != nil {
			return nil, gcCloneRef{}, fmt.Errorf("source object %d type: %w", pos+1, err)
		}
		sourceLocal, ok := source.gcLocalType(domainType)
		if !ok {
			return nil, gcCloneRef{}, fmt.Errorf("source canonical type %d has no local structural identity", domainType)
		}
		targetLocal, err := mapType(sourceLocal)
		if err != nil {
			return nil, gcCloneRef{}, fmt.Errorf("source object %d: %w", pos+1, err)
		}
		sourceDesc, ok := cloneGCDescriptor(source.c, gc.TypeID(sourceLocal))
		if !ok || (sourceDesc.Kind != gc.KindStruct && sourceDesc.Kind != gc.KindArray) {
			return nil, gcCloneRef{}, fmt.Errorf("source object %d type %d is not transferable", pos+1, sourceLocal)
		}
		targetDesc, ok := cloneGCDescriptor(target.c, gc.TypeID(targetLocal))
		if !ok || targetDesc.Kind != sourceDesc.Kind {
			return nil, gcCloneRef{}, fmt.Errorf("target type %d has incompatible collector layout", targetLocal)
		}
		record := gcCloneObject{typeID: gc.TypeID(targetLocal)}
		var count uint32
		if sourceDesc.Kind == gc.KindStruct {
			count = uint32(len(sourceDesc.Fields))
		} else {
			count, err = source.gc.ArrayLen(ref)
			if err != nil {
				return nil, gcCloneRef{}, fmt.Errorf("source object %d array length: %w", pos+1, err)
			}
			record.arrayLen = count
		}
		if uint64(valueCount)+uint64(count) > maxForeignGCCloneValues {
			return nil, gcCloneRef{}, fmt.Errorf("GC graph exceeds clone value bound %d", maxForeignGCCloneValues)
		}
		valueCount += int(count)
		record.values = make([]gcCloneValue, count)
		for i := uint32(0); i < count; i++ {
			var value gc.Value
			if sourceDesc.Kind == gc.KindStruct {
				value, err = source.gc.StructGet(ref, i)
			} else {
				value, err = source.gc.ArrayGet(ref, i)
			}
			if err != nil {
				return nil, gcCloneRef{}, fmt.Errorf("source object %d value %d: %w", pos+1, i, err)
			}
			entry := gcCloneValue{kind: value.Kind, bits: value.Bits, bitsHi: value.BitsHi}
			switch value.Kind {
			case gc.StorageRef, gc.StorageRefNull:
				entry.ref, err = encodeRef(value.Ref)
				entry.bits = 0
				payloadBytes += 4
			case gc.StorageFuncRef, gc.StorageFuncRefNull, gc.StorageExternRef, gc.StorageExternRefNull:
				if value.Bits != 0 {
					return nil, gcCloneRef{}, fmt.Errorf("source object %d value %d contains non-transferable non-null opaque reference", pos+1, i)
				}
				payloadBytes += 8
			case gc.StorageV128:
				payloadBytes += 16
			case gc.StorageI8:
				payloadBytes++
			case gc.StorageI16:
				payloadBytes += 2
			case gc.StorageI32, gc.StorageF32:
				payloadBytes += 4
			default:
				payloadBytes += 8
			}
			if err != nil {
				return nil, gcCloneRef{}, fmt.Errorf("source object %d value %d: %w", pos+1, i, err)
			}
			if payloadBytes > maxForeignGCCloneBytes {
				return nil, gcCloneRef{}, fmt.Errorf("GC graph exceeds clone payload bound %d bytes", maxForeignGCCloneBytes)
			}
			record.values[i] = entry
		}
		objects = append(objects, record)
	}
	return objects, root, nil
}

func restoreForeignGCGraph(target *Instance, objects []gcCloneObject, root gcCloneRef) (gc.Ref, uint32, error) {
	if root.kind != gcCloneRefObject || root.value == 0 || int(root.value) > len(objects) {
		return gc.Null(), 0, fmt.Errorf("foreign GC graph has an invalid root")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := target.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	target.refStore.mu.Lock()
	record := target.refStore.instances[target]
	target.refStore.mu.Unlock()
	if record == nil || record.resourcesReleased || target.gc == nil {
		return gc.Null(), 0, fmt.Errorf("target GC collector domain is closed")
	}

	refs := make(gc.RefSliceRoots, len(objects))
	rollback := func(cause error) (gc.Ref, uint32, error) {
		clear(refs)
		_ = target.gc.CollectFull(nil)
		return gc.Null(), 0, cause
	}
	for i, object := range objects {
		desc, ok := cloneGCDescriptor(target.c, object.typeID)
		if !ok || (desc.Kind != gc.KindStruct && desc.Kind != gc.KindArray) {
			return rollback(fmt.Errorf("target object %d type %d is unavailable", i+1, object.typeID))
		}
		domainType, ok := target.gcDomainType(uint32(object.typeID))
		if !ok {
			return rollback(fmt.Errorf("target object %d type %d has no canonical domain identity", i+1, object.typeID))
		}
		var ref gc.Ref
		var err error
		if desc.Kind == gc.KindStruct {
			ref, err = target.gc.NewStructUninitializedWithRoots(domainType, refs)
		} else {
			ref, err = target.gc.NewArrayUninitializedWithRoots(domainType, object.arrayLen, refs)
		}
		if err != nil {
			return rollback(fmt.Errorf("allocate target object %d: %w", i+1, err))
		}
		refs[i] = ref
	}
	decodeRef := func(encoded gcCloneRef) gc.Ref {
		switch encoded.kind {
		case gcCloneRefI31:
			return gc.Ref(encoded.value)
		case gcCloneRefObject:
			return refs[encoded.value-1]
		default:
			return gc.Null()
		}
	}
	for i, object := range objects {
		desc, _ := cloneGCDescriptor(target.c, object.typeID)
		for j, encoded := range object.values {
			value := gc.Value{Kind: encoded.kind, Bits: encoded.bits, BitsHi: encoded.bitsHi}
			if encoded.kind == gc.StorageRef || encoded.kind == gc.StorageRefNull {
				value.Ref = decodeRef(encoded.ref)
			}
			var err error
			if desc.Kind == gc.KindStruct {
				err = target.gc.StructSet(refs[i], uint32(j), value)
			} else {
				err = target.gc.ArraySet(refs[i], uint32(j), value)
			}
			if err != nil {
				return rollback(fmt.Errorf("populate target object %d value %d: %w", i+1, j, err))
			}
		}
	}
	result := refs[root.value-1]
	if state.cloneRootMade {
		if err := target.gc.SetGlobalSlot(state.cloneRootSlot, result); err != nil {
			return rollback(fmt.Errorf("root cloned GC graph: %w", err))
		}
	} else {
		slot, err := target.gc.NewCheckedClassifiedGlobalSlot(result, gc.RootForeignInstance)
		if err != nil {
			return rollback(fmt.Errorf("root cloned GC graph: %w", err))
		}
		state.cloneRootSlot = slot
		state.cloneRootMade = true
	}
	return result, uint32(objects[root.value-1].typeID), nil
}

func clearForeignCloneRoot(target *Instance, collect bool) {
	if target == nil || target.gc == nil {
		return
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := target.existingPublicGCState()
	if state == nil {
		return
	}
	state.mu.Lock()
	if state.cloneRootMade {
		_ = target.gc.SetGlobalSlot(state.cloneRootSlot, gc.Null())
	}
	if collect {
		_ = target.gc.CollectFull(nil)
	}
	state.mu.Unlock()
}
