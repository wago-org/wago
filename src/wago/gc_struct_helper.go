// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"errors"
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Internal GC helper dispatch occupies bit 30. Public host-funcref dispatch uses
// bit 31, and ordinary Wasm import indexes use neither. The amd64 backend mirrors
// these compile-only constants.
const (
	gcStructDispatchBit       uint32 = 1 << 30
	gcStructAllocDefault             = 1
	gcStructGet                      = 2
	gcStructSet                      = 3
	gcStructGetS                     = 4
	gcStructGetU                     = 5
	gcStructRefTest                  = 6
	gcStructTableSet                 = 7
	gcAnyConvertExtern               = 8
	gcExternConvertAny               = 9
	gcStructRefCast                  = 10
	gcStructAllocOne                 = 11
	gcStructFinalCastGet             = 12
	gcStructFinalCastArrayLen        = 13
	gcStructReserveDead              = 15
	gcStructReserveDeadNative        = 40 // dead reservation followed by native batch preparation
	gcStructReserveDeadBatch         = 42
)

func (in *Instance) gcObjectTypeMatches(actual gc.TypeID, want uint32) bool {
	if in == nil || in.gc == nil {
		return false
	}
	required, ok := in.gcDomainType(want)
	if !ok {
		return false
	}
	if int(want) < len(in.c.Types) && in.c.Types[want].Final {
		return actual == required
	}
	matched, err := in.gc.TypeSubtype(actual, required)
	return err == nil && matched
}

func gcHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne, gcStructReserveDead, gcStructReserveDeadNative, gcStructReserveDeadBatch, gcArrayReserveDeadBatch,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill,
		gcArrayAllocDefaultNative, gcArrayAllocUniformNative, gcArrayAllocFixedNative,
		gcArrayCheckDefault, gcArrayCheckUniform, gcArrayCheckData, gcArrayCheckFixed:
		return true
	default:
		return false
	}
}

func (in *Instance) dispatchGCStructHelperLocked(helper uint32, args, results []uint64, state *gcPublicState, frameRoots gc.RootSet) {
	switch {
	case gcHelperMaskForImage&(1<<gcStructReserveDeadBatch) != 0 && helper == gcStructReserveDeadBatch:
		if len(args) != 2 || len(results) != 0 {
			panic(gcHelperFailuref("gc struct batch reservation arity = %d/%d, want 2/0", len(args), len(results)))
		}
		if err := in.gc.ReserveDeadStructAllocationsWithPoll(in.requireGCDomainType(uint32(args[1])), uint32(args[0]), frameRoots, in.pollGCBatchInterrupt); err != nil {
			panic(gcHelperFailure(err))
		}
		if gc.NativeDeadBatchSupported {
			in.prepareNativeDeadStructBatch(uint32(args[1]), uint32(args[0]))
		}
	case gcHelperMaskForImage&(1<<gcStructReserveDead) != 0 && helper == gcStructReserveDead,
		gcHelperMaskForImage&(1<<gcStructReserveDeadNative) != 0 && helper == gcStructReserveDeadNative:
		if len(args) != 1 {
			panic(gcHelperFailuref("gc struct dead reservation helper arity = %d, want 1", len(args)))
		}
		ref, err := in.gc.ReserveDeadStructAllocation(in.requireGCDomainType(uint32(args[0])), frameRoots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if len(results) != 0 {
			results[0] = uint64(ref)
		}
		if gcHelperMaskForImage&(1<<gcStructReserveDeadNative) != 0 && helper == gcStructReserveDeadNative {
			in.prepareNativeDeadStructHandle(uint32(args[0]))
		}
	case gcHelperMaskForImage&(1<<gcStructAllocDefault) != 0 && helper == gcStructAllocDefault:
		if len(args) != 1 || len(results) < 1 {
			panic(gcHelperFailuref("gc struct alloc helper arity = %d/%d, want 1/at-least-1", len(args), len(results)))
		}
		// Exact local products have no live frame ref across allocation. The
		// ref.test table product may retain prior objects only in checked collector
		// table slots, and stores each returned ref before the next allocation.
		// A non-nil empty frame-root set keeps stress collection explicit.
		ref, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(uint32(args[0])), frameRoots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(ref)
	case gcHelperMaskForImage&(1<<gcStructAllocOne) != 0 && helper == gcStructAllocOne:
		if len(args) < 1 || len(results) < 1 {
			panic(gcHelperFailuref("gc struct alloc helper arity = %d/%d, want at-least-1/at-least-1", len(args), len(results)))
		}
		typeID := uint32(args[len(args)-1])
		if int(typeID) >= len(in.c.GCTypeDescs) || in.c.GCTypeDescs[typeID].Kind != gc.KindStruct {
			panic(gcHelperFailuref("gc struct type %d is unavailable", typeID))
		}
		descFields := in.c.GCTypeDescs[typeID].Fields
		typeFields := in.c.Types[typeID].Fields
		cursor := 0
		for i := range descFields {
			kind := descFields[i].Kind
			slots := 1
			if kind == gc.StorageV128 {
				slots = 2
			}
			if cursor+slots > len(args)-1 {
				panic(gcHelperFailuref("gc struct type %d initializer slots end at %d, args = %d", typeID, cursor+slots, len(args)))
			}
			words := args[cursor : cursor+slots]
			switch kind {
			case gc.StorageRef, gc.StorageRefNull,
				gc.StorageFuncRef, gc.StorageFuncRefNull,
				gc.StorageExternRef, gc.StorageExternRefNull:
				_ = in.gcStructValueKnown(typeID, uint32(i), kind, typeFields[i].Storage.Value, words)
			}
			cursor += slots
		}
		if cursor != len(args)-1 {
			panic(gcHelperFailuref("gc struct type %d initializer uses %d slots, args = %d", typeID, cursor, len(args)))
		}
		ref, err := in.gc.NewStructWordsPrevalidatedWithRootScratch(in.requireGCDomainType(typeID), args[:len(args)-1], frameRoots, state.structInitializerScratch())
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = uint64(ref)
		in.prepareNativeStructHandles(typeID)
	case gcHelperMaskForImage&(1<<gcStructGet) != 0 && helper == gcStructGet,
		gcHelperMaskForImage&(1<<gcStructGetS) != 0 && helper == gcStructGetS,
		gcHelperMaskForImage&(1<<gcStructGetU) != 0 && helper == gcStructGetU:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc struct get helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcHelperTrap(coreruntime.TrapNullReference))
		}
		want := uint32(args[1])
		required, ok := in.gcDomainType(want)
		if !ok {
			panic(gcHelperFailuref("gc struct get type %d has no Runtime-domain identity", want))
		}
		exact := int(want) < len(in.c.Types) && in.c.Types[want].Final
		value, actual, matched, err := in.gc.StructGetTyped(ref, required, exact, uint32(args[2]))
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperFailuref("gc struct get type = %d, want subtype of %d", actual, want))
		}
		if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
			results[0] = uint64(value.Ref)
			break
		}
		switch {
		case gcHelperMaskForImage&(1<<gcStructGetS) != 0 && helper == gcStructGetS:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			case gc.StorageI16:
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			default:
				panic(gcHelperFailuref("gc struct.get_s field kind %d is not packed", value.Kind))
			}
		case gcHelperMaskForImage&(1<<gcStructGetU) != 0 && helper == gcStructGetU:
			switch value.Kind {
			case gc.StorageI8:
				results[0] = uint64(uint32(uint8(value.Bits)))
			case gc.StorageI16:
				results[0] = uint64(uint32(uint16(value.Bits)))
			default:
				panic(gcHelperFailuref("gc struct.get_u field kind %d is not packed", value.Kind))
			}
		default:
			results[0] = value.Bits
			if value.Kind == gc.StorageV128 {
				if len(results) < 2 {
					panic(gcHelperFailuref("gc struct.get v128 result arity = %d, want at least 2", len(results)))
				}
				results[1] = value.BitsHi
			}
		}
	case gcHelperMaskForImage&(1<<gcStructRefTest) != 0 && helper == gcStructRefTest:
		if len(args) != 4 || len(results) < 1 {
			panic(gcHelperFailuref("gc ref.test helper arity = %d/%d, want 4/at-least-1", len(args), len(results)))
		}
		if matched, ok := gcNonAnyHierarchyRefTest(args[0], int64(args[1]), args[2] != 0); ok {
			results[0] = 0
			if matched {
				results[0] = 1
			}
			break
		}
		target, err := in.gcDynamicRefTarget(int64(args[1]), args[2] != 0, args[3] != 0)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		var matched bool
		if state := in.existingGCRefTestTableState(); state != nil {
			matched, err = state.refTest(in.gc, args[0], target)
		} else {
			matched, err = (*gcRefTestTableState)(nil).refTest(in.gc, args[0], target)
		}
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if matched {
			results[0] = 1
		} else {
			results[0] = 0
		}
	case gcHelperMaskForImage&(1<<gcStructRefCast) != 0 && helper == gcStructRefCast:
		if len(args) != 4 || len(results) < 1 {
			panic(gcHelperFailuref("gc ref.cast helper arity = %d/%d, want 4/at-least-1", len(args), len(results)))
		}
		exact := args[3] != 0
		if matched, ok := gcNonAnyHierarchyRefTest(args[0], int64(args[1]), args[2] != 0); ok {
			if !matched {
				panic(gcHelperTrap(coreruntime.TrapCastFailure))
			}
			results[0] = args[0]
			break
		}
		if value, handled, err := in.gcDefinedRefCast(args[0], int64(args[1]), args[2] != 0, exact); handled {
			if errors.Is(err, gc.ErrCastFailure) {
				panic(gcHelperTrap(coreruntime.TrapCastFailure))
			}
			if err != nil {
				panic(gcHelperFailure(err))
			}
			results[0] = value
			break
		}
		target, err := in.gcDynamicRefTarget(int64(args[1]), args[2] != 0, exact)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		var value uint64
		if state := in.existingGCRefTestTableState(); state != nil {
			value, err = state.refCast(in.gc, args[0], target)
		} else {
			value, err = (*gcRefTestTableState)(nil).refCast(in.gc, args[0], target)
		}
		if errors.Is(err, gc.ErrCastFailure) {
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		if err != nil {
			panic(gcHelperFailure(err))
		}
		results[0] = value
	case gcHelperMaskForImage&(1<<gcStructFinalCastGet) != 0 && helper == gcStructFinalCastGet:
		if len(args) != 5 || len(results) < 1 {
			panic(gcHelperFailuref("gc final cast/get helper arity = %d/%d, want 5/at-least-1", len(args), len(results)))
		}
		bits, typeID, fieldID, nullable, mode := args[0], uint32(args[1]), uint32(args[2]), args[3] != 0, uint32(args[4])
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			if in.isForeignAnyref(bits) {
				panic(gcHelperTrap(coreruntime.TrapCastFailure))
			}
			panic(gcHelperFailuref("gc final cast/get contains non-compact reference %#x", bits))
		}
		if ref.IsNull() {
			if nullable {
				panic(gcHelperTrap(coreruntime.TrapNullReference))
			}
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		if ref.IsI31() {
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		if int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) || !in.c.Types[typeID].Final || in.c.Types[typeID].Kind != CompositeTypeStruct || int(fieldID) >= len(in.c.Types[typeID].Fields) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcHelperFailuref("gc final cast/get field %d:%d is unavailable", typeID, fieldID))
		}
		kind := in.c.GCTypeDescs[typeID].Fields[fieldID].Kind
		if mode > 2 || (mode == 0 && (kind == gc.StorageI8 || kind == gc.StorageI16)) || (mode != 0 && kind != gc.StorageI8 && kind != gc.StorageI16) {
			panic(gcHelperFailuref("gc final cast/get field %d:%d kind %d rejects mode %d", typeID, fieldID, kind, mode))
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcHelperFailuref("gc final cast/get type %d has no Runtime-domain identity", typeID))
		}
		if mode == 0 && (kind == gc.StorageRef || kind == gc.StorageRefNull) {
			value, matched, err := in.gc.StructGetFinalRef(ref, required, fieldID)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			if !matched {
				panic(gcHelperTrap(coreruntime.TrapCastFailure))
			}
			results[0] = uint64(value)
			break
		}
		value, _, matched, err := in.gc.StructGetTyped(ref, required, true, fieldID)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		switch mode {
		case 1:
			if value.Kind == gc.StorageI8 {
				results[0] = uint64(uint32(int32(int8(value.Bits))))
			} else {
				results[0] = uint64(uint32(int32(int16(value.Bits))))
			}
		case 2:
			if value.Kind == gc.StorageI8 {
				results[0] = uint64(uint32(uint8(value.Bits)))
			} else {
				results[0] = uint64(uint32(uint16(value.Bits)))
			}
		default:
			if value.Kind == gc.StorageRef || value.Kind == gc.StorageRefNull {
				results[0] = uint64(value.Ref)
			} else {
				results[0] = value.Bits
				if value.Kind == gc.StorageV128 {
					if len(results) < 2 {
						panic(gcHelperFailuref("gc final cast/get v128 result arity = %d, want at least 2", len(results)))
					}
					results[1] = value.BitsHi
				}
			}
		}
	case gcHelperMaskForImage&(1<<gcStructFinalCastArrayLen) != 0 && helper == gcStructFinalCastArrayLen:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc final cast/array.len helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		bits, typeID, nullable := args[0], uint32(args[1]), args[2] != 0
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			if in.isForeignAnyref(bits) {
				panic(gcHelperTrap(coreruntime.TrapCastFailure))
			}
			panic(gcHelperFailuref("gc final cast/array.len contains non-compact reference %#x", bits))
		}
		if ref.IsNull() {
			if nullable {
				panic(gcHelperTrap(coreruntime.TrapNullReference))
			}
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		if ref.IsI31() {
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		if int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) || !in.c.Types[typeID].Final || in.c.Types[typeID].Kind != CompositeTypeArray || in.c.GCTypeDescs[typeID].Kind != gc.KindArray {
			panic(gcHelperFailuref("gc final cast/array.len type %d is unavailable", typeID))
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcHelperFailuref("gc final cast/array.len type %d has no Runtime-domain identity", typeID))
		}
		length, _, matched, err := in.gc.ArrayLenTyped(ref, required, true)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperTrap(coreruntime.TrapCastFailure))
		}
		results[0] = uint64(length)
	case gcHelperMaskForImage&(1<<gcStructSet) != 0 && helper == gcStructSet:
		if len(args) < 4 {
			panic(gcHelperFailuref("gc struct set helper arity = %d, want at least 4", len(args)))
		}
		ref := gc.Ref(uint32(args[0]))
		if ref.IsNull() {
			panic(gcHelperTrap(coreruntime.TrapNullReference))
		}
		typeID := uint32(args[len(args)-2])
		fieldID := uint32(args[len(args)-1])
		valueSlots := in.gcStructValueSlots(typeID, fieldID)
		if len(args) != valueSlots+3 {
			panic(gcHelperFailuref("gc struct set helper arity = %d, want %d", len(args), valueSlots+3))
		}
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			panic(gcHelperFailuref("gc struct set field %d:%d is unavailable", typeID, fieldID))
		}
		required, ok := in.gcDomainType(typeID)
		if !ok {
			panic(gcHelperFailuref("gc struct set type %d has no Runtime-domain identity", typeID))
		}
		exact := int(typeID) < len(in.c.Types) && in.c.Types[typeID].Final
		actual, matched, err := in.gc.StructSetTyped(ref, required, exact, fieldID, in.gcStructValue(typeID, fieldID, args[1:1+valueSlots]))
		if err != nil {
			panic(gcHelperFailure(err))
		}
		if !matched {
			panic(gcHelperFailuref("gc struct set type = %d, want subtype of %d", actual, typeID))
		}
	default:
		panic(gcHelperFailuref("unknown gc struct helper %d", helper))
	}
}

// isForeignAnyref reports whether bits is the anyref word of a host externref
// (any.convert_extern), which no defined struct/array type admits.
func (in *Instance) isForeignAnyref(bits uint64) bool {
	if !gcExternConversionForImage {
		return false
	}
	conversion := in.existingGCExternConversionState()
	if conversion == nil {
		return false
	}
	foreign, err := conversion.isForeignAny(bits)
	return err == nil && foreign
}

// gcDefinedRefCast avoids descriptor/subtype walks when equality is the complete
// decision: either the encoded target is exact or the declared target is final.
func (in *Instance) gcDefinedRefCast(bits uint64, heap int64, nullable, exact bool) (uint64, bool, error) {
	if in == nil || in.gc == nil || in.existingGCRefTestTableState() != nil || heap < 0 || uint64(heap) >= uint64(len(in.c.Types)) {
		return 0, false, nil
	}
	type_ := in.c.Types[heap]
	if (!exact && !type_.Final) || (type_.Kind != CompositeTypeStruct && type_.Kind != CompositeTypeArray) {
		return 0, false, nil
	}
	ref := gc.Ref(uint32(bits))
	if bits != uint64(ref) {
		return 0, true, fmt.Errorf("gc ref.cast contains non-compact reference %#x", bits)
	}
	if ref.IsNull() {
		if nullable {
			return 0, true, nil
		}
		return 0, true, gc.ErrCastFailure
	}
	if ref.IsI31() {
		return 0, true, gc.ErrCastFailure
	}
	actual, err := in.gc.ObjectType(ref)
	if err != nil {
		return 0, true, err
	}
	required, ok := in.gcDomainType(uint32(heap))
	if !ok {
		return 0, true, fmt.Errorf("gc dynamic reference heap type %d has no Runtime-domain identity", heap)
	}
	if actual != required {
		return 0, true, gc.ErrCastFailure
	}
	return bits, true, nil
}

// gcNonAnyHierarchyRefTest answers ref.test/ref.cast against the abstract
// func, extern, and exn hierarchies, which have no collector identity.
// Validation keeps the operand inside the target's hierarchy, so the top type
// admits every non-null reference and the bottom type admits only null.
func gcNonAnyHierarchyRefTest(bits uint64, heap int64, nullable bool) (matched, ok bool) {
	switch heap {
	case -16, -17, -23: // func, extern, exn
		return bits != 0 || nullable, true
	case -13, -14, -12: // nofunc, noextern, noexn
		return bits == 0 && nullable, true
	}
	return false, false
}

func (in *Instance) gcDynamicRefTarget(heap int64, nullable, exact bool) (gc.RefTestTarget, error) {
	target := gc.RefTestTarget{Nullable: nullable, Exact: exact}
	switch heap {
	case -15:
		target.Kind = gc.RefTestNone
	case -18:
		target.Kind = gc.RefTestAny
	case -19:
		target.Kind = gc.RefTestEq
	case -20:
		target.Kind = gc.RefTestI31
	case -21:
		target.Kind = gc.RefTestStruct
	case -22:
		target.Kind = gc.RefTestArray
	default:
		if heap < 0 || uint64(heap) > uint64(^uint32(0)) {
			return gc.RefTestTarget{}, fmt.Errorf("gc dynamic reference heap type %d is unavailable", heap)
		}
		domain, ok := in.gcDomainType(uint32(heap))
		if !ok {
			return gc.RefTestTarget{}, fmt.Errorf("gc dynamic reference heap type %d has no Runtime-domain identity", heap)
		}
		target.Kind, target.Type = gc.RefTestDefined, domain
	}
	if exact && target.Kind != gc.RefTestDefined {
		return gc.RefTestTarget{}, fmt.Errorf("gc dynamic exact reference heap type %d is not defined", heap)
	}
	return target, nil
}

//go:noinline
func (in *Instance) gcStructFieldKind(typeID, fieldID uint32) gc.StorageKind {
	if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) || int(typeID) >= len(in.c.Types) || in.c.Types[typeID].Kind != CompositeTypeStruct || int(fieldID) >= len(in.c.Types[typeID].Fields) {
		panic(gcHelperFailuref("gc struct field %d:%d is unavailable", typeID, fieldID))
	}
	return in.c.GCTypeDescs[typeID].Fields[fieldID].Kind
}

//go:noinline
func (in *Instance) gcStructValueSlots(typeID, fieldID uint32) int {
	if in.gcStructFieldKind(typeID, fieldID) == gc.StorageV128 {
		return 2
	}
	return 1
}

//go:noinline
func (in *Instance) gcStructValueKnown(typeID, fieldID uint32, kind gc.StorageKind, want ValueTypeDescriptor, words []uint64) gc.Value {
	wantSlots := 1
	if kind == gc.StorageV128 {
		wantSlots = 2
	}
	if len(words) != wantSlots {
		panic(gcHelperFailuref("gc struct field %d:%d value uses %d slot(s), want %d", typeID, fieldID, len(words), wantSlots))
	}
	bits := words[0]
	if kind == gc.StorageFuncRef || kind == gc.StorageFuncRefNull {
		if bits == 0 {
			if kind == gc.StorageFuncRef {
				panic(gcHelperFailuref("gc struct field %d:%d rejects null funcref", typeID, fieldID))
			}
			return gc.Value{Kind: kind}
		}
		actual, actualTypes, ok := in.attachedFuncrefExactType(bits)
		if !ok && in.refStore != nil {
			actual, actualTypes, ok = in.refStore.descriptorFuncrefExactType(in, bits)
		}
		if !ok || !valueTypeSubtype(actual, actualTypes, want, in.c.Types) {
			panic(gcHelperFailuref("gc struct funcref type does not match field %d:%d", typeID, fieldID))
		}
		return gc.Value{Kind: kind, Bits: bits}
	}
	if kind == gc.StorageExternRef || kind == gc.StorageExternRefNull {
		if bits == 0 && kind == gc.StorageExternRef {
			panic(gcHelperFailuref("gc struct field %d:%d rejects null externref", typeID, fieldID))
		}
		return gc.Value{Kind: kind, Bits: bits}
	}
	if kind != gc.StorageRef && kind != gc.StorageRefNull {
		valueKind := kind
		if kind == gc.StorageI8 || kind == gc.StorageI16 {
			valueKind = gc.StorageI32
		}
		value := gc.Value{Kind: valueKind, Bits: bits}
		if kind == gc.StorageV128 {
			value.BitsHi = words[1]
		}
		return value
	}
	ref := gc.Ref(uint32(bits))
	if ref.IsNull() {
		if !want.Ref.Nullable {
			panic(gcHelperFailuref("gc struct field %d:%d rejects null", typeID, fieldID))
		}
		return gc.RefValue(ref)
	}
	if !in.gcRefMatchesValueType(ref, want) {
		panic(gcHelperFailuref("gc struct reference type does not match field %d:%d", typeID, fieldID))
	}
	return gc.RefValue(ref)
}

//go:noinline
func (in *Instance) gcStructValue(typeID, fieldID uint32, words []uint64) gc.Value {
	kind := in.gcStructFieldKind(typeID, fieldID)
	return in.gcStructValueKnown(typeID, fieldID, kind, in.c.Types[typeID].Fields[fieldID].Storage.Value, words)
}

// Reduced loops remain in Go across collecting boundaries, so native interrupt
// delivery must be observed here before charging the next allocation batch.
func (in *Instance) pollGCBatchInterrupt() {
	if coreruntime.PreparedIntTrapCode(in.trap) == coreruntime.TrapInterrupted {
		panic(gcHelperTrap(coreruntime.TrapInterrupted))
	}
}
