// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && tinygo

package runtime

import (
	"reflect"
	"unsafe"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

const scalarLeafArg0, scalarLeafArg1 = x64.RDI, x64.RSI

func tinygoScalarLeafEntry(fn any, params uint32) uintptr {
	var value funcValue
	switch typed := fn.(type) {
	case func(int32) int32:
		if params != 1 {
			return 0
		}
		value = *(*funcValue)(unsafe.Pointer(&typed))
	case func(int32, int32) int32:
		if params != 2 {
			return 0
		}
		value = *(*funcValue)(unsafe.Pointer(&typed))
	default:
		return 0
	}
	// Check before consulting the shared cache. Its key omits closure state.
	if value.context != 0 {
		return 0
	}
	return value.fnptr
}

func scalarLeafCanRegister(fn any, params uint32) bool {
	entry := tinygoScalarLeafEntry(fn, params)
	// The shared cache keys on reflect.Value.Pointer. Require it to name
	// the same address checked by the TinyGo function-value decoder.
	return entry != 0 && reflect.ValueOf(fn).Pointer() == entry
}

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
	const executable, amd64Machine = 2, 62
	if [4]byte(header.identity[:4]) != [4]byte{0x7f, 'E', 'L', 'F'} ||
		header.identity[4] != 2 || header.identity[5] != 1 ||
		header.kind != executable || header.machine != amd64Machine ||
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

func scalarLeafBody(fn any, params uint32) []byte {
	entry := tinygoScalarLeafEntry(fn, params)
	if entry == 0 {
		return nil
	}
	text := tinygoScalarLeafText(entry)
	defined := scalarLeafDefinedArgs(params)
	for offset := 0; offset < len(text); {
		if text[offset] == 0xc3 && defined&(1<<x64.RAX) != 0 {
			return text[:offset]
		}
		size, ok := scalarLeafInstruction(text[offset:], &defined)
		if !ok {
			return nil
		}
		offset += size
	}
	return nil
}
