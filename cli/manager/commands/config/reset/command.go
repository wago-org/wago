// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package reset

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

func Command(environment options.Environment) *command.Cmd {
	return &command.Cmd{
		Name: "reset", Summary: "reset settings in the selected scope", Args: "[setting]", Automation: command.JSONOutput | command.DryRun,
		Flags: append([]command.Flag{
			{Name: "all", Short: "a", Bool: true, Help: "reset every setting"},
			{Name: "experimental", Short: "x", Bool: true, Help: "allow resetting an experimental setting"},
		}, options.ScopeFlags()...),
		Run: func(ctx *command.Ctx) {
			key := ctx.Optional("[setting]")
			if key == "" && !ctx.Bool("all") {
				ui.Usage("config reset: provide <setting> or --all")
			}
			if key != "" && ctx.Bool("all") {
				ui.Usage("config reset: choose <setting> or --all")
			}
			request := options.Request{Action: options.Reset, Key: key, All: ctx.Bool("all"), Experimental: ctx.Bool("experimental")}
			if err := options.ApplyScope(ctx, &request); err != nil {
				ui.Usage("config reset: %v", err)
			}
			environment.Configure(request)
		},
	}
}
