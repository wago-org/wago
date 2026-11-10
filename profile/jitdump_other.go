// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || tinygo

package profile

import "fmt"

type JITFile struct{ *JITDump }

func OpenJITDump(string) (*JITFile, error) {
	return nil, fmt.Errorf("native jitdump capture requires standard Go on Linux")
}
func (f *JITFile) Close() error { return nil }

func (f *JITFile) RefreshMarker() error { return fmt.Errorf("jitdump discovery requires Linux") }
