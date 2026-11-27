// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_reference_store

package wago

type runtimeReferenceStore = struct{}

func (*Runtime) initReferenceStore() {}

func (*Runtime) references() *referenceStore { return nil }

func (*Runtime) referenceStoreEmptyForInlineClose() bool { return true }

func randomNonzeroUint64() (uint64, error) { return 0, ErrPermissionDenied }

func closeRuntimeReferenceStore(store *referenceStore) error {
	return nil
}
