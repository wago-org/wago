// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

import "testing"

func TestPluginUpdateRequiredNeedsBuildAndLockToMatch(t *testing.T) {
	latest := "v1.2.3"
	if pluginUpdateRequired(latest, latest, latest, false) {
		t.Fatal("matching state should not update")
	}
	if !pluginUpdateRequired("v1.2.2", latest, latest, false) || !pluginUpdateRequired(latest, "v1.2.2", latest, false) {
		t.Fatal("stale build or lock should update")
	}
	if !pluginUpdateRequired(latest, latest, latest, true) {
		t.Fatal("force should update matching state")
	}
}
