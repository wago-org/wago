// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package module defines the wago module command tree.
package module

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/runtime/commands/module/capabilities"
	"github.com/wago-org/wago/cli/runtime/commands/module/exports"
	"github.com/wago-org/wago/cli/runtime/commands/module/imports"
)

func Command() *command.Cmd {
	return &command.Cmd{
		Name: "module", Aliases: []string{"mod"},
		Summary: "inspect a module's imports, exports, and required capabilities",
		Children: []*command.Cmd{
			imports.Command(),
			exports.Command(),
			capabilities.Command(),
		},
	}
}
