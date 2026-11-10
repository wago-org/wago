// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago_test

import (
	"strings"

	wago "github.com/wago-org/wago"
)

func testWagoImports(pairs ...any) *wago.Imports {
	im := wago.NewImports()
	for i := 0; i < len(pairs); i += 2 {
		addWagoImport(im, pairs[i].(string), pairs[i+1])
	}
	return im
}

func addWagoImport(im *wago.Imports, key string, value any) {
	module, name, _ := strings.Cut(key, ".")
	switch value.(type) {
	case *wago.Memory:
		im.Memory(module, name, value.(*wago.Memory))
	case *wago.Table:
		im.Table(module, name, value.(*wago.Table))
	case *wago.Tag:
		im.Tag(module, name, value.(*wago.Tag))
	case *wago.Global, wago.GlobalImport:
		im.Global(module, name, value)
	case *wago.HostFuncRef, *wago.InstanceExport:
		im.Function(module, name, value)
	default:
		im.HostFunc(module, name, value)
	}
}

func testWagoImportMap(pairs ...any) map[string]any {
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}
