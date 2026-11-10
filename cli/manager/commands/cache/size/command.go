// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package size

import (
	"github.com/wago-org/wago/cli/internal/command"
	cacheoptions "github.com/wago-org/wago/cli/manager/commands/cache/options"
)

type Environment interface {
	CacheSize(cacheoptions.Selection)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "size",
		Summary:    "show cache disk usage",
		Automation: command.JSONOutput,
		Flags:      cacheoptions.Flags(),
		Run: func(ctx *command.Ctx) {
			selection := cacheoptions.Selected(ctx)
			if !selection.Downloads && !selection.Builds {
				selection.Downloads, selection.Builds = true, true
			}
			environment.CacheSize(selection)
		},
	}
}
