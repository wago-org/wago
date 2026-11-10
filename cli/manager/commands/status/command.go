// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package status defines wago status.
package status

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Status()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "status",
		Aliases:    []string{"st"},
		Summary:    "show the active runtime, project, plugins, and lockfile",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Status() },
	}
}
