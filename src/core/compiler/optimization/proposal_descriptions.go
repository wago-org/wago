// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package optimization

import "github.com/wago-org/wago/internal/buildfeatures"

// Catalog identities and defaults are portable because projects share settings
// across builds. Descriptions belong to the implementation that uses them.
type proposalDescription struct {
	name, label, description string
}

func addProposalDescriptions(definitions []Definition) {
	if buildfeatures.SIMD != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"simd-superopt", "SIMD superoptimization", "recognize bounded multi-operation SIMD sequences"},
			proposalDescription{"simd-reduction-borrow", "Read-only vector reductions", "read pinned vector inputs without copying them for bitmasks and any-true reductions"},
			proposalDescription{"avx512-ternary", "AVX-512 ternary SIMD", "fuse adjacent 128-bit vector boolean operations on AVX-512VL hosts"},
			proposalDescription{"avx512-vrotate", "AVX-512 vector rotates", "select packed rotate instructions for exact i32x4 rotate idioms on AVX-512VL hosts"},
			proposalDescription{"simd-store-load-address", "Adjacent SIMD memory addresses", "reuse an identical pure address expression for a vector store and following real load"},
			proposalDescription{"simd-load-extract", "SIMD load lane extraction", "load the consumed scalar lane while retaining the original vector access bounds"},
			proposalDescription{"v128-direct-results", "Direct vector results", "write vector results directly into eligible pinned locals"},
			proposalDescription{"v128-pins", "Vector pins", "pin hot vector locals in registers"},
		)
	}
	if buildfeatures.BulkMemoryOperations != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"memcopy-tail4", "Four-byte memory-copy tails", "finish small dynamic memory copies with one four-byte tail chunk"},
			proposalDescription{"memcopy-qpairs", "Paired vector memory-copy", "copy 32-byte groups with paired 128-bit loads and stores"},
			proposalDescription{"bulk-fill-vector", "Constant vector fills", "broadcast a byte into native vector stores for 16 to 64 byte constant fills"},
			proposalDescription{"bulk-copy-vector", "Constant vector copies", "load complete 16 to 64 byte source spans before overlap-safe vector stores"},
			proposalDescription{"bulk-memory-loops", "Wide bulk-memory loops", "align bulk stores and reduce loop overhead with architecture-specific wide groups"},
			proposalDescription{"bulk-fill-copy-forward", "Filled-source copies", "reuse a proven constant fill for an adjacent unshared copy while retaining both range checks"},
		)
	}
	if buildfeatures.Threads != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"atomic-notify-empty", "Empty atomic notifications", "bypass the Go notification helper when shared backing has no queued waiters"},
			proposalDescription{"atomic-wait-zero-timeout", "Immediate atomic waits", "resolve literal zero-timeout shared-memory waits in native code"},
			proposalDescription{"shared-memory-base", "Shared memory backing cache", "reuse stable shared backing addresses while reading live extents"},
			proposalDescription{"atomic-lse", "LSE atomics", "use native acquire/release atomic RMW and compare-exchange on LSE hosts"},
			proposalDescription{"atomic-static-bounds", "Constant atomic bounds", "prove aligned constant atomic addresses against the declared memory minimum"},
		)
	}
	if buildfeatures.TailCall != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"tail-self-loop", "Self-tail loops", "reuse the native frame for eligible direct self-tail transfers"},
			proposalDescription{"tail-loop-inline", "Tail-loop inlining", "inline bounded numeric tail-call bodies as call-free native loops"},
		)
	}
	if buildfeatures.GC != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"dead-gc-new", "Dead GC constructors", "remove dropped GC constructor trees while preserving traps"},
			proposalDescription{"gc-native-alloc", "Native GC allocation", "allocate admitted GC objects through native nursery fast paths"},
		)
	}
	if buildfeatures.ReferenceTypes != 0 {
		setProposalDescriptions(definitions,
			proposalDescription{"direct-reference-abi", "Direct reference register ABI", "pass bounded ordinary local reference signatures through registers"},
		)
	}
}

func setProposalDescriptions(definitions []Definition, descriptions ...proposalDescription) {
	for _, description := range descriptions {
		for index := range definitions {
			entry := &definitions[index]
			if entry.Name == description.name {
				entry.Label = description.label
				entry.Description = description.description
				break
			}
		}
	}
}
