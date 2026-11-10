// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package current defines wago version current.
package current

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Current()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "current", Aliases: []string{"active"}, Summary: "print the active runtime, profile, and build",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Current() },
	}
}
