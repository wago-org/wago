// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64

package wago

type railshotTableEntryLease struct{}

func railshotRequiresNativeOwnerScope(*Compiled) bool { return false }

func railshotAcquireNativeTableTargets(*Instance) (railshotTableEntryLease, error) {
	return railshotTableEntryLease{}, nil
}
func railshotReleaseNativeTableTargets(railshotTableEntryLease) {}
func railshotCurrentNativeTableTargets(*Instance) railshotTableEntryLease {
	return railshotTableEntryLease{}
}
func railshotRefreshNativeTableTargets(railshotTableEntryLease) {}

func railshotRetireNativeTableTargets(railshotTableEntryLease) error { return nil }
func railshotRetryNativeTableRetirement(*Instance) error             { return nil }

func railshotRetryClosedNativeTableRetirement(*Instance) error { return nil }

func railshotRequiresNativeHostControl(*Compiled) bool { return false }

func railshotRetainedFunctionExactType(*Instance, uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	return ValueTypeDescriptor{}, nil, false
}
