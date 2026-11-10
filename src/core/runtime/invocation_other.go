// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 || arm64

package runtime

func (*Engine) prepareNativeInvocation([]byte, []byte) {}
func (*Engine) resetNativeInvocation()                 {}

// TinyGo stores its generated entry address in e.stackTop. Resume needs the
// physical foreign-stack top instead, which remains derivable from e.stack.
func (e *Engine) nativeResumeStackTop() uintptr { return e.StackTop() }
