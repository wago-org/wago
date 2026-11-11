// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package self defines the wago self command tree.
package self

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/self/uninstall"
	"github.com/wago-org/wago/cli/manager/commands/self/update"
)

type Environment interface {
	update.Environment
	uninstall.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "self", Summary: "update or uninstall Wago",
		Children: []*command.Cmd{
			update.Command(environment),
			uninstall.Command(environment),
		},
	}
}
