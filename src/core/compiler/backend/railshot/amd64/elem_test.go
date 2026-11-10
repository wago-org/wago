// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

func testValueElem(st storage) *elem {
	e := &elem{}
	e.setElemKind(ekValue)
	e.st = st
	return e
}

func testDeferredElem(op wOp, typ machineType, arg0, arg1 *elem) *elem {
	e := &elem{arg0: arg0, arg1: arg1}
	e.setElemKind(ekDeferred)
	e.setDeferredOp(op)
	e.setValueType(typ)
	return e
}
