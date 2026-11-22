// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && (arm64 || amd64) && tinygo

package runtime

import (
	native "runtime"
	"unsafe"
)

type scalarLeafELFHeader struct {
	identity                       [16]byte
	kind, machine                  uint16
	version                        uint32
	entry, programOffset, sections uint64
	flags                          uint32
	headerSize, programSize        uint16
	programCount                   uint16
}

type scalarLeafELFProgram struct {
	kind, flags                     uint32
	offset, virtual, physical       uint64
	fileSize, memorySize, alignment uint64
}

// TinyGo's Linux runtime also uses this linker symbol to locate its image.
//
//go:extern __ehdr_start
var scalarLeafMainImage scalarLeafELFHeader

// This verifier only reads the loaded main ELF image. A fixed-address ELF64
// executable has no load bias, so program virtual addresses are usable directly.
func tinygoScalarLeafText(entry uintptr) []byte {
	header := &scalarLeafMainImage
	const executable = 2
	machine := uint16(62)
	if native.GOARCH == "arm64" {
		machine = 183
	}
	if [4]byte(header.identity[:4]) != [4]byte{0x7f, 'E', 'L', 'F'} ||
		header.identity[4] != 2 || header.identity[5] != 1 ||
		header.kind != executable || header.machine != machine ||
		header.programSize != uint16(unsafe.Sizeof(scalarLeafELFProgram{})) {
		return nil
	}
	programs := uintptr(unsafe.Pointer(header)) + uintptr(header.programOffset)
	for index := uintptr(0); index < uintptr(header.programCount); index++ {
		program := (*scalarLeafELFProgram)(offHeapPointer(programs + index*uintptr(header.programSize)))
		const load, executableReadable = 1, 5
		start := uintptr(program.virtual)
		size := min(uintptr(program.fileSize), uintptr(program.memorySize))
		if program.kind == load && program.flags&7 == executableReadable &&
			entry >= start && entry-start < size {
			const maxLeafBytes = 128
			return unsafe.Slice((*byte)(offHeapPointer(entry)), min(uintptr(maxLeafBytes), size-(entry-start)))
		}
	}
	return nil
}
