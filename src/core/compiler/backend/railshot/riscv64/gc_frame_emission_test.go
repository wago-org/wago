// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestRV64GCFrameEmissionState(t *testing.T) {
	plans := shared.NewGCModuleFrameRootPlan(2)
	plans.MarkFunction(1)
	plans.ReserveFunctions(1)
	plan, ok := plans.BeginFunction(1)
	if !ok {
		t.Fatal("allocate source frame plan")
	}
	opts := CompileOptions{GCFrameRoots: plans}
	module := newGCFrameModuleState(&opts)
	var unplanned, planned emissionMode
	module.bindFunction(&unplanned, 0)
	module.bindFunction(&planned, 1)

	word := unsafe.Sizeof(uintptr(0))
	var want [4]uintptr
	var wantPlan *shared.GCFrameRootPlan
	if buildfeatures.RISCV64CollectorFrameRoots {
		want = [4]uintptr{word, 2 * word, 3 * word, 2*word + 2*unsafe.Sizeof(uint32(0))}
		wantPlan = plan
	}
	got := [4]uintptr{
		unsafe.Sizeof(opts.gcFrameOperandPlanning),
		unsafe.Sizeof(module),
		unsafe.Sizeof(planned.gcFrameEmissionMode),
		unsafe.Sizeof(integerEmitter{}.integerGCFrameState),
	}
	if got != want {
		t.Fatalf("frame state sizes (options, module, function, emitter) = %v, want %v", got, want)
	}
	if unplanned.gcFramePlan() != nil || planned.gcFramePlan() != wantPlan {
		t.Fatal("frame plan selection disagrees with the build profile")
	}
}
