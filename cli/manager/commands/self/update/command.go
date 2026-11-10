// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package update defines wago self update.
package update

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Update(force bool)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "update", Aliases: []string{"up", "upgrade"}, Summary: "update Wago on its current release channel",
		Automation: command.DryRun,
		Flags:      []command.Flag{{Name: "force", Short: "f", Bool: true, Help: "reinstall even when the commit matches"}},
		Run:        func(ctx *command.Ctx) { environment.Update(ctx.Bool("force")) },
	}
}
