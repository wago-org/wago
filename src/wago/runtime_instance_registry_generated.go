// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins && wago_standalone_generated

package wago

type runtimeInstanceRegistry struct {
	attach func(*Runtime, *Instance, *runtimeInstanceReservation) error
	detach func(*Runtime, *Instance)
}

type runtimeInstanceRegistryBridge struct {
	instanceRegistry *runtimeInstanceRegistry
}

// Managed startup installs the registry. A directly instantiated image keeps
// the instance builder and teardown independent of Runtime's registry code.
var standaloneRuntimeInstanceRegistry = runtimeInstanceRegistry{
	attach: (*Runtime).registerInstance,
	detach: (*Runtime).unregisterInstance,
}

func (rt *Runtime) initInstanceRegistry() {
	rt.instanceRegistry = &standaloneRuntimeInstanceRegistry
}

func (rt *Runtime) attachInstance(in *Instance, reservation *runtimeInstanceReservation) error {
	return rt.instanceRegistry.attach(rt, in, reservation)
}

func (rt *Runtime) detachInstance(in *Instance) {
	rt.instanceRegistry.detach(rt, in)
}
