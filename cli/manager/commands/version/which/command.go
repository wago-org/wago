// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package which defines wago version which.
package which

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Which()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "which", Aliases: []string{"path"}, Summary: "print the active runtime executable path",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Which() },
	}
}
