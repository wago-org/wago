// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64

package wago

// The leading empty cache field preserves existing-target cache footprints.
type railshotHostThunkConstruction struct{}

func railshotInitHostThunkConstruction(*railshotHostThunkConstruction, *Compiled) {}
func railshotCertifiedHostThunk(_ *railshotHostThunkConstruction, _ int, dispatch uint32, params, results int, owned bool) ([]byte, error) {
	if owned {
		return railshotHostIndirectOwnedSyncThunk(dispatch, params, results), nil
	}
	return railshotHostIndirectSyncThunk(dispatch, params, results), nil
}
func railshotOwnedHostThunk(_ *railshotNativeImportConstruction, _ int, dispatch uint32, params, results int) ([]byte, error) {
	return railshotHostIndirectOwnedSyncThunk(dispatch, params, results), nil
}
func railshotRecordHostThunkSpan(*railshotHostThunkConstruction, int, int)    {}
func railshotFinishHostThunkConstruction(*railshotHostThunkConstruction, int) {}
