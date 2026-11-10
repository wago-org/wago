// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_regalloccheck

package codegen

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestSourceFinalBudgetHistoricalAndClose(t *testing.T) {
	for _, mode := range []string{"work", "storage", "retry", "close"} {
		t.Run(mode, func(t *testing.T) {
			m, a := validatedContextModule(t)
			opts := SourceOptions(Options{}, m, a, wasm.ValidationFeatures{})
			ctx := SourceContextFor(opts, m)
			if mode == "work" {
				ctx.finalWork = 0
			}
			if mode == "storage" {
				ctx.finalStorage = 0
			}
			ok := ReserveFinalSourcePass(ctx, m)
			if ok != (mode == "retry" || mode == "close") {
				t.Fatalf("reserved=%v", ok)
			}
			if ReserveFinalSourcePass(ctx, m) {
				t.Fatal("retry replenished final credits")
			}
			CloseSourceContext(opts, m)
			if ctx.finalWork != 0 || ctx.finalStorage != 0 || ReserveFinalSourcePass(ctx, m) {
				t.Fatal("retained/replenished final credits")
			}
		})
	}
}
