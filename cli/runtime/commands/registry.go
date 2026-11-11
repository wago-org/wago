// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package commands assembles the runtime-owned command registry.
package commands

import "github.com/wago-org/wago/cli/internal/command"

// Registry returns a runtime root with commands in help-display order.
func Registry(children ...*command.Cmd) *command.Cmd {
	return &command.Cmd{Name: "wago", Children: children}
}
