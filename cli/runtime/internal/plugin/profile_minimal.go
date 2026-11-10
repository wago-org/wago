// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_minimal

package plugin

import (
	"errors"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func Flags() []command.Flag { return nil }

func LoadRuntime(cfg *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(cfg), wago.WithGuestArguments(guestArgs))
}

func Summary() string { return compiledPluginSummary() }

func ScopeLabel() string { return "bare" }

func Configure(set wago.PluginSet) {
	if len(set.Providers) != 0 || len(set.Selections) != 0 {
		panic("minimal Wago runtime cannot link plugins")
	}
}

func Verify() error { return nil }

func Definitions() []wago.PluginDefinition { return nil }

func Definition(string) (wago.PluginDefinition, bool) { return wago.PluginDefinition{}, false }

func PluginSet() wago.PluginSet { return wago.PluginSet{} }

func Inspect() (*wago.PluginPlan, error) {
	return nil, errors.New("plugins are unavailable in the minimal runtime")
}
