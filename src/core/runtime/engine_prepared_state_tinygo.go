// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

type tinygoPreparedIntState struct {
	mem      []byte
	stackTop uintptr
}

func (state *tinygoPreparedIntState) close() error {
	if err := munmap(state.mem); err != nil {
		return err
	}
	state.mem = nil
	state.stackTop = 0
	return nil
}
