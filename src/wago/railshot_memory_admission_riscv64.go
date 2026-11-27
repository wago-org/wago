// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"

func rv64DataBudgetError(c *Compiled) string {
	if activeDataForImage && uint64(c.activeDataCount()) > uint64(railshot.MaxPublicCodeBytes)/16 || passiveDataForImage && uint64(len(c.PassiveData)) > uint64(railshot.MaxPublicCodeBytes)/16 {
		return "data descriptor count bound"
	}
	remaining := railshot.MaxModuleBytes
	for i := 0; activeDataForImage && i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if len(data.Bytes) > remaining {
			return "data payload size bound"
		}
		remaining -= len(data.Bytes)
	}
	for i := 0; passiveDataForImage && i < len(c.PassiveData); i++ {
		data := c.PassiveData[i]
		if len(data.Bytes) > remaining {
			return "data payload size bound"
		}
		remaining -= len(data.Bytes)
	}
	return ""
}

func rv64DataInitializerError(c *Compiled) string {
	memories := c.memoryCount()
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if uint64(data.MemoryIndex) >= uint64(memories) {
			return "data initializer memory index"
		}
		offsetType := ValI32
		if c.memoryDef(int(data.MemoryIndex)).Addr64 {
			offsetType = ValI64
			if data.Offset.HasGlobal {
				return "memory64 global offset requires an i64 expression"
			}
		}
		if len(data.Offset.Expr) != 0 {
			if data.Offset.HasGlobal {
				return "conflicting data initializer"
			}
			if err := rv64ValidateIntegerInitializer(c, data.Offset.Expr, offsetType, len(c.Globals)); err != nil {
				return err.Error()
			}
		}
		if data.Offset.HasGlobal && !rv64InitializerGlobal(c, data.Offset.Global, ValI32, len(c.Globals)) {
			return "data initializer lowering is pending"
		}
	}
	return ""
}

func rv64MemoryDeclarationError(c *Compiled) string {
	memories := c.memoryCount()
	if memories > 1 && !c.requiredFeatures.IsEnabled(CoreFeatureMultiMemory) {
		return "memory directory requires multi-memory feature metadata"
	}
	for i := 0; i < memories; i++ {
		memory := c.memoryDef(i)
		maxTypePages := uint64(65536)
		if memory.Addr64 {
			maxTypePages = 1 << 48
			if !c.requiredFeatures.IsEnabled(CoreFeatureMemory64) {
				return "memory64 requires feature metadata"
			}
		}
		reservation, err := ownedMemoryReservationPages(memory)
		if memory.Shared && !rv64ThreadedMemoryProfile(c) || err != nil || (memory.HasMax && memory.Max > maxTypePages) {
			return "bounded unshared memory required"
		}
		if i == 0 && (!c.HasMemory || uint64(c.MemMinPages) != memory.Min || uint64(c.MemMaxPages) < memory.Min || uint64(c.MemMaxPages) > reservation) {
			return "primary memory reservation metadata mismatch"
		}
	}
	if len(c.memoryExportMap()) > railshot.MaxModuleExports {
		return "memory export count bound"
	}
	for _, index := range c.memoryExportMap() {
		if index < 0 || index >= memories {
			return "memory export index bound"
		}
	}
	return ""
}
