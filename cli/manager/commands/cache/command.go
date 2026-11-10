// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package cache defines wago cache.
package cache

import (
	"github.com/wago-org/wago/cli/internal/command"
	cacheclean "github.com/wago-org/wago/cli/manager/commands/cache/clean"
	"github.com/wago-org/wago/cli/manager/commands/cache/dir"
	"github.com/wago-org/wago/cli/manager/commands/cache/prune"
	"github.com/wago-org/wago/cli/manager/commands/cache/size"
)

type Environment interface {
	dir.Environment
	size.Environment
	cacheclean.Environment
	prune.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "cache",
		Summary: "inspect and clean regenerable Wago data",
		Children: []*command.Cmd{
			dir.Command(environment),
			size.Command(environment),
			prune.Command(environment),
			cacheclean.Command(environment),
		},
	}
}
