//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func (in *Instance) managedFunctionAggregateMayAllocate(helper uint32, args []uint64) bool {
	if in == nil || in.c == nil {
		return false
	}
	switch helper {
	case gcStructSet:
		if len(args) >= 4 {
			typeID, field := uint32(args[len(args)-2]), uint32(args[len(args)-1])
			return int(typeID) < len(in.c.GCTypeDescs) && int(field) < len(in.c.GCTypeDescs[typeID].Fields) && (functionStorage(in.c.GCTypeDescs[typeID].Fields[field].Kind) || in.c.compactExternFrameRoots() && externAggregateStorage(in.c.GCTypeDescs[typeID].Fields[field].Kind))
		}
	case gcArraySet, gcArrayFill:
		if len(args) >= 4 {
			t := uint32(args[len(args)-1])
			return int(t) < len(in.c.GCTypeDescs) && (functionStorage(in.c.GCTypeDescs[t].Elem) || in.c.compactExternFrameRoots() && externAggregateStorage(in.c.GCTypeDescs[t].Elem))
		}
	case gcArrayInitElem:
		if len(args) == 6 {
			t := uint32(args[4])
			return int(t) < len(in.c.GCTypeDescs) && (functionStorage(in.c.GCTypeDescs[t].Elem) || in.c.compactExternFrameRoots() && externAggregateStorage(in.c.GCTypeDescs[t].Elem))
		}
	}
	return false
}

func (in *Instance) dispatchManagedFunctionAggregate(helper uint32, args, results []uint64, roots gc.RootSet, public *gcPublicState) bool {
	if in.c == nil || !in.c.usesManagedOpaqueStorage() {
		return false
	}
	var typeID uint32
	isStruct := helper == gcStructAllocOne || helper == gcStructSet
	switch helper {
	case gcStructAllocOne:
		if len(args) < 1 {
			return false
		}
		typeID = uint32(args[len(args)-1])
	case gcStructSet:
		if len(args) < 4 {
			return false
		}
		typeID = uint32(args[len(args)-2])
	case gcArrayAllocUniform, gcArrayAllocUniformNative, gcArrayAllocFixed, gcArrayAllocFixedNative, gcArraySet, gcArrayFill:
		if len(args) < 3 {
			return false
		}
		typeID = uint32(args[len(args)-1])
		if helper == gcArrayAllocFixed || helper == gcArrayAllocFixedNative {
			typeID = uint32(args[len(args)-2])
		}
	case gcArrayAllocElem:
		if len(args) != 4 {
			return false
		}
		typeID = uint32(args[2])
	case gcArrayInitElem:
		if len(args) != 6 {
			return false
		}
		typeID = uint32(args[4])
	case gcArrayCopy:
		if len(args) != 7 {
			return false
		}
		typeID = uint32(args[5])
	default:
		return false
	}
	if int(typeID) >= len(in.c.GCTypeDescs) || int(typeID) >= len(in.c.Types) {
		return false
	}
	desc := &in.c.GCTypeDescs[typeID]
	if isStruct {
		if desc.Kind != gc.KindStruct {
			return false
		}
		if helper == gcStructSet {
			field := uint32(args[len(args)-1])
			if int(field) >= len(desc.Fields) || !opaqueAggregateStorage(desc.Fields[field].Kind) {
				return false
			}
		} else {
			has := false
			for _, field := range desc.Fields {
				has = has || opaqueAggregateStorage(field.Kind)
			}
			if !has {
				return false
			}
		}
	} else if desc.Kind != gc.KindArray || !opaqueAggregateStorage(desc.Elem) {
		return false
	}

	if in.refStore == nil && in.c.compactExternFrameRoots() {
		if _, err := in.compactExternCollector(in.gcInvocationDomain()); err != nil {
			panic(gcHelperFailure(err))
		}
	}
	state := in.managedFunctionCollector(in.gcInvocationDomain())
	var object gc.Ref
	if helper == gcStructSet || helper == gcArraySet || helper == gcArrayFill || helper == gcArrayInitElem || helper == gcArrayCopy {
		object = gc.Ref(uint32(args[0]))
	}
	a := state.aggregateScratch(roots, object)
	defer a.clear()
	switch helper {
	case gcStructAllocOne:
		if len(results) == 0 || len(in.c.Types[typeID].Fields) != len(desc.Fields) {
			panic(gcHelperFailuref("invalid function struct initializer metadata"))
		}
		cursor := 0
		for i, field := range desc.Fields {
			slots := 1
			if field.Kind == gc.StorageV128 {
				slots = 2
			}
			if cursor+slots > len(args)-1 {
				panic(gcHelperFailuref("truncated function struct initializer"))
			}
			if opaqueAggregateStorage(field.Kind) {
				args[cursor] = in.managedOpaqueTypedValue(field.Kind, in.c.Types[typeID].Fields[i].Storage.Value, args[cursor]).Bits
			} else {
				_ = in.gcStructValueKnown(typeID, uint32(i), field.Kind, in.c.Types[typeID].Fields[i].Storage.Value, args[cursor:cursor+slots])
			}
			cursor += slots
		}
		if cursor != len(args)-1 {
			panic(gcHelperFailuref("invalid function struct initializer arity"))
		}
		cursor = 0
		for _, field := range desc.Fields {
			if opaqueAggregateStorage(field.Kind) {
				a.changeOpaque(state, in, field.Kind, args[cursor], 1, true)
			}
			cursor++
			if field.Kind == gc.StorageV128 {
				cursor++
			}
		}
		ref, err := in.gc.NewStructWordsPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), args[:len(args)-1], a, &public.initializerRoots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		a.publish(state, ref)
		results[0] = uint64(ref)
	case gcStructSet:
		field := uint32(args[len(args)-1])
		if len(args) != 4 {
			panic(gcHelperFailuref("invalid function struct setter arity"))
		}
		old, actual, matched, err := in.gc.StructGetTyped(object, in.requireGCDomainType(typeID), in.c.Types[typeID].Final, field)
		if err != nil {
			if object.IsNull() {
				panic(gcHelperTrap(coreruntime.TrapNullReference))
			}
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperFailuref("function struct type %d does not match %d", actual, typeID))
		}
		value := in.managedOpaqueTypedValue(desc.Fields[field].Kind, in.c.Types[typeID].Fields[field].Storage.Value, args[1])
		a.changeOpaque(state, in, desc.Fields[field].Kind, value.Bits, 1, true)
		a.changeOpaque(state, in, desc.Fields[field].Kind, old.Bits, -1, false)
		if err = in.gc.StructSet(object, field, value); err != nil {
			panic(gcHelperFailure(err))
		}
		a.publish(state, object)
	case gcArrayAllocUniform, gcArrayAllocUniformNative:
		if len(args) != 3 || len(results) == 0 {
			panic(gcHelperFailuref("invalid function array initializer arity"))
		}
		value, count := in.managedFunctionArrayValue(typeID, args[0]), uint32(args[1])
		a.changeOpaque(state, in, desc.Elem, value.Bits, int64(count), true)
		ref, err := in.gc.NewArrayWithRootScratch(in.requireGCDomainType(typeID), count, value, a, &public.arrayInitializerRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		a.publish(state, ref)
		results[0] = uint64(ref)
	case gcArrayAllocFixed, gcArrayAllocFixedNative:
		count := uint32(args[len(args)-1])
		if uint64(count)+2 != uint64(len(args)) || len(results) == 0 {
			panic(gcHelperFailuref("invalid function fixed array initializer arity"))
		}
		values, err := public.constructorValues(count)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		for i := range values {
			values[i] = in.managedFunctionArrayValue(typeID, args[i])
		}
		for _, value := range values {
			a.changeOpaque(state, in, desc.Elem, value.Bits, 1, true)
		}
		ref, err := in.gc.NewArrayFixedPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), values, a, &public.arrayInitializerRoots)
		if err != nil {
			in.gcPanicArrayError(err)
		}
		a.publish(state, ref)
		results[0] = uint64(ref)
	case gcArraySet:
		if len(args) != 4 {
			panic(gcHelperFailuref("invalid function array setter arity"))
		}
		in.gcCheckArray(object, typeID)
		old, err := in.gc.ArrayGet(object, uint32(args[1]))
		if err != nil {
			panic(gcHelperTrap(coreruntime.TrapBuiltin))
		}
		value := in.managedFunctionArrayValue(typeID, args[2])
		a.changeOpaque(state, in, desc.Elem, value.Bits, 1, true)
		a.changeOpaque(state, in, desc.Elem, old.Bits, -1, false)
		if err = in.gc.ArraySet(object, uint32(args[1]), value); err != nil {
			panic(gcHelperFailure(err))
		}
		a.publish(state, object)
	case gcArrayFill:
		if len(args) != 5 {
			panic(gcHelperFailuref("invalid function array fill arity"))
		}
		in.gcCheckArray(object, typeID)
		start, count := uint32(args[1]), uint32(args[3])
		length, err := in.gc.ArrayLen(object)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if uint64(start)+uint64(count) > uint64(length) {
			panic(gcHelperTrap(coreruntime.TrapBuiltin))
		}
		value := in.managedFunctionArrayValue(typeID, args[2])
		a.changeOpaque(state, in, desc.Elem, value.Bits, int64(count), true)
		for i := uint32(0); i < count; i++ {
			old, err := in.gc.ArrayGet(object, start+i)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			a.changeOpaque(state, in, desc.Elem, old.Bits, -1, false)
		}
		if err = in.gc.ArrayFill(object, start, value, count); err != nil {
			panic(gcHelperFailure(err))
		}
		a.publish(state, object)
	case gcArrayAllocElem, gcArrayInitElem:
		var source, count, segment, destination uint32
		if helper == gcArrayAllocElem {
			source, count, segment = uint32(args[0]), uint32(args[1]), uint32(args[3])
			if len(results) == 0 {
				panic(gcHelperFailuref("missing function array result"))
			}
		} else {
			destination, source, count, segment = uint32(args[1]), uint32(args[2]), uint32(args[3]), uint32(args[5])
			in.gcCheckArray(object, typeID)
			length, err := in.gc.ArrayLen(object)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			if uint64(destination)+uint64(count) > uint64(length) {
				panic(gcHelperTrap(coreruntime.TrapBuiltin))
			}
		}
		entries, length, width := in.gcArrayElemSegment(segment)
		if uint64(source)+uint64(count) > uint64(length) {
			panic(gcHelperTrap(coreruntime.TrapIndirectOutOfBounds))
		}
		for i := uint32(0); i < count; i++ {
			_ = in.managedFunctionArrayValue(typeID, in.gcArrayElemBits(entries, width, source+i))
		}
		for i := uint32(0); i < count; i++ {
			value := in.managedFunctionArrayValue(typeID, in.gcArrayElemBits(entries, width, source+i))
			a.changeOpaque(state, in, desc.Elem, value.Bits, 1, true)
		}
		if helper == gcArrayInitElem {
			for i := uint32(0); i < count; i++ {
				old, err := in.gc.ArrayGet(object, destination+i)
				if err != nil {
					panic(gcHelperFailure(err))
				}
				a.changeOpaque(state, in, desc.Elem, old.Bits, -1, false)
			}
		} else {
			ref, err := in.gc.NewArrayUninitializedWithRoots(in.requireGCDomainType(typeID), count, a)
			if err != nil {
				in.gcPanicArrayError(err)
			}
			object = ref
		}
		for i := uint32(0); i < count; i++ {
			if err := in.gc.ArraySet(object, destination+i, in.managedFunctionArrayValue(typeID, in.gcArrayElemBits(entries, width, source+i))); err != nil {
				panic(gcHelperFailure(err))
			}
		}
		a.publish(state, object)
		if helper == gcArrayAllocElem {
			results[0] = uint64(object)
		}
	case gcArrayCopy:
		in.gcCheckArray(object, typeID)
		src, dstStart, srcStart, count, srcType := gc.Ref(uint32(args[2])), uint32(args[1]), uint32(args[3]), uint32(args[4]), uint32(args[6])
		in.gcCheckArray(src, srcType)
		dstLen, err := in.gc.ArrayLen(object)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		srcLen, err := in.gc.ArrayLen(src)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if uint64(dstStart)+uint64(count) > uint64(dstLen) || uint64(srcStart)+uint64(count) > uint64(srcLen) {
			panic(gcHelperTrap(coreruntime.TrapBuiltin))
		}
		// Read both ranges before ArrayCopy mutates them, including overlapping
		// self-copies. Existing source edges prove ownership without allocation.
		for i := uint32(0); i < count; i++ {
			value, err := in.gc.ArrayGet(src, srcStart+i)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			a.changeOpaque(state, in, desc.Elem, value.Bits, 1, false)
			old, err := in.gc.ArrayGet(object, dstStart+i)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			a.changeOpaque(state, in, desc.Elem, old.Bits, -1, false)
		}
		if err = in.gc.ArrayCopy(object, dstStart, src, srcStart, count); err != nil {
			panic(gcHelperFailure(err))
		}
		a.publish(state, object)
	}
	return true
}

func (in *Instance) managedFunctionArrayValue(typeID uint32, bits uint64) gc.Value {
	return in.managedOpaqueTypedValue(in.c.GCTypeDescs[typeID].Elem, in.c.Types[typeID].Array.Storage.Value, bits)
}
func (in *Instance) managedFunctionTypedValue(kind gc.StorageKind, want ValueTypeDescriptor, bits uint64) gc.Value {
	if !functionStorage(kind) || want.Kind != ValueTypeReference {
		panic(gcHelperFailuref("invalid function aggregate descriptor"))
	}
	if bits == 0 {
		if kind == gc.StorageFuncRef || !want.Ref.Nullable {
			panic(gcHelperFailuref("function aggregate rejects null"))
		}
		return gc.Value{Kind: kind}
	}
	actual, types, ok := in.attachedFuncrefExactType(bits)
	if !ok && in.refStore != nil {
		actual, types, ok = in.refStore.descriptorFuncrefExactType(in, bits)
	}
	// An opaque field can move between instances without ever becoming a public
	// token. The physical directory proof also used by Env creation authenticates
	// that exact descriptor before its producer's type metadata is read.
	if !ok {
		actual, types, ok = instanceFuncrefExactType(managedFunctionDescriptorOwner(in, bits), bits)
	}
	if !ok || !valueTypeSubtype(actual, types, want, in.c.Types) {
		panic(gcHelperFailuref("function aggregate value has incompatible exact type"))
	}
	return gc.Value{Kind: kind, Bits: bits}
}
