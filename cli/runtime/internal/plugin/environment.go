// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_minimal

package plugin

import (
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

type pluginEnvironment struct {
	scope        string
	manifestDir  string
	dependencies []string
}

func resolvePluginEnvironment() (pluginEnvironment, error) {
	scope, err := project.ResolveScope(".", wagopaths.DirsFor("runtime").Data)
	if err != nil {
		return pluginEnvironment{}, err
	}
	return pluginEnvironment{scope: scope.Name, manifestDir: scope.ManifestDir, dependencies: scope.Dependencies}, nil
}

func ScopeLabel() string {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return "active scope"
	}
	return project.ScopeLabel(project.Scope{Name: environment.scope})
}
