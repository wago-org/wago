// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin && arm64 && tinygo

package runtime

import "unsafe"

type scalarLeafMachHeader struct {
	magic, cpu, cpuSubtype, fileType        uint32
	commands, commandsSize, flags, reserved uint32
}

type scalarLeafMachCommand struct {
	kind, size uint32
}

type scalarLeafMachSegment struct {
	scalarLeafMachCommand
	name                                              [16]byte
	address, size, fileOffset, fileSize               uint64
	maxProtection, initialProtection, sections, flags uint32
}

//go:extern _mh_execute_header
var scalarLeafMainImage scalarLeafMachHeader

// The executable's Mach-O header lives at file offset zero in __TEXT.
// Its loaded address includes the slide applied by dyld.
func tinygoScalarLeafText(entry uintptr) []byte {
	header := &scalarLeafMainImage
	if header.magic != 0xfeedfacf || header.cpu != 0x0100000c ||
		header.fileType != 2 || header.commandsSize > 1<<20 {
		return nil
	}
	start := uintptr(unsafe.Pointer(header))
	next := start + unsafe.Sizeof(*header)
	end := next + uintptr(header.commandsSize)
	for i := uint32(0); i < header.commands && end-next >= unsafe.Sizeof(scalarLeafMachCommand{}); i++ {
		command := (*scalarLeafMachCommand)(offHeapPointer(next))
		size := uintptr(command.size)
		if size < unsafe.Sizeof(*command) || size > end-next {
			return nil
		}
		if command.kind == 0x19 && size >= unsafe.Sizeof(scalarLeafMachSegment{}) { // LC_SEGMENT_64
			text := (*scalarLeafMachSegment)(offHeapPointer(next))
			const readableAndExecutable = 5
			if text.name == ([16]byte{'_', '_', 'T', 'E', 'X', 'T'}) &&
				text.fileOffset == 0 && text.initialProtection&7 == readableAndExecutable {
				limit := min(uintptr(text.size), uintptr(text.fileSize))
				if entry >= start && entry-start < limit {
					const maxLeafBytes = 128
					return unsafe.Slice((*byte)(offHeapPointer(entry)), min(uintptr(maxLeafBytes), limit-(entry-start)))
				}
				return nil
			}
		}
		next += size
	}
	return nil
}
