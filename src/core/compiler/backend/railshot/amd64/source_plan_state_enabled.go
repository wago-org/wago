// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && wago_regalloccheck

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Pointer keys locate associations only; source identity and lifetime come from
// the plan's owner-bound logical slots and fresh generations.
type nativeSourceAssociation struct {
	producer shared.SourceMaterializationProducer
	slot     uint32
	ref      shared.SourceSlotRef
}
type nativeSourcePlanState struct {
	physical                  *shared.SourceIntegerPhysical
	materialization           *shared.SourceMaterializationJournal
	materializationToken      shared.SourceMaterializationToken
	owner                     *shared.IntegerSourcePlan
	locals                    []shared.SourceNodeRef
	associations              map[*elem]nativeSourceAssociation
	nextSlot                  uint32
	nextEvent                 int
	pending, selected, sealed bool
	contract                  shared.SourceEventContract
	recipe                    shared.SourceRecipeToken
	rule                      shared.SourceRecipeRule
	start, count              int
	roots                     [128]*elem
	nodes                     [128]shared.SourceNodeRef
}
