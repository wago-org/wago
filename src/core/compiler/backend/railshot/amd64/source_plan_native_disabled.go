// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && (!wago_regalloccheck || tinygo || wago_profile)

package amd64

func checkNativeSourceBefore(*fn, int, byte)     {}
func checkNativeSourceAfter(*fn, int)            {}
func checkNativeSourceGet(*fn, uint32)           {}
func checkNativeSourceSet(*fn, int, bool, *elem) {}

func checkNativeSourceALUBefore(*fn, *elem, *elem, *elem) {}
func checkNativeSourceALUAfter(*fn)                       {}
