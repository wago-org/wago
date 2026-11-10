// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package plugin

import (
	"testing"

	"github.com/wago-org/wago/internal/wagopaths"
)

func TestGlobalPluginDirectoryIsSharedV1Intent(t *testing.T) {
	dirs := wagopaths.Dirs{Data: t.TempDir()}
	if got := sharedGlobalPluginDir(dirs); got != dirs.Data {
		t.Fatalf("sharedGlobalPluginDir = %q, want %q", got, dirs.Data)
	}
}
