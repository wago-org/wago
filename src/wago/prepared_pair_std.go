// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo && (amd64 || arm64) && (linux || darwin || windows)

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectPairSupported = true

func directIntPairArgument(bits uint64, slot int, wideMask uint8) uint64 {
	if pairIntNarrowArgsForImage == 0 {
		return bits
	}
	if pairIntWideArgsForImage == 0 {
		return uint64(uint32(bits))
	}
	lane := uint8(1 << slot)
	if (pairIntNarrowArgsForImage == 0xff || pairIntNarrowArgsForImage&lane != 0) &&
		(pairIntWideArgsForImage != 0xff && pairIntWideArgsForImage&lane == 0 || wideMask&lane == 0) {
		return uint64(uint32(bits))
	}
	return bits
}

func storeDirectIntPair(out []uint64, resultWide []bool, r0, r1 uint64) {
	out[0] = directBankValue(r0, 0, resultWide, pairIntNarrowResultsForImage, pairIntWideResultsForImage)
	out[1] = directBankValue(r1, 1, resultWide, pairIntNarrowResultsForImage, pairIntWideResultsForImage)
}

func (in *Instance) invokeDirectIntPairEntry(entry uintptr, paramWideMask uint8, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var raw [4]uint64
	for i, bits := range args {
		raw[i] = directIntPairArgument(bits, i, paramWideMask)
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1 := in.eng.EnterPreparedIntPairBounded(entry, in.jm.LinMemBase(), raw[0], raw[1], raw[2], raw[3])
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:2]
	storeDirectIntPair(out, resultWide, r0, r1)
	return out, nil
}

// invokeDirectIntPairSession uses the compiler's bounded two-register integer
// return ABI. The owner has already reserved the instance and direct gate.
func (fn *WasmFunc) invokeDirectIntPairSession(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	args := [4]uint64{a0, a1, a2, a3}
	if pairIntNarrowArgsForImage != 0 {
		for i := 0; i < fn.paramSlots; i++ {
			args[i] = directIntPairArgument(args[i], i, fn.scalarWideMask)
		}
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1 := in.eng.EnterPreparedIntPairBounded(fn.directEntry, fn.directLinMem, args[0], args[1], args[2], args[3])
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(fn)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:2]
	storeDirectIntPair(out, fn.resultWide, r0, r1)
	return out, nil
}
