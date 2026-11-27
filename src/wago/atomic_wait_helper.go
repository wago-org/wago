// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const errAtomicWaitInstanceClosed = sentinelError("wago: instance closed while waiting")

type atomicWaitHelperError struct{ err error }

func setAtomicWaitHelperPanicError(value any, err *error) bool {
	helper, ok := value.(atomicWaitHelperError)
	if !ok {
		return false
	}
	if atomicBlockingWaitRuntimeBuilt && errors.Is(helper.err, errAtomicWaitInstanceClosed) {
		*err = &coreruntime.TrapError{Code: coreruntime.TrapInterrupted}
	} else {
		*err = helper.err
	}
	return true
}

type atomicWaitInvocationContext struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

var atomicWaitInvocationContexts sync.Map // *Instance -> *atomicWaitInvocationContext

func (in *Instance) needsAtomicWaitContext() bool {
	return in != nil && in.c != nil && (in.c.usesAtomicWaitHelpers() || in.executionFlags.Load()&executionFlagForeignNativeCalls != 0)
}

func (in *Instance) publishAtomicWaitContext(parent context.Context) func() {
	if !atomicBlockingWaitRuntimeBuilt || in == nil || in.c == nil || !in.needsAtomicWaitContext() {
		return noOpCancellationWatch
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	state := &atomicWaitInvocationContext{ctx: ctx, cancel: cancel}
	atomicWaitInvocationContexts.Store(in, state)
	// Close can publish the invocation gate immediately before this entry is
	// installed. Checking after Store closes that missed-cancellation window;
	// the opposite ordering is caught by cancelAtomicWaitContext's map lookup.
	if in.isLogicallyClosed() {
		cancel(errAtomicWaitInstanceClosed)
	}
	return func() {
		// Threaded Invoke owns the instance invocation mutex until this cleanup,
		// so no later context for the same instance can be published concurrently.
		// Delete keeps this path compatible with TinyGo's sync.Map subset.
		atomicWaitInvocationContexts.Delete(in)
		cancel(context.Canceled)
	}
}

func cancelAtomicWaitContext(in *Instance) {
	if !atomicBlockingWaitRuntimeBuilt {
		return
	}
	if state, ok := atomicWaitInvocationContexts.Load(in); ok {
		state.(*atomicWaitInvocationContext).cancel(errAtomicWaitInstanceClosed)
	}
}

func (in *Instance) dispatchAtomicWaitHelper(helper uint32, args, results []uint64) {
	in.dispatchAtomicWaitHelperContext(helper, args, results, nil)
}

func atomicBlockingWaitHelper(helper uint32) bool {
	return atomicWait32RuntimeBuilt && helper == shared.AtomicHelperWait32 ||
		atomicWait64RuntimeBuilt && helper == shared.AtomicHelperWait64
}

func (in *Instance) dispatchAtomicWaitHelperContext(helper uint32, args, results []uint64, ctx context.Context) {
	if in == nil || in.memory == nil {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper has no shared memory")})
	}
	if len(results) < 1 {
		panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper %d has no result slot", helper)})
	}
	memoryIndex := uint32(0)
	memory := in.memory
	if indexedAtomicHelperForImage {
		indexedGrowth := atomicGrowthRuntimeBuilt && helper == shared.AtomicHelperMemoryGrow && len(args) == 2
		indexedNotify := atomicNotifyRuntimeBuilt && helper == shared.AtomicHelperNotify && len(args) == 4
		indexedWait := atomicBlockingWaitHelper(helper) && len(args) == 5
		if indexedGrowth || indexedNotify || indexedWait {
			memoryIndex = uint32(args[len(args)-1])
			args = args[:len(args)-1]
		}
		if memoryIndex != 0 {
			if !multiMemoryRuntimeBuilt || in.memoryDir == nil || int(memoryIndex) >= len(in.memoryDir.memories) {
				panic(atomicWaitHelperError{err: fmt.Errorf("atomic helper memory index %d is unavailable", memoryIndex)})
			}
			memory = in.memoryDir.memories[memoryIndex]
		}
	}
	if atomicGrowthRuntimeBuilt && helper == shared.AtomicHelperMemoryGrow {
		if len(args) != 1 || in.memoryDir == nil || len(in.memoryDir.native) < (int(memoryIndex)+1)*abi.MemoryDirEntryBytes {
			panic(atomicWaitHelperError{err: fmt.Errorf("shared memory.grow helper has invalid arguments or directory")})
		}
		maximum := binary.LittleEndian.Uint32(in.memoryDir.native[int(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirPolicyMaxPagesOffset:])
		address64 := atomicMemory64ForImage && in.c.memoryDef(int(memoryIndex)).Addr64
		if address64 && args[0] > uint64(^uint32(0)) {
			// Failed shared growth still reads the authoritative length for ordering.
			memory.currentPages()
			results[0] = ^uint64(0)
			return
		}
		previous := memory.growShared(uint32(args[0]), maximum)
		results[0] = uint64(previous)
		if address64 && previous == ^uint32(0) {
			results[0] = ^uint64(0)
		}
		return
	}
	if atomicBlockingWaitRuntimeBuilt && atomicBlockingWaitHelper(helper) && ctx == nil {
		ctx = context.Background()
		if state, ok := atomicWaitInvocationContexts.Load(in); ok {
			ctx = state.(*atomicWaitInvocationContext).ctx
		}
	}
	var (
		result uint32
		err    error
	)
	switch {
	case atomicNotifyRuntimeBuilt && helper == shared.AtomicHelperNotify:
		if len(args) != 3 {
			err = fmt.Errorf("atomic notify helper arity = %d, want 3", len(args))
			break
		}
		result, err = memory.notify(in.atomicHelperAddressAt(memoryIndex, args[0], args[2], 4), uint32(args[1]))
	case atomicWait32RuntimeBuilt && helper == shared.AtomicHelperWait32:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait32 helper arity = %d, want 4", len(args))
			break
		}
		result, err = memory.wait32(ctx, in.atomicHelperAddressAt(memoryIndex, args[0], args[3], 4), uint32(args[1]), int64(args[2]))
	case atomicWait64RuntimeBuilt && helper == shared.AtomicHelperWait64:
		if len(args) != 4 {
			err = fmt.Errorf("atomic wait64 helper arity = %d, want 4", len(args))
			break
		}
		result, err = memory.wait64(ctx, in.atomicHelperAddressAt(memoryIndex, args[0], args[3], 8), args[1], int64(args[2]))
	default:
		err = fmt.Errorf("unknown atomic wait helper %d", helper)
	}
	if err != nil {
		panic(atomicWaitHelperError{err: err})
	}
	results[0] = uint64(result)
}

func (in *Instance) atomicHelperAddressAt(memoryIndex uint32, address, offset, width uint64) uint64 {
	if !atomicMemory64ForImage {
		// The native Memory32 bridge carries a validated u32 memarg offset.
		return uint64(uint32(address)) + offset
	}
	if in.c == nil || !in.c.memoryDef(int(memoryIndex)).Addr64 {
		address = uint64(uint32(address))
	}
	effective := address + offset
	if effective < address {
		// Preserve the alignment-before-bounds trap order even when addition
		// exceeds 64 bits. An aligned impossible address cannot wrap into backing.
		return (^uint64(0) &^ (width - 1)) | (effective & (width - 1))
	}
	return effective
}
