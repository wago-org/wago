// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && !wago_standalone

package riscv64

func (f *tailRAFunction) publishCode() error {
	if err := f.validateCode(); err != nil {
		return err
	}
	if f.published != nil {
		return nil // Publication belongs to the borrowed runtime owner.
	}
	return f.code.Seal()
}
