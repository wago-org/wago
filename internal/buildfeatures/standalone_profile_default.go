// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins || !wago_standalone_generated

package buildfeatures

// StandaloneNativeCollector permits the collector in builds without an image profile.
const StandaloneNativeCollector = true

// StandaloneExceptionHandling retains handler cleanup in unsealed builds.
const StandaloneExceptionHandling = true

// StandaloneForeignHostControl permits foreign frames in unsealed builds.
const StandaloneForeignHostControl = true
const StandaloneNativeStackHostControl = true

// Builds without a sealed image support the full set of integer entries.
const StandaloneDirectIntFixed = true
const StandaloneDirectIntBoundedLight = true
const StandaloneFixedIntArgCount = 4
const StandaloneBoundedLightIntArgCount = 4
const StandaloneDirectIntUnboundedFull = true
const StandaloneDirectIntUnboundedLight = true

// StandaloneHasSignature distinguishes a baked export from dynamic selection.
const StandaloneHasSignature = false
const StandaloneInvokeExport = ""
const StandaloneParameterTypes = ""
const StandaloneReturnTypes = ""
const StandaloneResultTypes = ^uint8(0)
const StandaloneUnboundFunctionImport = ""
const StandaloneUnboundStorageDiagnostic = ""
const StandaloneUnboundTagImport = ""
const StandaloneStructuralDiagnostic = ""
const StandaloneArenaDiagnostic = ""
const StandaloneArenaBytes = 0
const StandaloneMaxParamSlots = 0
const StandaloneMaxResultSlots = 0
const StandaloneSyncHostSlots = 0

// Builds without a sealed image accept the complete initializer grammar.
const StandaloneGCConstOpcodes = ^uint32(0)
const StandaloneConstExprLowOpcodes = ^uint64(0)
const StandaloneConstExprNumericOpcodes = ^uint64(0)
const StandaloneConstExprReferenceOpcodes = ^uint64(0)
