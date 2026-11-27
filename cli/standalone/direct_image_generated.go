// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins && wago_standalone_generated

package standalone

import (
	"errors"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/buildfeatures"
	impl "github.com/wago-org/wago/src/wago"
)

// Only a host callable can request a guest exit.
const standaloneGuestExit = buildfeatures.StandaloneFunctionImports

const standaloneDirectImage = buildfeatures.StandaloneHasSignature &&
	(!buildfeatures.StandaloneReferenceStore || !buildfeatures.StandaloneModuleImports)

func executeDirectImage(compiled *wago.Compiled, config *wago.RuntimeConfig, export string, args []string) (err error) {
	image, err := impl.PrepareStandaloneImage(compiled, config)
	if err != nil {
		if compiled != nil {
			return errors.Join(err, compiled.Close())
		}
		return err
	}
	defer func() { err = errors.Join(err, compiled.Close()) }()
	invoke, values, results, err := invocationArguments(image, export, args)
	if err != nil {
		return err
	}
	// Resolve invocation arguments before reporting an unbound import.
	if buildfeatures.StandaloneUnboundFunctionImport != "" {
		return unboundFunctionImportError(buildfeatures.StandaloneUnboundFunctionImport)
	}
	var run func(*wago.Instance) error
	if buildfeatures.StandaloneUnboundStorageDiagnostic == "" && buildfeatures.StandaloneUnboundTagImport == "" {
		run = func(instance *wago.Instance) error {
			result, err := impl.InvokeStandaloneImageExport(instance, invoke, values)
			if err != nil {
				return err
			}
			printStandaloneResults(invoke, result, results)
			return nil
		}
	}
	return impl.RunStandaloneImageInstance(image, compiled, config, run)
}
