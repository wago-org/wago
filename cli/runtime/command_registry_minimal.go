// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_minimal

package runtime

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/profiling"
	runtimecommands "github.com/wago-org/wago/cli/runtime/commands"
	runcmd "github.com/wago-org/wago/cli/runtime/commands/run"
)

func buildCommandRegistry() *command.Cmd {
	return profiling.Append(runtimecommands.Registry(runcmd.Command(commandEnvironment{})))
}
