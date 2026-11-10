// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && !wago_regalloccheck

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

const regallocCheckEnabled = false

type regallocFnState struct{}
type regallocReadMask struct{}

func (*target) checkProtectRead(uint8, bool) regallocReadMask { return regallocReadMask{} }
func (*target) checkRestoreRead(regallocReadMask)             {}

type regallocGPWriteMask struct{}

func (*target) checkImmutable(enc.Reg)                     {}
func (*target) checkEndLifetimes()                         {}
func (*target) checkTerminalGPWrites() regallocGPWriteMask { return regallocGPWriteMask{} }
func (*target) checkRestoreGPWrites(regallocGPWriteMask)   {}
