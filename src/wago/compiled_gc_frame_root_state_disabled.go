// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling)

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

type compiledGCFrameRootState struct{}

func (*Compiled) genericGCFrameRoots() *compiledGCFrameRoots { return nil }

func (*Compiled) needsExactNativeGCRoots() bool { return false }

func (*Compiled) needsNativeGCABI() bool { return false }

func (*Compiled) hasCollectorDeclarationRoots() bool { return false }

func (*Compiled) needsRuntimeGCCollectorDomain() bool { return false }

func (*Compiled) opaqueExternHelperSlots() int { return 0 }

func (*Compiled) compactExternFrameRoots() bool { return false }

func (*Compiled) setCompiledGCFrameRoots(*shared.GCModuleFrameRootPlan, []uint64) {}
