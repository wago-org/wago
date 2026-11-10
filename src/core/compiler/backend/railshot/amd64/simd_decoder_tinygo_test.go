// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && tinygo

package amd64

import "testing"

// Instruction decoding is performed by the standard-Go suite using objdump.
// TinyGo runs the same semantic vectors without depending on os/exec support.
func assertSIMDBaseline(_ *testing.T, _ []byte) {}
