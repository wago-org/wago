// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64

package wago

// Existing targets keep no container-receipt sidecar. The batch still defers
// physical release until the common caller has dropped native/container locks.
type globalDescriptorDependencies struct{}

type globalDescriptorDependencyBatch struct {
	releases importDedup[*Instance]
}

func railshotRecordGlobalDescriptorDependencyLocked(*globalOwner, *Instance, *retainedInstanceRoot, *globalDescriptorDependencyBatch) {
}

func railshotPublishGlobalDescriptorDependencyLocked(*globalOwner, *retainedInstanceRoot, *globalDescriptorDependencyBatch) {
}

func railshotRetireGlobalDescriptorDependencyLocked(_ *globalOwner, producer *Instance, _ *retainedInstanceRoot, batch *globalDescriptorDependencyBatch) {
	batch.releases.add(producer)
}

func railshotRetryGlobalDescriptorDependenciesLocked(*globalOwner, *globalDescriptorDependencyBatch) {
}

func railshotFlushGlobalDescriptorDependencies(batch *globalDescriptorDependencyBatch) {
	batch.releases.drain((*Instance).releaseResourceRoot)
}

func railshotLinkOwnedGlobalDescriptorHolderLocked(*Instance, *globalOwner) {}
func railshotReleaseOwnedGlobalDescriptorDependencies(*Instance)            {}
func railshotRetryClosedGlobalDescriptorRetirement(*Instance) error         { return nil }
