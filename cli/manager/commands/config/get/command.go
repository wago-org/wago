// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package get

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "get", Summary: "print one configured default", Args: "<setting>", Automation: command.JSONOutput,
		Flags: options.ScopeFlags(),
		Run: func(ctx *command.Ctx) {
			request := options.Request{Action: options.Get, Key: ctx.One("<setting>")}
			if err := options.ApplyScope(ctx, &request); err != nil {
				ui.Usage("config get: %v", err)
			}
			environment.Configure(request)
		},
	}
}
