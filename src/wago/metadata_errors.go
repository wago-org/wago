// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

// Share contextual error formatting so compiler and loader validation exits do
// not each carry their own variadic formatting setup. This only runs on errors.
//
//go:noinline
func wrapContextError(context string, err error) error {
	return fmt.Errorf("%s: %w", context, err)
}
