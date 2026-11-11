// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build riscv64

package wago

import (
	"context"
	"sync"
)

// A foreign RV64 callback owns a fresh scope because concurrent public roots
// can park in one producer. Its parent is immutable from publication to expiry;
// it must not be recovered from that producer's shared canonical control word.
// Expiry clears the retained parent under its own lock.
type railshotHostScopeParent struct {
	parentMu sync.Mutex
	parent   context.Context
	bound    bool
}

func (s *hostCallScope) invocationParent(fallback context.Context) context.Context {
	if s.bound {
		s.parentMu.Lock()
		parent := s.parent
		s.parentMu.Unlock()
		return parent
	}
	return fallback
}

func (s *hostCallScope) releaseInvocationParent() {
	if s.bound {
		s.parentMu.Lock()
		s.parent = nil
		s.parentMu.Unlock()
	}
}
