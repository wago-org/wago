// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows && !darwin && !linux

package atomicfile

import "os"

type retainedReplaceHandle struct{}

func createRetainedReplacementTemp(destination string, requireExistingParent bool) (*os.File, retainedReplaceHandle, error) {
	file, err := createTempWithParentPolicy(destination, requireExistingParent)
	return file, retainedReplaceHandle{}, err
}

func (retainedReplaceHandle) valid() bool          { return false }
func (retainedReplaceHandle) replace(string) error { return nil }
func (retainedReplaceHandle) remove() error        { return nil }
func (retainedReplaceHandle) close() error         { return nil }
