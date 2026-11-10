// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package codegen defines the target-selection seam for trusted plugin machine
// code. Concrete lowering interfaces live in architecture packages such as
// codegen/amd64 and codegen/arm64.
package codegen

// Architecture identifies the machine ISA consumed by a lowering.
type Architecture string

const (
	AMD64   Architecture = "amd64"
	ARM64   Architecture = "arm64"
	RISCV64 Architecture = "riscv64"
)

// Lowering is one target-specific plugin code generator. Instruction metadata
// carries exactly one lowering: plugin packages select it in architecture-tagged
// source files instead of combining unrelated ISAs in one registration.
type Lowering interface {
	Architecture() Architecture
}
