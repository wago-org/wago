// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"errors"
	"fmt"
)

var errStartOutsideImageProfile = errors.New("compiled metadata invalid: start function exceeds the standalone runtime profile")

// Optional start metadata uses the Wasm function index space. The requirement
// bit makes old readers reject the extension rather than omit initialization.
// Non-start artifacts retain their existing byte format.
func compiledStartIndex(c *Compiled) (uint32, error) {
	if !startFunctionForImage {
		return 0, errStartOutsideImageProfile
	}
	if c.NumImports < 0 {
		return 0, fmt.Errorf("start function has a negative import count")
	}
	var sig FuncSig
	var index uint64
	if c.StartIsImport {
		if !importedStartForImage {
			return 0, errStartOutsideImageProfile
		}
		if c.StartImportIdx < 0 || c.StartImportIdx >= c.NumImports || c.StartImportIdx >= len(c.importFuncSigs) {
			return 0, fmt.Errorf("start function requires a bounded imported signature")
		}
		sig = c.importFuncSigs[c.StartImportIdx]
		index = uint64(c.StartImportIdx)
	} else {
		if !localStartForImage {
			return 0, errStartOutsideImageProfile
		}
		if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Funcs) || c.StartLocalFunc >= len(c.Entry) {
			return 0, fmt.Errorf("start function requires a bounded defined signature and entry")
		}
		sig = c.Funcs[c.StartLocalFunc]
		index = uint64(c.NumImports) + uint64(c.StartLocalFunc)
	}
	if index > uint64(^uint32(0)) || len(sig.Params) != 0 || len(sig.Results) != 0 {
		return 0, fmt.Errorf("start function requires a void signature and uint32 index")
	}
	return uint32(index), nil
}
