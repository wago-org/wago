// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package remove defines wago rm and wago plugin remove.
package remove

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name            string
	Global, Local   bool
	AcceptContracts bool
}

type Environment interface {
	Remove(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "remove", Aliases: []string{"rm"},
		Summary: "remove and disable a direct plugin", Args: "<plugin-id>",
		Automation: command.DryRun,
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "accept-contracts", Bool: true, Help: "accept changed exact contract bindings without prompting"},
		},
		Run: func(c *command.Ctx) {
			environment.Remove(Options{
				Name: c.One("<plugin-id>"), Global: c.Bool("global"), Local: c.Bool("local"),
				AcceptContracts: c.Bool("accept-contracts"),
			})
		},
	}
}
