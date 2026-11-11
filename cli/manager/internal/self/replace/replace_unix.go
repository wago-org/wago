// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !windows

package replace

import "os"

func StageRemoval(executable string, _ []string) (string, error) {
	return executable, nil
}

func Executable(executable, staged string) (bool, error) {
	return false, os.Rename(staged, executable)
}

func Remove(executable string) (bool, error) {
	err := os.Remove(executable)
	if os.IsNotExist(err) {
		err = nil
	}
	return false, err
}

func ScheduleTargetRemoval(_ string, _ []string, _ string, _ []string) (bool, error) {
	return false, nil
}
