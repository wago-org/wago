// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package buildfeatures defines the compile-time WebAssembly feature ceiling.
// The bit assignments are part of the public CoreFeatures API.
package buildfeatures

const (
	BulkMemoryOperationsBit            = 1 << 0
	MultiValueBit                      = 1 << 1
	MutableGlobalBit                   = 1 << 2
	NonTrappingFloatToIntConversionBit = 1 << 3
	ReferenceTypesBit                  = 1 << 4
	SignExtensionOpsBit                = 1 << 5
	SIMDBit                            = 1 << 6
	ExtendedConstBit                   = 1 << 7
	TailCallBit                        = 1 << 8
	ExtendedConstExpressionsBit        = 1 << 9
	TypedFunctionReferencesBit         = 1 << 10
	GCBit                              = 1 << 11
	ExceptionHandlingBit               = 1 << 12
	MultiMemoryBit                     = 1 << 13
	Memory64Bit                        = 1 << 14
	Table64Bit                         = 1 << 15
	ThreadsBit                         = 1 << 16
	CompactImportsBit                  = 1 << 17
	WideArithmeticBit                  = 1 << 18
	// RelaxedSIMD is separate for builds while the runtime setting covers both.
	RelaxedSIMDBit = 1 << 19
)

const Enabled = BulkMemoryOperations |
	MultiValue |
	MutableGlobal |
	NonTrappingFloatToIntConversion |
	ReferenceTypes |
	SignExtensionOps |
	SIMD |
	ExtendedConst |
	TailCall |
	ExtendedConstExpressions |
	TypedFunctionReferences |
	GC |
	ExceptionHandling |
	MultiMemory |
	Memory64 |
	Table64 |
	Threads |
	CompactImports |
	WideArithmetic |
	RelaxedSIMD

// ExtendedConstArithmetic enables add, sub, and mul in constant expressions.
// Either the original arithmetic feature or the Core 3 feature provides it.
const ExtendedConstArithmetic = ExtendedConst|ExtendedConstExpressions != 0

// StandaloneConstExprArithmeticOpcodes uses bits relative to opcode 0x40.
const StandaloneConstExprArithmeticOpcodes = StandaloneConstExprNumericOpcodes & (7<<(0x6a-0x40) | 7<<(0x7c-0x40))

// ReferenceSignatures enables value-level references. MVP table descriptors
// and indirect calls also exist in builds without reference signatures.
const ReferenceSignatures = ReferenceTypes|TypedFunctionReferences|GC|ExceptionHandling != 0

// RISCV64Collector is the shared representation for managed Wasm objects,
// compact externrefs, and owned exception references in the RISC-V backend.
const RISCV64Collector = GC != 0 || ReferenceTypes != 0 || ExceptionHandling != 0

// RISCV64CollectorFrameRoots enables exact maps for collecting Wasm frames.
const RISCV64CollectorFrameRoots = GC != 0 || ExceptionHandling != 0

// NativeRISCV64Collector is available when the current target uses the RISC-V
// backend's collector representation.
const NativeRISCV64Collector = nativeRISCV64Target && RISCV64Collector

// NativeCollector reports whether collector or compact-reference infrastructure is built.
const NativeCollector = GC != 0 || NativeRISCV64Collector

// NativeCollectorFrameRoots enables the native heap and exact frame root maps.
const NativeCollectorFrameRoots = GC != 0 || nativeRISCV64Target && RISCV64CollectorFrameRoots

const Custom = Enabled != (RelaxedSIMDBit<<1)-1
