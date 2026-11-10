// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

// Shared guard growth can extend JobMemory's Go slice after it commits pages.
// Hold the same lifetime lock while reading that host view. Native accesses use
// the immutable reservation base and the separately published byte-size header.
func threadedMemoryHostBytes(m *Memory) []byte {
	if m == nil {
		return nil
	}
	s := m.state.Load()
	if s == nil {
		if m.jm == nil {
			return nil
		}
		return m.jm.HostBytes()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has(memoryStateClosed) || m.jm == nil {
		return nil
	}
	return m.jm.HostBytes()
}
