// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && tinygo

package runtime

import x64 "github.com/wago-org/wago/src/core/encoder/amd64"

const scalarLeafArg0, scalarLeafArg1 = x64.RDI, x64.RSI

type tinygoScalarLeafFuncValue = funcValue

func scalarLeafBody(fn any, params uint32) []byte {
	entry := tinygoScalarLeafEntry(fn, params)
	if entry == 0 {
		return nil
	}
	text := tinygoScalarLeafText(entry)
	defined := scalarLeafDefinedArgs(params)
	for offset := 0; offset < len(text); {
		if text[offset] == 0xc3 && defined&(1<<x64.RAX) != 0 {
			return text[:offset]
		}
		size, ok := scalarLeafInstruction(text[offset:], &defined)
		if !ok {
			return nil
		}
		offset += size
	}
	return nil
}
