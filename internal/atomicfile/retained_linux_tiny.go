// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "os"

// The TinyGo minimal runtime registers only the run command, and every linked
// atomicfile caller uses ordinary replacement. Keep an unexpected build-only
// retention request fail-closed without linking the much larger private-stage
// implementation into this size-constrained profile.
type retainedReplaceHandle struct{}

func createRetainedReplacementTemp(string, bool) (*os.File, retainedReplaceHandle, error) {
	return nil, retainedReplaceHandle{}, newError("retained atomic staging is unavailable")
}

func (retainedReplaceHandle) valid() bool          { return false }
func (retainedReplaceHandle) replace(string) error { return newError("missing retained replacement") }
func (retainedReplaceHandle) remove() error        { return nil }
func (retainedReplaceHandle) close() error         { return nil }
