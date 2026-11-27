// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_gc && (!wago_standalone || !wago_standalone_no_plugins)

package wago

type pluginGCImportState struct {
	imports map[uint32]struct{}
}

func (s *instanceImportState) gcImports() map[uint32]struct{} { return s.imports }

func (s *instanceImportState) setGCImports(imports map[uint32]struct{}) {
	s.imports = imports
}
