// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package runtime

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

// commandEnvironment adapts the compiled runtime profile to command packages.
// Command parsing and execution flow remain in each commands/.../command.go.
type commandEnvironment struct{}

func (commandEnvironment) ProfileFlags() []command.Flag {
	return runtimeplugin.Flags()
}

func (commandEnvironment) LoadRuntime(config *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	return runtimeplugin.LoadRuntime(config, guestArgs)
}

func (commandEnvironment) ScopeLabel() string {
	return runtimeplugin.ScopeLabel()
}
