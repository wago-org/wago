// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_profile

package wago

import "github.com/wago-org/wago/internal/jitprofile"

type codeProfileSession = jitprofile.Session
type codeProfileToken = jitprofile.SpanToken

func newCodeProfile(options CodeProfileOptions) *CodeProfile { return jitprofile.New(options) }
