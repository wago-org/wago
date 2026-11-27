// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_standalone || !wago_standalone_no_plugins || !wago_standalone_generated)

package wago

import "github.com/wago-org/wago/internal/buildfeatures"

const rv64NativeTailMetadataForImage = buildfeatures.TailCall != 0
const nativeTailPlansForImage = nativeTailForImage
const directNativeTailPlansForImage = nativeTailForImage
const indirectNativeTailPlansForImage = nativeTailForImage
const referenceNativeTailPlansForImage = nativeTailForImage
const staticNativeTailForImage = nativeTailPlansForImage
const legacyNativeTailReferenceCarriersForImage = nativeTailPlansForImage && referenceSignaturesBuilt
const rv64NativeOwnerScopeForImage = true
const rv64CallableTableRowsForImage = true
const typedCallKeysForImage = true
const rv64RootedReceiptsForImage = rv64RootedReceiptsBuilt
const rv64LegacyI31ProductMaskForImage = ^uint16(0)
const rv64LegacyI31ProductsForImage = i31ExecutionForImage && rv64LegacyI31ProductMaskForImage != 0
const stateOnlyTable32ForImage = true
const rv64MutableFuncrefGlobalsForImage = true
const rv64ReferenceFunctionsForImage = true
const rv64ReferenceCallsForImage = true
