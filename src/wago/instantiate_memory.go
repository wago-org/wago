// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime"
)

// The caller owns the primary memory and the rollback for each published slot.
// Record ownership before sharing a new backing so any later error can unwind it.
func (b *instanceBuilder) instantiateSecondaryMemories(memories []*Memory, owned []bool, attach func(*Memory) error) error {
	c := b.c
	for i := 1; i < len(memories); i++ {
		def := c.memoryDef(i)
		if memoryImportsForImage && def.ImportKey != "" {
			memory, ok := b.imports.memory(c.memoryImportBindingKey(i))
			if !ok {
				return fmt.Errorf("missing imported memory %q", def.ImportKey)
			}
			if err := memory.validateLimits(def.Min, def.Max, def.HasMax, def.Addr64, def.Shared); err != nil {
				return fmt.Errorf("imported memory %q limits: %w", def.ImportKey, err)
			}
			if err := attach(memory); err != nil {
				return fmt.Errorf("imported memory %q: %w", def.ImportKey, err)
			}
			memories[i] = memory
			continue
		}
		maxPages, reservationErr := ownedMemoryReservationPages(def)
		if reservationErr != nil {
			return fmt.Errorf("memory %d: %w", i, reservationErr)
		}
		initialPages := def.Min
		var secondaryJM *runtime.JobMemory
		var allocErr error
		if guardedMemoryForImage && (c.boundsMode == BoundsChecksSignalsBased || c.prefersGuardMemory()) {
			// Every owned memory requested by a signals-based configuration uses the guarded
			// representation, even though indexed-memory accesses retain explicit
			// checks. An exported nonzero memory may become memory 0 of another
			// signals-based instance, where guard-backed ownership is mandatory.
			secondaryJM, allocErr = newGuardedJobMemory(int(initialPages)*65536, int(maxPages)*65536)
		} else {
			secondaryJM, allocErr = runtime.AcquireJobMemoryGrowable(int(initialPages)*65536, int(maxPages)*65536)
		}
		if allocErr != nil {
			return fmt.Errorf("memory %d: %w", i, allocErr)
		}
		memories[i] = &Memory{jm: secondaryJM}
		owned[i] = true
		if sharedMemoryRuntimeBuilt && def.Shared {
			if err := memories[i].share(nil, def); err != nil {
				return fmt.Errorf("shared memory %d: %w", i, err)
			}
		}
	}
	return nil
}
