// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wagobench

import "github.com/wago-org/wago/src/core/codeimage"

type benchCompiledModule struct {
	Code  []byte
	Entry []int
	image codeimage.Image
}

func (m *benchCompiledModule) Close() error {
	if m == nil {
		return nil
	}
	image := m.image
	m.Code, m.Entry, m.image = nil, nil, nil
	if image != nil {
		return image.Close()
	}
	return nil
}
