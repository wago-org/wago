// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_nativecompare && (!wago_profile || !amd64)

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCaptureCommandRequiresProfileAMD64(t *testing.T) {
	const helper = "WAGO_NATIVE_COMPARE_CAPTURE_CONTROL"
	if os.Getenv(helper) == "1" {
		os.Args = []string{"native-compare", "capture", "unused.wasm", "unused.json"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureCommandRequiresProfileAMD64$")
	cmd.Env = append(os.Environ(), helper+"=1")
	output, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "capture requires an AMD64 build with -tags=wago_profile") {
		t.Fatalf("unsupported capture: error=%v output=%s", err, output)
	}
}
