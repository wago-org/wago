// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestProperTailReferenceResultContractsExecuteAcrossKinds(t *testing.T) {
	for _, kind := range []properTailBehaviorKind{properTailDirect, properTailIndirect, properTailRef} {
		t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
			compiled := compileProperTailBehavior(t, properTailResultModule(kind, []wasm.ValType{wasm.FuncRef}, []byte{0xd2, 0x00, 0x0b}), 2)
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.InvokeValues(context.Background(), "run")
			if err != nil || len(got) != 1 || got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() || !in.FuncRefMatchesFunction(got[0].FuncRef(), 0) {
				t.Fatalf("proper-tail funcref result = %v, %v", got, err)
			}
		})
	}
}
