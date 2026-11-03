// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !(linux || (darwin && arm64)) || tinygo || wago_target_tinygo

package gc

type objectEdgeSourceDisabled struct{}

func (*objectEdgeSourceDisabled) ObjectEdgeCount(Ref) uint32   { return 0 }
func (*objectEdgeSourceDisabled) ObjectEdgeAt(Ref, uint32) Ref { return Null() }

func (*Collector) objectEdgeSource() *objectEdgeSourceDisabled  { return nil }
func (*Collector) objectEdgeCount(Ref) uint32                   { return 0 }
func (*Collector) persistentRootIsStrong(SlotKind, uint32) bool { return true }
func (*Collector) objectEdgesMutated(Ref)                       {}
func (*Collector) objectRootPublished(Ref)                      {}
func (*Collector) closeObjectEdgeSource()                       {}
