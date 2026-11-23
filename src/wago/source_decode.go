// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"sync/atomic"
)

var idleCompilerDecode atomic.Pointer[wasm.CompilerDecodeScratch]

func borrowCompilerDecodeScratch() *wasm.CompilerDecodeScratch {
	if scratch := idleCompilerDecode.Swap(nil); scratch != nil {
		return scratch
	}
	return new(wasm.CompilerDecodeScratch)
}

func returnCompilerDecodeScratch(scratch *wasm.CompilerDecodeScratch) {
	scratch.Reset()
	idleCompilerDecode.CompareAndSwap(nil, scratch)
}

// decodeCompilerSource leaves body syntax to the validator on the successful
// compilation path. The ordinary decoder remains authoritative for malformed
// inputs, including rejection phase, offsets and ordering among decode errors.
func decodeCompilerSource(data []byte, features wasm.ValidationFeatures, scratch *wasm.CompilerDecodeScratch) (*wasm.Module, bool, error) {
	if os.Getenv("WAGO_FUSED_DECODE_VALIDATION") == "0" {
		m, err := wasm.DecodeModuleWithFeatures(data, features)
		return m, false, err
	}
	m, err := scratch.DecodeForValidation(data, features)
	if err != nil {
		m, err = wasm.DecodeModuleWithFeatures(data, features)
		return m, false, err
	}
	return m, true, nil
}
