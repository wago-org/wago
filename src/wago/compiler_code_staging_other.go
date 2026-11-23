// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !arm64 || (!darwin && !linux && !tinygo) || (tinygo && ((!darwin && !linux) || !wago_tinygo_finalizers || (!gc.conservative && !gc.precise)))

package wago

const compilerCodeStaging = false

type compilerCodeStage struct{}

func (*Compiled) retainCodeStaging([]byte) {}

func (*Compiled) releaseCodeStagingLocked() error { return nil }

func (*Compiled) stagingCodeMapping() []byte { return nil }

func (*Compiled) takeCodeStagingLocked() []byte { return nil }
