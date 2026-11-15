// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

// UseOwnedThroughputBacking permits stable virtual heap and handle storage
// on supported hosts.
// Call it before the first allocation. The caller must call Close and retain
// the collector while any borrowed byte view or native pointer is in use.
// NewCollector alone retains the ordinary Go-owned backing contract.
// Unsupported hosts and failed reservations keep the ordinary backing path.
func (c *Collector) UseOwnedThroughputBacking() {
	if !c.closed && c.cfg.Profile == ProfileThroughput && len(c.throughput.mem) == 0 {
		c.throughput.ownedBacking = true
	}
}
