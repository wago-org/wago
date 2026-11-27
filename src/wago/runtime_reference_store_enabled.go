// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store

package wago

import (
	"crypto/rand"
	"encoding/binary"
)

type runtimeReferenceStore struct {
	refStore *referenceStore
}

func (rt *Runtime) initReferenceStore() {
	rt.refStore = newReferenceStore(false)
}

func (rt *Runtime) references() *referenceStore { return rt.refStore }

func (rt *Runtime) referenceStoreEmptyForInlineClose() bool {
	store := rt.references()
	return store == nil || store.emptyForInlineClose()
}

func randomNonzeroUint64() (uint64, error) {
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		if token := binary.LittleEndian.Uint64(buf[:]); token != 0 {
			return token, nil
		}
	}
}

func closeRuntimeReferenceStore(store *referenceStore) error {
	return callShutdownSafely("reference store close", store.closeRuntime)
}
