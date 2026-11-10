// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !(linux || (darwin && arm64)) || riscv64 || tinygo || wago_target_tinygo

package wago

func registerConversionHost(*referenceStore, []byte, *tableOwner, *globalOwner) {}
func lockConversionHostTableClose(*Table) func()                                { return func() {} }
func lockConversionHostGlobalClose(*Global) func()                              { return func() {} }
func releaseConversionHostStorage(*referenceStore, []byte)                      {}
func conversionHostPublicWord(*referenceStore, uint64) (uint64, bool)           { return 0, false }
func conversionHostRootIsStrong(*gcConversionContainer) bool                    { return true }
