// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

// FuncName returns the first name-section function name for a global function
// index. Empty names are reported as present.
func (n *NameSec) FuncName(idx uint32) (string, bool) {
	if n == nil {
		return "", false
	}
	for i := range n.FunctionNames {
		if n.FunctionNames[i].Index == idx {
			return n.FunctionNames[i].Name, true
		}
	}
	return "", false
}

// LocalName returns the first name-section local name for localIdx in global
// function index funcIdx. Empty names are reported as present.
func (n *NameSec) LocalName(funcIdx, localIdx uint32) (string, bool) {
	if n == nil {
		return "", false
	}
	for i := range n.LocalNames {
		if n.LocalNames[i].Index != funcIdx {
			continue
		}
		for j := range n.LocalNames[i].Names {
			if n.LocalNames[i].Names[j].Index == localIdx {
				return n.LocalNames[i].Names[j].Name, true
			}
		}
	}
	return "", false
}
