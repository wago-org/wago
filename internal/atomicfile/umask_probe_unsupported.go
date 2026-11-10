// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux && !darwin && !windows

package atomicfile

import (
	"errors"
	"io/fs"
)

func probeUmaskMode(string, fs.FileMode, bool) (fs.FileMode, error) {
	return 0, errors.New("secure atomic umask probing is unsupported on this platform")
}
