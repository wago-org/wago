// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package codeimage defines the ownership boundary between native-code
// producers and the runtime that seals and executes their output.
package codeimage

// Image transfers an unsealed machine-code mapping from a producer to its
// runtime owner. Take succeeds once; Close releases an image not yet taken.
type Image interface {
	Take() (mapping []byte, base uintptr, err error)
	Close() error
}
