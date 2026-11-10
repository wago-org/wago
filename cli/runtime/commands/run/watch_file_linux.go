// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !wago_lean

package run

import (
	"fmt"
	"os"
	"syscall"
)

func openWatchedFile(path string) (*os.File, error) { return os.Open(path) }

func metadataForWatchedFile(_ *os.File, info os.FileInfo) (watchedFileMetadata, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return watchedFileMetadata{}, fmt.Errorf("read Linux module metadata")
	}
	return watchedFileMetadata{
		size:     info.Size(),
		modified: info.ModTime().UnixNano(),
		change:   [4]uint64{uint64(stat.Dev), stat.Ino, uint64(stat.Ctim.Sec), uint64(stat.Ctim.Nsec)},
	}, nil
}
