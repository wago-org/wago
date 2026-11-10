// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func instanceNativeGCDomainID(in *Instance) uint64 {
	if in == nil || in.nativeContext == 0 {
		return 0
	}
	context := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	return binary.LittleEndian.Uint64(context[coreruntime.InstanceContextGCDomainOffset:])
}
