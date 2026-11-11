// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
)

//go:noescape
func inlineHostMultiViewEnter(fn FixedHostCallView, code, args, linMem, trap, results, stack, ctrl uintptr, signatures []uint32) uint64

func (e *Engine) tryInlineBoundedMultiHostView(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, signatures []uint32, fixed FixedHostCallView) (bool, error) {
	if !inlineHostEnabled {
		return false, nil
	}
	old := binary.LittleEndian.Uint64(ctrl[hcTrampoline:])
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(inlineHostViewBridgeAddr()))
	defer func() {
		binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], old)
		goruntime.KeepAlive(e)
		goruntime.KeepAlive(args)
		goruntime.KeepAlive(trap)
		goruntime.KeepAlive(results)
		goruntime.KeepAlive(ctrl)
	}()
	if outcome := inlineHostMultiViewEnter(fixed, code, slicePtr(args), linMem, slicePtr(trap), slicePtr(results), e.stackTop, slicePtr(ctrl), signatures); outcome == 1 {
		return true, fmt.Errorf("jit: inline multi-host view escaped admitted import")
	}
	if tc := loadTrap(trap); tc != 0 {
		return true, trapErrorFromBuffer(TrapCode(tc), trap)
	}
	return true, nil
}
