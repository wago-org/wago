// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_gcplanscancheck

package gc

const promotionGroupScanCheckEnabled = true

var promotionGroupScanCount uint64

func notePromotionGroupScan() { promotionGroupScanCount++ }

func resetPromotionGroupScanCount() { promotionGroupScanCount = 0 }

func currentPromotionGroupScanCount() uint64 { return promotionGroupScanCount }
