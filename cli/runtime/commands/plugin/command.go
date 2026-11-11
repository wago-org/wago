// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package plugin defines the runtime-owned wago plugin command tree.
package plugin

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/runtime/commands/plugin/inspect"
	"github.com/wago-org/wago/cli/runtime/commands/plugin/list"
)

type Environment interface {
	list.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "plugin", Aliases: []string{"plugins"},
		Summary: "list and inspect compiled plugins",
		Children: []*command.Cmd{
			list.Command(environment),
			inspect.Command(),
		},
	}
}
