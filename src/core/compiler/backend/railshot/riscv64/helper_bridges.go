// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"slices"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const referenceTableBridgesBuilt = buildfeatures.RISCV64CollectorFrameRoots && buildfeatures.ReferenceTypes != 0
const bulkTableHelpersBuilt = referenceTableBridgesBuilt && buildfeatures.BulkMemoryOperations != 0

// Reserve space for shared bridges and the GC group when it is built.
const maxCanonicalHelperBridges = 12 + (len(gcStructBridgeSpecs)+1+len(gcArrayAndConversionBridgeSpecs))*(buildfeatures.GC/buildfeatures.GCBit)

type helperBridge struct {
	helper    uint32
	slots     int
	safepoint uint32
}

func (b helperBridge) key() helperBridge {
	if buildfeatures.GC == 0 || b.helper != gcArrayAllocFixed && b.helper != gcStructAllocOne {
		b.slots = 0
	}
	return b
}

func (c directCall) bridgeKey() helperBridge {
	return (helperBridge{helper: c.helper, slots: c.helperSlots, safepoint: c.safepoint}).key()
}

func emittedGCHelperUses(entries map[helperBridge]int) uint64 {
	uses := shared.GCHelperUsesKnown
	for bridge := range entries {
		switch bridge.helper {
		case guestGlobalClear, guestGlobalStore, guestGlobalStoreIndexed:
			continue
		}
		uses |= shared.GCHelperUseBit(bridge.helper)
	}
	return uses
}

func appendCanonicalHelperBridges(bridges []helperBridge, ownedExceptions bool) []helperBridge {
	// Preserve the legacy zero-safepoint order in emitted images.
	if buildfeatures.RISCV64CollectorFrameRoots {
		bridges = append(bridges, helperBridge{helper: gcExternGlobalSet, slots: 2})
	}
	if buildfeatures.GC != 0 {
		bridges = append(bridges, gcStructBridgeSpecs[:]...)
	}
	if buildfeatures.ReferenceSignatures {
		bridges = append(bridges, helperBridge{helper: gcFunctionSubtypeTest, slots: 3})
	}
	if buildfeatures.GC != 0 {
		bridges = append(bridges, helperBridge{helper: gcStructRefCast, slots: 4})
	}
	if buildfeatures.RISCV64CollectorFrameRoots {
		bridges = append(bridges, helperBridge{helper: gcStructTableSet, slots: 3})
	}
	if buildfeatures.GC != 0 {
		bridges = append(bridges, gcArrayAndConversionBridgeSpecs[:]...)
	}
	if referenceTableBridgesBuilt {
		bridges = append(bridges, helperBridge{helper: gcTableFill, slots: 4})
		if bulkTableHelpersBuilt {
			bridges = append(bridges, helperBridge{helper: gcTableCopy, slots: 5})
		}
		bridges = append(bridges, helperBridge{helper: gcTableGrow, slots: 3})
		if bulkTableHelpersBuilt {
			bridges = append(bridges, helperBridge{helper: gcTableInit, slots: 5}, helperBridge{helper: gcTableElemDrop, slots: 1})
		}
	}
	bridges = append(bridges,
		helperBridge{helper: guestGlobalClear},
		helperBridge{helper: guestGlobalStore, slots: 1},
		helperBridge{helper: guestGlobalStoreIndexed, slots: 2},
	)
	if buildfeatures.ExceptionHandling != 0 && ownedExceptions {
		bridges = append(bridges, helperBridge{helper: gcExceptionPayload, slots: 1})
	}
	return bridges
}

func (b helperBridge) makeThunk() []byte {
	switch b.helper {
	case guestGlobalClear, guestGlobalStore, guestGlobalStoreIndexed:
		return HostIndirectOwnedSyncThunk(guestGlobalStoreDispatch, b.slots, 0)
	}
	payload, ok := shared.EncodeGCDispatch(b.helper, b.safepoint)
	if !ok {
		return nil
	}
	results := 1
	switch {
	case buildfeatures.RISCV64CollectorFrameRoots && (b.helper == gcExternGlobalSet || b.helper == gcStructTableSet),
		referenceTableBridgesBuilt && b.helper == gcTableFill,
		bulkTableHelpersBuilt && (b.helper == gcTableCopy || b.helper == gcTableInit || b.helper == gcTableElemDrop),
		buildfeatures.GC != 0 && gcObjectBridgeHasNoResult(b.helper):
		results = 0
	}
	return HostIndirectOwnedSyncThunk(gcStructDispatchBit|payload, b.slots, results)
}

// appendInternalHelperBridges appends only the image-relative bridges referenced
// by this module. Safepointed calls retain their exact helper and root plan.
func appendInternalHelperBridges(code []byte, calls []directCall, codeLimit int, ownedExceptions bool) ([]byte, map[helperBridge]int, error) {
	var canonical [maxCanonicalHelperBridges]helperBridge
	bridges := appendCanonicalHelperBridges(canonical[:0], ownedExceptions)
	for _, call := range calls {
		if call.helper == 0 || call.safepoint == 0 {
			continue
		}
		slots, ok := gcAllocHelperSlots(call.helper, call.helperSlots)
		if !ok {
			return nil, nil, unsupported("missing canonical GC helper bridge shape")
		}
		bridge := helperBridge{helper: call.helper, slots: slots, safepoint: call.safepoint}
		if !slices.Contains(bridges, bridge) {
			bridges = append(bridges, bridge)
		}
	}
	var entries map[helperBridge]int
	for _, bridge := range bridges {
		key := bridge.key()
		used := false
		for _, call := range calls {
			if call.bridgeKey() == key {
				used = true
				break
			}
		}
		if !used {
			continue
		}
		padding := (-len(code)) & 15
		thunk := bridge.makeThunk()
		if len(thunk) == 0 || padding > codeLimit-len(code) || len(thunk) > codeLimit-len(code)-padding {
			return nil, nil, unsupported("internal helper bridge image bound")
		}
		for ; padding > 0; padding -= 4 {
			code = append(code, 0x13, 0, 0, 0)
		}
		if entries == nil {
			entries = make(map[helperBridge]int, 12)
		}
		entries[key] = len(code)
		code = append(code, thunk...)
	}
	return code, entries, nil
}
