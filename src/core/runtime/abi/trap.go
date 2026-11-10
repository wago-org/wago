// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package abi

// Trap values shared by native emitters and the production runtime.
const (
	TrapNone = iota
	TrapUnreachable
	TrapBuiltin
	TrapLinMemOutOfBounds
	TrapLinMemCouldNotExtend
	TrapIndirectOutOfBounds
	TrapIndirectWrongSig
	TrapLinkedMemNotLinked
	TrapLinkedMemOutOfBounds
	TrapDivZero
	TrapDivOverflow
	TrapTruncOverflow
	TrapInterrupted
	TrapStackFenceBreached
	TrapCalledFnNotLinked
	TrapUnsupportedTailCall
	TrapNullReference
	TrapUnhandledException
	TrapCastFailure
	TrapTableOutOfBounds
	TrapAtomicUnaligned
	TrapExpectedSharedMemory
	TrapHostEventOverflow
)
