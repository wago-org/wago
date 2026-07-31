package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

// gcSnapshotRef is a collector-independent reference. Object indexes are
// one-based so the zero value is the canonical null reference.
type gcSnapshotRef struct {
	kind  uint8
	value uint32
}

const (
	gcSnapshotRefNull uint8 = iota
	gcSnapshotRefI31
	gcSnapshotRefObject
)

type gcSnapshotValue struct {
	kind   gc.StorageKind
	bits   uint64
	bitsHi uint64
	ref    gcSnapshotRef
}

type gcObjectSnapshot struct {
	typeID   gc.TypeID
	arrayLen uint32
	values   []gcSnapshotValue
}

func snapshotGCDescriptor(c *Compiled, id gc.TypeID) (gc.TypeDesc, bool) {
	if c == nil || uint64(id) >= uint64(len(c.GCTypeDescs)) {
		return gc.TypeDesc{}, false
	}
	d := c.GCTypeDescs[id]
	return d, d.ID == id
}

func captureGCHeapSnapshot(in *Instance, s *Snapshot) error {
	if in == nil || in.c == nil || in.gc == nil || s == nil {
		return nil
	}
	s.gcGlobalRefs = make([]gcSnapshotRef, len(in.globalCells))
	ids := make(map[gc.Ref]uint32)
	queue := make([]gc.Ref, 0, 16)
	encodeRef := func(ref gc.Ref) (gcSnapshotRef, error) {
		if ref.IsNull() {
			return gcSnapshotRef{}, nil
		}
		if ref.IsI31() {
			return gcSnapshotRef{kind: gcSnapshotRefI31, value: uint32(ref)}, nil
		}
		if id, ok := ids[ref]; ok {
			return gcSnapshotRef{kind: gcSnapshotRefObject, value: id}, nil
		}
		if _, err := in.gc.ObjectType(ref); err != nil {
			return gcSnapshotRef{}, fmt.Errorf("reference %#x: %w", uint32(ref), err)
		}
		id := uint32(len(queue) + 1)
		ids[ref] = id
		queue = append(queue, ref)
		return gcSnapshotRef{kind: gcSnapshotRefObject, value: id}, nil
	}

	for i, cell := range in.globalCells {
		if cell == nil || i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) {
			continue
		}
		bits := readGlobalObject(cell, in.c.Globals[i].Type)
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return fmt.Errorf("global %d contains non-compact GC reference %#x", i, bits)
		}
		encoded, err := encodeRef(ref)
		if err != nil {
			return fmt.Errorf("global %d: %w", i, err)
		}
		s.gcGlobalRefs[i] = encoded
		// Never retain a compact collector handle in the general global record.
		s.globals[i].bits = 0
	}

	for pos := 0; pos < len(queue); pos++ {
		ref := queue[pos]
		typeID, err := in.gc.ObjectType(ref)
		if err != nil {
			return fmt.Errorf("object %d type: %w", pos+1, err)
		}
		desc, ok := snapshotGCDescriptor(in.c, typeID)
		if !ok || (desc.Kind != gc.KindStruct && desc.Kind != gc.KindArray) {
			return fmt.Errorf("object %d has unavailable type %d", pos+1, typeID)
		}
		record := gcObjectSnapshot{typeID: typeID}
		var count uint32
		if desc.Kind == gc.KindStruct {
			count = uint32(len(desc.Fields))
		} else {
			count, err = in.gc.ArrayLen(ref)
			if err != nil {
				return fmt.Errorf("object %d array length: %w", pos+1, err)
			}
			record.arrayLen = count
		}
		record.values = make([]gcSnapshotValue, count)
		for i := uint32(0); i < count; i++ {
			var value gc.Value
			if desc.Kind == gc.KindStruct {
				value, err = in.gc.StructGet(ref, i)
			} else {
				value, err = in.gc.ArrayGet(ref, i)
			}
			if err != nil {
				return fmt.Errorf("object %d value %d: %w", pos+1, i, err)
			}
			entry := gcSnapshotValue{kind: value.Kind, bits: value.Bits, bitsHi: value.BitsHi}
			switch value.Kind {
			case gc.StorageRef, gc.StorageRefNull:
				entry.ref, err = encodeRef(value.Ref)
				if err != nil {
					return fmt.Errorf("object %d value %d: %w", pos+1, i, err)
				}
				entry.bits = 0
			case gc.StorageFuncRef, gc.StorageFuncRefNull, gc.StorageExternRef, gc.StorageExternRefNull:
				if value.Bits != 0 {
					return fmt.Errorf("object %d value %d contains non-null non-collector reference storage", pos+1, i)
				}
			}
			record.values[i] = entry
		}
		s.gcObjects = append(s.gcObjects, record)
	}
	return nil
}

func validateGCSnapshot(c *Compiled, globals []globalSnap, roots []gcSnapshotRef, objects []gcObjectSnapshot) error {
	if c == nil {
		return fmt.Errorf("snapshot has no bound module")
	}
	if len(roots) != len(globals) {
		return fmt.Errorf("GC global root count %d does not match global count %d", len(roots), len(globals))
	}
	validateRef := func(ref gcSnapshotRef) error {
		switch ref.kind {
		case gcSnapshotRefNull:
			if ref.value != 0 {
				return fmt.Errorf("null reference has payload %d", ref.value)
			}
		case gcSnapshotRefI31:
			if !gc.Ref(ref.value).IsI31() {
				return fmt.Errorf("i31 reference payload %#x is invalid", ref.value)
			}
		case gcSnapshotRefObject:
			if ref.value == 0 || uint64(ref.value) > uint64(len(objects)) {
				return fmt.Errorf("object reference %d exceeds object count %d", ref.value, len(objects))
			}
		default:
			return fmt.Errorf("unknown GC reference kind %d", ref.kind)
		}
		return nil
	}
	for i, ref := range roots {
		if err := validateRef(ref); err != nil {
			return fmt.Errorf("global %d: %w", i, err)
		}
		if i >= len(c.Globals) || !isGCRefValType(c.Globals[i].Type) {
			if ref.kind != gcSnapshotRefNull {
				return fmt.Errorf("non-GC global %d carries a GC root", i)
			}
		}
	}
	for i, object := range objects {
		desc, ok := snapshotGCDescriptor(c, object.typeID)
		if !ok || (desc.Kind != gc.KindStruct && desc.Kind != gc.KindArray) {
			return fmt.Errorf("object %d has unavailable type %d", i+1, object.typeID)
		}
		want := len(desc.Fields)
		if desc.Kind == gc.KindArray {
			want = int(object.arrayLen)
		}
		if len(object.values) != want {
			return fmt.Errorf("object %d value count %d does not match layout count %d", i+1, len(object.values), want)
		}
		for j, value := range object.values {
			wantKind := desc.Elem
			if desc.Kind == gc.KindStruct {
				wantKind = desc.Fields[j].Kind
			}
			if value.kind != wantKind {
				return fmt.Errorf("object %d value %d kind %d does not match layout kind %d", i+1, j, value.kind, wantKind)
			}
			if value.kind == gc.StorageRef || value.kind == gc.StorageRefNull {
				if err := validateRef(value.ref); err != nil {
					return fmt.Errorf("object %d value %d: %w", i+1, j, err)
				}
			} else if value.ref.kind != 0 || value.ref.value != 0 {
				return fmt.Errorf("object %d value %d has a reference payload for non-reference storage", i+1, j)
			}
			if (value.kind == gc.StorageFuncRef || value.kind == gc.StorageFuncRefNull || value.kind == gc.StorageExternRef || value.kind == gc.StorageExternRefNull) && value.bits != 0 {
				return fmt.Errorf("object %d value %d contains a non-null non-collector reference", i+1, j)
			}
		}
	}
	return nil
}

func restoreGCHeapSnapshot(in *Instance, s *Snapshot) error {
	if in == nil || in.gc == nil || s == nil || len(s.gcGlobalRefs) == 0 {
		return nil
	}
	if err := validateGCSnapshot(in.c, s.globals, s.gcGlobalRefs, s.gcObjects); err != nil {
		return err
	}
	public := in.publicGCState()
	// Drop freshly replayed initializer roots before allocating the persisted
	// graph. They are not part of the snapshot and otherwise can pin a duplicate
	// graph long enough to exhaust a Tiny heap during two-pass reconstruction.
	for i := range s.gcGlobalRefs {
		if i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) || i >= len(in.globalCells) || in.globalCells[i] == nil {
			continue
		}
		writeGlobalObject(in.globalCells[i], in.c.Globals[i].Type, 0)
		found := false
		for _, mapping := range public.globalRoots {
			if mapping.GlobalIndex == uint32(i) {
				if err := in.gc.SetGlobalSlot(mapping.SlotIndex, gc.Null()); err != nil {
					return fmt.Errorf("clear GC global %d root: %w", i, err)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("GC global %d has no checked collector root", i)
		}
	}

	refs := make(gc.RefSliceRoots, len(s.gcObjects))
	for i, object := range s.gcObjects {
		desc, _ := snapshotGCDescriptor(in.c, object.typeID)
		var ref gc.Ref
		var err error
		if desc.Kind == gc.KindStruct {
			ref, err = in.gc.NewStructDefaultWithRoots(object.typeID, refs)
		} else {
			ref, err = in.gc.NewArrayDefaultWithRoots(object.typeID, object.arrayLen, refs)
		}
		if err != nil {
			return fmt.Errorf("allocate object %d: %w", i+1, err)
		}
		refs[i] = ref
	}
	decodeRef := func(encoded gcSnapshotRef) gc.Ref {
		switch encoded.kind {
		case gcSnapshotRefI31:
			return gc.Ref(encoded.value)
		case gcSnapshotRefObject:
			return refs[encoded.value-1]
		default:
			return gc.Null()
		}
	}
	for i, object := range s.gcObjects {
		desc, _ := snapshotGCDescriptor(in.c, object.typeID)
		for j, encoded := range object.values {
			value := gc.Value{Kind: encoded.kind, Bits: encoded.bits, BitsHi: encoded.bitsHi}
			if encoded.kind == gc.StorageRef || encoded.kind == gc.StorageRefNull {
				value.Ref = decodeRef(encoded.ref)
			}
			var err error
			if desc.Kind == gc.KindStruct {
				err = in.gc.StructSet(refs[i], uint32(j), value)
			} else {
				err = in.gc.ArraySet(refs[i], uint32(j), value)
			}
			if err != nil {
				return fmt.Errorf("restore object %d value %d: %w", i+1, j, err)
			}
		}
	}

	for i, encoded := range s.gcGlobalRefs {
		if i >= len(in.c.Globals) || !isGCRefValType(in.c.Globals[i].Type) {
			continue
		}
		if i >= len(in.globalCells) || in.globalCells[i] == nil {
			return fmt.Errorf("GC global %d is unavailable", i)
		}
		ref := decodeRef(encoded)
		writeGlobalObject(in.globalCells[i], in.c.Globals[i].Type, uint64(ref))
		found := false
		for _, mapping := range public.globalRoots {
			if mapping.GlobalIndex != uint32(i) {
				continue
			}
			if err := in.gc.SetGlobalSlot(mapping.SlotIndex, ref); err != nil {
				return fmt.Errorf("GC global %d root: %w", i, err)
			}
			found = true
			break
		}
		if !found {
			return fmt.Errorf("GC global %d has no checked collector root", i)
		}
	}
	return nil
}
