// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Command wagoprof is the standalone profiling workflow.
package main

import (
	"fmt"
	"github.com/wago-org/wago/internal/profilecmd"
	"os"
)

func main() {
	if err := profilecmd.Run(os.Args[1:], nil); err != nil {
		fmt.Fprintln(os.Stderr, "wagoprof:", err)
		os.Exit(1)
	}
}
