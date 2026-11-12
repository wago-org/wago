// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package codeimage

import "crypto/sha256"

// IntegrityDigest authenticates the immutable logical span of one emitted
// native-code object. It is code-ownership metadata, never a workload selector.
type IntegrityDigest [sha256.Size]byte

// Digest computes the integrity identity of emitted native code.
func Digest(code []byte) IntegrityDigest { return sha256.Sum256(code) }
