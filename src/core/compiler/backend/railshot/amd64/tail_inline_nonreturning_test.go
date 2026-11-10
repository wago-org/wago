// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestTailInlineNonReturningHelperWithPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, k, add = wasm.I64, 0x42, 0x7c
		}
		for _, localPrefix := range []bool{false, true} {
			caller := []byte{0, k, 5}
			if localPrefix {
				caller = []byte{0, 0x20, 1}
			}
			caller = append(caller, 0x20, 0, 0x10, 1, add, 0x0b)
			for _, kind := range []string{"countdown", "immediate-trap", "self-tail"} {
				helper := []byte{0}
				switch kind {
				case "countdown":
					helper = append(helper, 0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b, 0x20, 0, 0x41, 1, 0x6b)
				case "immediate-trap":
					helper = append(helper, 0x00, 0x20, 0)
				case "self-tail":
					helper = append(helper, 0x20, 0)
				}
				helper = append(helper, 0x12, 1, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{typ}, helper})
				for _, enabled := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"tail-loop-inline": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					c, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatalf("wide=%t local-prefix=%t kind=%s enabled=%t compile: %v", wide, localPrefix, kind, enabled, err)
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
					if kind != "self-tail" {
						_, _, err = runMemAmd64WithOptions(t, m, opts, nil, 2, 17)
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapUnreachable {
							t.Fatalf("kind=%s enabled=%t err=%v want unreachable trap", kind, enabled, err)
						}
					}
					if diagnosticsEnabled && enabled && !profileEnabled && stats.Funcs[0].Peephole["inline-prefix-retained"] == 0 {
						t.Fatalf("kind=%s fixture did not retain caller prefix", kind)
					}
				}
			}
		}
	}
}
