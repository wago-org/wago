// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_typed_function_references || !wago_no_gc || !wago_no_exception_handling)

package wago

import "fmt"

func railshotUsesGuestGlobalStoreHelper(c *Compiled) bool {
	return rv64MutableFuncrefGlobalsForImage && (rv64PrimitiveFunctionGlobalStores(c) || rv64MutableFuncrefGlobalClear(c) || rv64MutableFuncrefGlobalStoreSelf(c) || rv64MutableFuncrefGlobalCopy(c) || rv64MutableFuncrefGlobalMove(c) || rv64MutableFuncrefGlobalBidirectionalCopy(c) || rv64MutableFuncrefGlobalLocalCopy(c) || rv64MutableFuncrefGlobalLocalPublish(c) || rv64MutableFuncrefGlobalLocalTransfer(c) || rv64MutableFuncrefGlobalLocalExport(c) || rv64MutableFuncrefGlobalLocalObserve(c) || rv64MutableFuncrefGlobalLocalCall(c))
}

// retainGuestGlobalStoreDescriptor closes the raw-load/park ownership window
// for exact guest-global helper products. It runs while native execution is
// still leased, before a competing host writer can retire the container root.
// The caller owns the returned physical root and releases it while still
// parked, before native execution resumes; destination publication acquires its
// own independent unit.
func (in *Instance) retainGuestGlobalStoreDescriptor(dispatch uint32, args, results []uint64) (*Instance, error) {
	if !rv64MutableFuncrefGlobalsForImage || dispatch != hostFuncRefDispatchBit || in == nil || !railshotUsesGuestGlobalStoreHelper(in.c) || len(results) != 0 {
		return nil, nil
	}
	var descriptor uint64
	switch {
	case len(args) == 2 && args[1] < uint64(len(in.globalCells)) && rv64PrimitiveMutableFunctionGlobal(in.c, args[1]):
		descriptor = args[0]
	case rv64MutableFuncrefGlobalClear(in.c) && len(args) == 0 && len(in.globalCells) == 1:
		return nil, nil
	case rv64MutableFuncrefGlobalStoreSelf(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 0 && len(in.globalCells) == 2:
		return nil, nil
	case rv64MutableFuncrefGlobalBidirectionalCopy(in.c) && len(args) == 2 && len(in.globalCells) == 2 && args[1] < 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalPublish(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalTransfer(in.c) && len(args) == 2 && len(in.globalCells) == 3 && (args[1] == 1 || args[1] == 2):
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalExport(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalObserve(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	case rv64MutableFuncrefGlobalLocalCall(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		descriptor = args[0]
	default:
		return nil, fmt.Errorf("invalid RV64 guest global store bridge")
	}
	if descriptor == 0 {
		return nil, nil
	}
	if in.refStore != nil {
		if owner := in.refStore.retainDescriptorOwnerForFinalization(descriptor); owner != nil {
			return owner, nil
		}
	}
	lease := railshotCurrentNativeTableTargets(in)
	if lease.targets == nil {
		return nil, fmt.Errorf("RV64 guest global store descriptor owner is unavailable")
	}
	var retained *Instance
	for _, candidate := range lease.targets.owners[:lease.targets.count] {
		if _, valid := rv64PublishedDescriptor(candidate, descriptor); !valid {
			continue
		}
		if retained != nil && retained != candidate {
			return nil, fmt.Errorf("RV64 guest global store descriptor owner is ambiguous")
		}
		retained = candidate
	}
	if retained == nil || !retained.retainResourceRootForFinalization() {
		return nil, fmt.Errorf("RV64 guest global store descriptor owner is unavailable")
	}
	return retained, nil
}

// The zero-index host-funcref dispatch encoding is permanently invalid for a
// public HostFuncRef. The exact authenticated image uses it as a private bridge
// while retaining the ordinary parked-host lease and context restoration path.
func (in *Instance) dispatchGuestGlobalStore(dispatch uint32, args, results []uint64) bool {
	return in.dispatchGuestGlobalStoreWithOwner(dispatch, args, results, nil)
}

func (in *Instance) dispatchGuestGlobalStoreWithOwner(dispatch uint32, args, results []uint64, descriptorOwner *Instance) bool {
	if !rv64MutableFuncrefGlobalsForImage || dispatch != hostFuncRefDispatchBit {
		return false
	}
	if in == nil || len(results) != 0 {
		panic(invalidHostReference{err: fmt.Errorf("invalid RV64 guest global store bridge")})
	}
	var err error
	switch {
	case len(args) == 2 && args[1] < uint64(len(in.globalCells)) && rv64PrimitiveMutableFunctionGlobal(in.c, args[1]):
		index := int(args[1])
		if index < len(in.c.GlobalImports) {
			err = in.globalCells[index].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
		} else {
			err = in.copyGuestFuncrefDescriptorToLocal(index, args[0])
		}
	case rv64MutableFuncrefGlobalClear(in.c) && len(args) == 0 && len(in.globalCells) == 1:
		err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
	case rv64MutableFuncrefGlobalStoreSelf(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
	case rv64MutableFuncrefGlobalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[1].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[1].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalMove(in.c) && len(args) == 0 && len(in.globalCells) == 2:
		err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
	case rv64MutableFuncrefGlobalBidirectionalCopy(in.c) && len(args) == 2 && len(in.globalCells) == 2 && args[1] < 2:
		err = in.globalCells[args[1]].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
	case rv64MutableFuncrefGlobalLocalCopy(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
	case rv64MutableFuncrefGlobalLocalPublish(in.c) && len(args) == 1 && len(in.globalCells) == 2:
		if args[0] == 0 {
			err = in.globalCells[0].SetValue(ValueFuncRef(NullFuncRef()))
		} else {
			err = in.globalCells[0].setGuestFuncrefDescriptor(in, args[0])
		}
	case rv64MutableFuncrefGlobalLocalTransfer(in.c) && len(args) == 2 && len(in.globalCells) == 3 && (args[1] == 1 || args[1] == 2):
		if args[1] == 2 {
			err = in.copyGuestFuncrefDescriptorToLocal(2, args[0])
		} else {
			err = in.globalCells[args[1]].copyGuestFuncrefDescriptorWithOwner(in, args[0], descriptorOwner)
		}
	case rv64MutableFuncrefGlobalLocalExport(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	case rv64MutableFuncrefGlobalLocalObserve(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	case rv64MutableFuncrefGlobalLocalCall(in.c) && len(args) == 1 && len(in.globalCells) == 1:
		err = in.copyGuestFuncrefDescriptorToLocal(0, args[0])
	default:
		panic(invalidHostReference{err: fmt.Errorf("invalid RV64 guest global store bridge")})
	}
	if err != nil {
		panic(invalidHostReference{err: fmt.Errorf("RV64 guest global store: %w", err)})
	}
	return true
}
