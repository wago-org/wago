// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

type failurePoint uint8

const (
	failPromotionPlan failurePoint = iota + 1
	failEvacuationCopy
	failEvacuationCommit
	failPromotionDestination
	failPromotionCommit
	failHandlePublication
	failObjectCardGrowth
	failSlotCardGrowth
	failBackingGrowth
	failThroughputReconciliation
)
