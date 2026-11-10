// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows

package managedrelease

import (
	"errors"
	"syscall"
	"testing"
)

func TestRetryWindowsFilesystemOperationRetriesSharingViolations(t *testing.T) {
	attempts := 0
	err := retryWindowsFilesystemOperation(func() error {
		attempts++
		if attempts < 3 {
			return syscall.ERROR_ACCESS_DENIED
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("retry result: attempts=%d, err=%v", attempts, err)
	}

	want := errors.New("permanent failure")
	attempts = 0
	err = retryWindowsFilesystemOperation(func() error {
		attempts++
		return want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("non-retryable result: attempts=%d, err=%v", attempts, err)
	}
}
