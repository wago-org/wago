// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package dir

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	CacheDir()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "dir",
		Summary:    "print the global cache directory",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.CacheDir() },
	}
}
