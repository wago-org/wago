// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package plugin defines shared pieces of the wago plugin command tree.
package plugin

import "github.com/wago-org/wago/cli/internal/command"

func Command(summary string, children []*command.Cmd) *command.Cmd {
	return &command.Cmd{
		Name: "plugin", Aliases: []string{"plugins"},
		Summary: summary, Children: children,
	}
}

func GlobalFlag() command.Flag {
	return command.Flag{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"}
}

func LocalFlag() command.Flag {
	return command.Flag{Name: "local", Short: "l", Bool: true, Help: "use this project's plugins"}
}
