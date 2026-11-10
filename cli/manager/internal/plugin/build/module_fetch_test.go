// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package build

import (
	"errors"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	err := &FetchError{
		Err:    errors.New("exit status 1"),
		Output: "remote: Repository not found.\nfatal: repository not found",
	}
	if !IsNotFound(err) {
		t.Fatal("repository-not-found response was not classified")
	}
	if IsNotFound(errors.New("network unavailable")) {
		t.Fatal("generic error was classified as not found")
	}
}
