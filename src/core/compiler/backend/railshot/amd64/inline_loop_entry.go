// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The first parameterized loop always executes its leading local.set bindings
// on entry. Prove the entire straight-line argument prefix and those bindings;
// a declared local still needs zero if its old value was read before any write.
func inlineLoopEntryInitializedLocals(m *wasm.Module, body []byte, params int, types []machineType) uint64 {
	if m == nil || params < 0 || params > len(types) || len(types) > 64 {
		return 0
	}
	r := wasm.NewReader(body)
	var stack [16]machineType
	depth := 0
	var written, readBeforeWrite, initialized uint64
	push := func(typ machineType) bool {
		if depth == len(stack) || (typ != mtI32 && typ != mtI64) {
			return false
		}
		stack[depth] = typ
		depth++
		return true
	}
	write := func(x uint32) {
		bit := uint64(1) << x
		if int(x) >= params && readBeforeWrite&bit == 0 {
			initialized |= bit
		}
		written |= bit
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return 0
		}
		switch op {
		case 0x01:
		case 0x41:
			if _, err := r.I32(); err != nil || !push(mtI32) {
				return 0
			}
		case 0x42:
			if _, err := r.I64(); err != nil || !push(mtI64) {
				return 0
			}
		case 0x20:
			x, err := r.U32()
			if err != nil || uint64(x) >= uint64(len(types)) || !push(types[x]) {
				return 0
			}
			if int(x) >= params && written&(uint64(1)<<x) == 0 {
				readBeforeWrite |= uint64(1) << x
			}
		case 0x21, 0x22:
			x, err := r.U32()
			if err != nil || uint64(x) >= uint64(len(types)) || depth == 0 || stack[depth-1] != types[x] {
				return 0
			}
			if op == 0x21 {
				depth--
			}
			write(x)
		case 0x03:
			index, err := r.I64()
			if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
				return 0
			}
			ft, ok := m.TypeFunc(uint32(index))
			if !ok || !sigIsIntOnly(ft) || len(ft.Params) < 1 || len(ft.Params) > 4 || depth < len(ft.Params) {
				return 0
			}
			for i, typ := range ft.Params {
				if stack[depth-len(ft.Params)+i] != mtOf(typ) {
					return 0
				}
			}
			for i := len(ft.Params) - 1; i >= 0; i-- {
				op, err := r.Byte()
				if err != nil || op != 0x21 {
					return 0
				}
				x, err := r.U32()
				if err != nil || uint64(x) >= uint64(len(types)) || types[x] != mtOf(ft.Params[i]) {
					return 0
				}
				write(x)
			}
			return initialized
		default:
			return 0
		}
	}
	return 0
}
