// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package logout defines wago auth logout.
package logout

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Logout()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "logout", Summary: "remove stored registry credentials",
		Automation: command.DryRun,
		Run:        func(*command.Ctx) { environment.Logout() },
	}
}
