// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func testGCPlanWithCallsites(t *testing.T, adapter uint32, sites ...[2]uint32) *shared.GCFrameRootPlan {
	t.Helper()
	p := &shared.GCFrameRootPlan{AdapterReturnOffset: adapter}
	for _, site := range sites {
		if !p.AppendCallsite(site[0], site[1], nil) {
			t.Fatal("failed to append test GC callsite")
		}
	}
	return p
}

func testGCCallsiteReturn(t *testing.T, p *shared.GCFrameRootPlan, index int) uint32 {
	t.Helper()
	site, ok := p.Callsite(index)
	if !ok {
		t.Fatalf("missing GC callsite %d", index)
	}
	return site.ReturnOffset()
}
