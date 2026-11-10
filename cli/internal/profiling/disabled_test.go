// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_profile

package profiling

import (
	"github.com/wago-org/wago/cli/internal/command"
	"testing"
)

func TestProfileCommandIsAbsent(t *testing.T) {
	root := &command.Cmd{Name: "wago"}
	if Append(root) != root || root.Child("profile") != nil || Capture([]string{"profile", "capture"}) {
		t.Fatal("ordinary build exposes profiling")
	}
}
