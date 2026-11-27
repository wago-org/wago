// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Array helpers reuse this storage under the public collector lock.
type gcArrayConstructorScratch struct {
	values           [63]gc.Value
	valuesExtra      []gc.Value
	initializerRoots gc.ArrayInitializerRootScratch
}

func (s *gcArrayConstructorScratch) constructorValues(count uint32) ([]gc.Value, error) {
	if count <= uint32(len(s.values)) {
		return s.values[:count], nil
	}
	if uint64(count) > uint64(maxInt()) {
		return nil, fmt.Errorf("GC constructor value count %d overflows int", count)
	}
	if cap(s.valuesExtra) < int(count) {
		s.valuesExtra = make([]gc.Value, count)
	} else {
		s.valuesExtra = s.valuesExtra[:count]
		clear(s.valuesExtra)
	}
	return s.valuesExtra, nil
}
