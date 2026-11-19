// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package codegen

import "crypto/sha256"

// GCBodyProof binds compiler-only metadata to the exact body that produced it.
// The digest is private: consumers can verify integrity but cannot use its
// value as an optimization selector.
type GCBodyProof struct{ digest [32]byte }

func NewGCBodyProof(body []byte) GCBodyProof {
	return GCBodyProof{digest: sha256.Sum256(body)}
}

func (p GCBodyProof) Matches(body []byte) bool {
	return p.digest == sha256.Sum256(body)
}
