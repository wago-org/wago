// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone || !wago_standalone_no_plugins || !wago_standalone_generated || !wago_standalone_one_shot

package wago

const standaloneOneShot = false
const standaloneInvokeFunctionIndex = -1
const standaloneInvokeExport = ""

type inlineInvokeCache [4]invokeCache
