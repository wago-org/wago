// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package shared

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type SourceAttempt struct{}

func PrepareSourceWorker(*ScalarState, int, int) {}

func SetSourceContext(*ScalarState, *codegen.SourceContext)                 {}
func SetSourceWorkerContext(*ScalarState, *codegen.SourceContext, int, int) {}
func BeginSourceAttempt(*ScalarState, *wasm.Module, int) *SourceAttempt     { return nil }
func EndSourceAttempt(*SourceAttempt)                                       {}
func finishSourceWorker(*ScalarState)                                       {}
