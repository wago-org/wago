// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins && wago_standalone_generated

package wago

import (
	"context"
	"errors"
)

// PrepareStandaloneImage applies the adoption checks for the generated CLI.
func PrepareStandaloneImage(c *Compiled, config *RuntimeConfig) (*Compiled, error) {
	if c == nil || config == nil {
		return nil, errors.New("wago: nil runtime or compiled module")
	}
	if err := checkArtifactAdmissionLimits(c, config.maxNativeCodeBytes, config.maxMemoriesPerModule); err != nil {
		return nil, err
	}
	return prepareModuleImage(c, config.maxCompiledMetadataBytes)
}

// InvokeStandaloneImageExport enters the first call of a linked instance.
// Its construction has not populated the public invocation cache.
func InvokeStandaloneImageExport(in *Instance, export string, args []uint64) ([]uint64, error) {
	return in.invokeEntry(export, args, invocationContextSet{})
}

// RunStandaloneImageInstance owns the instance and its reference store through
// the invocation. The caller retains ownership of the compiled image.
func RunStandaloneImageInstance(image, owner *Compiled, config *RuntimeConfig, run func(*Instance) error) (err error) {
	opts := config.instanceExecutionOptions(config.independentInstances)
	opts.startContext = context.Background()
	opts.ownedImports = true
	if referenceStoreForImage {
		store := newReferenceStore(false)
		opts.store = store
		defer func() {
			err = joinPrimary(err, closeRuntimeReferenceStore(store))
		}()
	}
	in, err := instantiateCoreWithModuleLease(image, owner, opts, moduleUseLease{})
	if standaloneStopsBeforeInstance || err != nil {
		return err
	}
	defer func() {
		if referenceStoreForImage {
			err = joinPrimary(err, in.closeAndWait())
		} else {
			err = joinPrimary(err, in.Close())
		}
	}()
	return run(in)
}
