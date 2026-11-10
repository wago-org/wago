// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import a64 "github.com/wago-org/wago/src/core/encoder/arm64"

// managedPluginARM64Context narrows the dynamic method set without allocating
// a second context object.
type managedPluginARM64Context pluginARM64Context

func (c *managedPluginARM64Context) full() *pluginARM64Context       { return (*pluginARM64Context)(c) }
func (c *managedPluginARM64Context) InputI32(i int) (a64.Reg, error) { return c.full().InputI32(i) }
func (c *managedPluginARM64Context) InputCustom(i int) ([]a64.Reg, error) {
	return c.full().InputCustom(i)
}
func (c *managedPluginARM64Context) Release(r a64.Reg)         { c.full().Release(r) }
func (c *managedPluginARM64Context) ReleaseGP(r a64.Reg)       { c.full().ReleaseGP(r) }
func (c *managedPluginARM64Context) ReleaseVector(r a64.Reg)   { c.full().ReleaseVector(r) }
func (c *managedPluginARM64Context) OutputI32(r a64.Reg) error { return c.full().OutputI32(r) }
func (c *managedPluginARM64Context) OutputCustom(regs ...a64.Reg) error {
	return c.full().OutputCustom(regs...)
}
