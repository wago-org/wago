// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_exception_handling

package wago

import "fmt"

type instanceExceptionTagState struct{}

type tagImportAttachments struct{}

func (*tagImportAttachments) recordAttached(*Tag) {}

func (*Instance) installNativeTagDirectory(uintptr) {}

func (*instanceBuilder) attachTagImports() error { return nil }

func (*instanceBuilder) rollbackTagImports(*resourceCleanup) {}

func detachImportedTags(*Instance) {}

// ExportedTag returns the tag exported under name, when available.
func (in *Instance) ExportedTag(name string) (*Tag, error) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return nil, fmt.Errorf("instance has no exported tag %q", name)
	}
	return nil, fmt.Errorf("no exported tag %q", name)
}
