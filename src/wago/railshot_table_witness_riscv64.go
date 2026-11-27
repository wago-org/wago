// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "github.com/wago-org/wago/internal/buildfeatures"

// rv64TableWitnessError verifies instruction markers on compiled modules.
// Indexed table32 and table64 share the marker values but require distinct
// retained source and layout proofs.
func rv64TableWitnessError(c *Compiled, primitiveReferences bool) string {
	qualifiedTable32 := legacyTable32InstructionsForImage && buildfeatures.ReferenceTypes != 0 && rv64IndexedTable32InstructionWitness(c)
	if c.nativeTable64ABI == nativeTable64SizeABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64Size(c) && !(multiTableForImage && rv64OwnedTwoTable64Size(c))) {
		return "table.size compiler witness requires its exact owned or indexed two-table profile"
	}
	if c.nativeTable64ABI == nativeTable64GrowABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64Grow(c) && !(multiTableForImage && rv64OwnedTwoTable64Grow(c))) {
		return "table.grow compiler witness requires its exact owned or indexed two-table profile"
	}
	if c.nativeTable64ABI == nativeTable64GetSetABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64GetSet(c) && !(multiTableForImage && rv64OwnedTwoTable64GetSet(c))) {
		return "table.get/set compiler witness requires its exact owned or indexed two-table profile"
	}
	if !primitiveReferences && c.nativeTable64ABI == nativeTable64IndirectABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64Indirect(c) && !(multiTableForImage && rv64OwnedTwoTable64Indirect(c)) &&
			!(tableImportsForImage && (rv64ImportedTable64Indirect(c) || multiTableForImage && (rv64MixedTable64Indirect(c) || rv64MixedTable64ActiveIndirect(c))))) {
		return "table indirect-call compiler witness requires its exact owned, indexed two-table32/table64, or imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64FillABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64Fill(c) && !(multiTableForImage && rv64OwnedTwoTable64Fill(c))) {
		return "table.fill compiler witness requires its exact owned or indexed two-table profile"
	}
	if c.nativeTable64ABI == nativeTable64CopyABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64Copy(c) && !(multiTableForImage && rv64OwnedTwoTable64Copy(c))) {
		return "table.copy compiler witness requires its exact owned or indexed two-table profile"
	}
	if c.nativeTable64ABI == nativeTable64InitDropABI && !qualifiedTable32 && (!legacyTable64ForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64OwnedTable64InitDrop(c) && !(multiTableForImage && rv64OwnedTwoTable64InitDrop(c))) {
		return "table.init/drop compiler witness requires its exact owned or indexed two-table profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedSizeABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64Size(c) && !(multiTableForImage && rv64MixedTable64Size(c))) {
		return "table64.size compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedGetSetABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64GetSet(c) && !(multiTableForImage && rv64MixedTable64GetSet(c))) {
		return "table64.get/set compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedGrowABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64Grow(c) && !(multiTableForImage && rv64MixedTable64Grow(c))) {
		return "table64.grow compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedFillABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64Fill(c) && !(multiTableForImage && rv64MixedTable64Fill(c))) {
		return "table64.fill compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedCopyABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64Copy(c) && !(multiTableForImage && (rv64MixedTable64Copy(c) || rv64MixedTable64CrossCopy(c)))) {
		return "table64.copy compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI == nativeTable64ImportedInitDropABI && (!legacyTable64ForImage || !tableImportsForImage || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) ||
		!rv64ImportedTable64InitDrop(c) && !(multiTableForImage && rv64MixedTable64InitDrop(c))) {
		return "table64.init/drop compiler witness requires its exact imported table64 profile"
	}
	if c.nativeTable64ABI > nativeTable64ImportedInitDropABI {
		return "unknown table64 compiler witness"
	}
	if c.requiredFeatures.IsEnabled(CoreFeatureTable64) && !(table64ForImage && rv64QualifiedTable64(c)) {
		return "table64 requires an exact qualified owned-table profile"
	}
	return ""
}

func rv64LegacyIndexedTable64(c *Compiled) bool {
	return rv64OwnedTwoTable64Witness(c) || tableImportsForImage && rv64MixedTable64Witness(c)
}

func rv64OwnedTwoTable64Witness(c *Compiled) bool {
	return rv64OwnedTwoTable64Size(c) ||
		rv64OwnedTwoTable64GetSet(c) ||
		rv64OwnedTwoTable64Grow(c) ||
		rv64OwnedTwoTable64Fill(c) ||
		rv64OwnedTwoTable64Copy(c) ||
		rv64OwnedTwoTable64InitDrop(c) ||
		rv64OwnedTwoTable64Indirect(c)
}

func rv64MixedTable64Witness(c *Compiled) bool {
	return rv64MixedTable64ActiveElement(c) ||
		rv64MixedTable64DualActiveElements(c) ||
		rv64MixedTable64DualActiveVectors(c) ||
		rv64MixedTable64Size(c) ||
		rv64MixedTable64GetSet(c) ||
		rv64MixedTable64Grow(c) ||
		rv64MixedTable64Fill(c) ||
		rv64MixedTable64Copy(c) ||
		rv64MixedTable64CrossCopy(c) ||
		rv64MixedTable64InitDrop(c) ||
		rv64MixedTable64Indirect(c) ||
		rv64MixedTable64ActiveIndirect(c)
}

func rv64LegacyTable64IndirectTailPlanShape(c *Compiled) bool {
	return rv64OwnedTable64IndirectTailPlanShape(c) ||
		multiTableForImage && rv64OwnedTwoTable64IndirectTail(c) ||
		tableImportsForImage && (rv64ImportedTable64IndirectTailPlanShape(c) || multiTableForImage && rv64MixedTable64ActiveIndirectTailPlanShape(c))
}
