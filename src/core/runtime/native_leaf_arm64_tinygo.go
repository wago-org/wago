// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64 && tinygo

package runtime

import "encoding/binary"

type tinygoScalarLeafFuncValue = tinygoARM64FuncValue

func scalarLeafBody(fn any, params uint32) []byte {
	entry := tinygoScalarLeafEntry(fn, params)
	if entry == 0 || entry%4 != 0 {
		return nil
	}
	text := tinygoScalarLeafText(entry)
	defined := uint32(1<<params) - 1
	for offset := 0; offset+4 <= len(text); offset += 4 {
		instruction := binary.LittleEndian.Uint32(text[offset:])
		if instruction == 0xd65f03c0 && defined&1 != 0 { // RET LR
			return text[:offset]
		}
		if !scalarLeafInstruction(instruction, &defined) {
			return nil
		}
	}
	return nil
}
