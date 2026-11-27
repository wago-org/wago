// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type gcReleaseBatch struct{}

func (*gcReleaseBatch) claimGCCollectorsLocked(*referenceStore) {}

func (*gcReleaseBatch) releaseGCCollectors(*resourceCleanup, func(*gc.Collector)) {}

func (*referenceStore) releaseUnclaimedGCCollector(*gc.Collector) {}

func releaseLifetimeEntries(_ *gc.Collector, entries referenceTokenEntries) {
	releaseReferenceEntries(entries)
}

func releaseReferenceEntries(entries referenceTokenEntries) {
	releaseReferenceEntriesWith(entries, releaseFuncrefTokenMetadata, nil, nil)
}
