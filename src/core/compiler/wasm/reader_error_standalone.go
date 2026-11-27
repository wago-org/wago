// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && wago_standalone_no_plugins && wago_standalone_generated

package wasm

import "errors"

// A generated plugin-free image reads retained metadata and reports diagnostics
// as text. Keep the compiler error type out of TinyGo's interface dispatch.
func readerError(code DecodeErrorCode, offset int) error {
	return errors.New(decodeErrorAt(code, offset))
}
