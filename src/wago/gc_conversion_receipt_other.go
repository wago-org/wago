// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !(linux || (darwin && arm64)) || riscv64 || tinygo || wago_target_tinygo

package wago

type gcConversionIdentity struct{}
type gcConversionCore struct{ owner *Instance }
type gcConversionContainer struct{}

func bindGCConversionCore(in *Instance) error {
	if s := in.existingGCExternConversionState(); s != nil {
		s.core = &gcConversionCore{owner: in}
	}
	return nil
}
func closeGCConversionCoreLocked(s *gcExternConversionState) { s.core = nil }
func publishGCConversionTable(s *gcRefTestTableState, table, index, old, word uint64, publish func() error) error {
	// The legacy transaction is sufficient for a table physically owned by
	// its publisher. Imported storage must never rely on that publisher's root.
	if core := s.Conversion.core; core != nil && table < uint64(core.owner.c.tableImportCount()) {
		if err := unsupportedConversionWord(s.Conversion.store, word); err != nil {
			return err
		}
	}
	return s.Conversion.publishExternReplacement(old, word, publish)
}
func releaseGCConversionTables(*gcRefTestTableState)                {}
func gcConversionCollectorRegistered(*gcExternConversionState) bool { return true }
func releaseGCConversionOwner(*Instance)                            {}
func lockGCConversionRetirement(*Instance) func()                   { return func() {} }
func retireGCConversionStoreLocked(*referenceStore)                 {}
