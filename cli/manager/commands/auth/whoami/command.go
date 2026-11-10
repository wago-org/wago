// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package whoami defines wago auth whoami.
package whoami

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Whoami()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "whoami", Aliases: []string{"who"}, Summary: "print the logged-in account",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Whoami() },
	}
}
