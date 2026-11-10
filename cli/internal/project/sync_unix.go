// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows

package project

import (
	"errors"
	"os"
)

func syncProjectDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return joinClose(directory.Sync(), directory)
}

func joinClose(primary error, file *os.File) error {
	return errors.Join(primary, file.Close())
}
