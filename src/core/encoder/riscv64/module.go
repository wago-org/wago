// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package riscv64

import "github.com/wago-org/wago/src/core/codeimage"

// NativeTailPlan is address-free immutable source/physical frame metadata.
// Imported target owners are attached separately for each instance.
type NativeTailPlan struct {
	Kind, TypeIndex                      uint32
	FunctionIndex, ImportIndex, SourcePC uint32
	FrameBytes, SourceBase, SourceStride int
	ReferenceBase                        int
}

const (
	NativeTailDirectImport uint32 = iota
	NativeTailIndirect
	NativeTailReference
)

type NativeTailModule struct{ Plans []NativeTailPlan }

// CompiledModule carries a production native image and its function entries.
// RV64G emits fixed-width instructions; optional extensions are not selected.
type CompiledModule struct {
	GCDeadInductionLayouts                                     []uint64
	GCI31Inductions                                            []uint64
	NativeReferenceCalls                                       bool
	NativeReferenceControl                                     bool
	NativeReferenceFunctions                                   bool
	RequiresARM64LSE                                           bool
	NativeTail                                                 *NativeTailModule
	Code                                                       []byte
	CodeImage                                                  codeimage.Image
	Entry, InternalEntry                                       []int
	DirectPrepared, DirectPreparedLight, DirectPreparedBounded []uint64
	PreparedIsolatedTables                                     bool
	RequiredAMD64Features                                      uint32
	RequiresBMI2, RequiresAVX2, RequiresAVX512                 bool
	RequiresBitCount                                           uint8
}
