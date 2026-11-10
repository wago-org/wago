// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin

package sourcearchive

import "golang.org/x/sys/unix"

func publishDirectoryNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
