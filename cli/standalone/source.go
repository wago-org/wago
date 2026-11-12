// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone

package standalone

import "github.com/wago-org/wago"

// Run executes source as a command and returns its process exit code. Plugins
// are handed in as one explicit, reviewed PluginSet.
func Run(source []byte, plugins wago.PluginSet, options Options, args []string) int {
	if err := execute(source, plugins, options, args); err != nil {
		return reportError(err, args)
	}
	return 0
}

func execute(source []byte, plugins wago.PluginSet, options Options, args []string) (err error) {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	defer finishRuntime(runtime, &err)
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	return executeModule(runtime, module, options, args)
}
