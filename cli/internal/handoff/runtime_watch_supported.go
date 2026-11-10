// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || windows) && !wago_lean

package handoff

import "github.com/wago-org/wago/cli/internal/command"

func runtimeWatchFlags() []command.Flag {
	return []command.Flag{
		{Name: "watch", Short: "w", Bool: true, Help: "rerun when the module changes"},
		{Name: "watch-interval", Arg: "<duration>", Help: "watch polling interval (default 200ms)"},
	}
}
