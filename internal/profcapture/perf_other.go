// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux

package profcapture

import "fmt"

func control(string, string, string) error { return fmt.Errorf("perf collection requires Linux") }
func RecordPerf(Options, []string) error {
	return fmt.Errorf("perf collection requires Linux; use none for metadata or pprof for Go CPU sampling")
}
