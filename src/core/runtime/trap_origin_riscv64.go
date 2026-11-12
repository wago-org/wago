// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func (j *JobMemory) SetRV64NativeContext(identity uintptr) {
	j.putU64(abi.RV64NativeContextOffset, uint64(identity))
}

func (j *JobMemory) RV64NativeContext() uintptr {
	return uintptr(j.getU64(abi.RV64NativeContextOffset))
}

func nativeTrapOrigin(trap []byte) (uintptr, bool) {
	return uintptr(binary.LittleEndian.Uint64(trap[8:16])), true
}

// SetRV64InvocationView and RV64InvocationView access only native-lease-owned
// basedata. Prototype context rebinding deliberately leaves this slot intact.
func (j *JobMemory) SetRV64InvocationView(pointer uintptr) {
	j.putU64(abi.RV64InvocationViewOffset, uint64(pointer))
}
func (j *JobMemory) RV64InvocationView() uintptr {
	return uintptr(j.getU64(abi.RV64InvocationViewOffset))
}
