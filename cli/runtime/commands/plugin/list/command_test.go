// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package list

import (
	"testing"

	"github.com/wago-org/wago"
)

func TestPlanEntry(t *testing.T) {
	plan := &wago.PluginPlan{Plugins: []wago.PluginPlanEntry{{ID: "github.com/acme/a"}, {ID: "github.com/acme/b"}}}
	if got := planEntry(plan, "github.com/acme/b"); got == nil || got.ID != "github.com/acme/b" {
		t.Fatalf("planEntry = %#v", got)
	}
	if got := planEntry(plan, "github.com/acme/missing"); got != nil {
		t.Fatalf("missing planEntry = %#v", got)
	}
}
