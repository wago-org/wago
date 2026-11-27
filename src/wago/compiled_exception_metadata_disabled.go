// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_exception_handling

package wago

type compiledExceptionTagMetadata struct{}

func (*compiledExceptionTagMetadata) exceptionTags() []compiledTagDef { return nil }

func (*compiledExceptionTagMetadata) exceptionTagExports() map[string]int { return nil }

func (*compiledExceptionTagMetadata) hasExceptionTagMetadata() bool { return false }

func (*compiledExceptionTagMetadata) appendExceptionTag(compiledTagDef) error {
	return checkCompiledFeatures(CoreFeatureExceptionHandling)
}

func (*compiledExceptionTagMetadata) setExceptionTagExport(string, int) error {
	return checkCompiledFeatures(CoreFeatureExceptionHandling)
}

// Keep both empty directory fields in the internal metadata contract.
func (w *compiledWriter) tags(*Compiled) {
	w.uvar(0)
	w.stringIntMap(nil)
}
