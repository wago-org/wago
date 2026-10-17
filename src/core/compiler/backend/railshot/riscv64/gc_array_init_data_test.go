//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
)

func canonicalSectionSizes(source []byte) []byte {
	canonical := append([]byte(nil), source[:8]...)
	for offset := 8; offset < len(source); {
		id := source[offset]
		offset++
		var size uint32
		for shift := uint(0); ; shift += 7 {
			b := source[offset]
			offset++
			size |= uint32(b&0x7f) << shift
			if b&0x80 == 0 {
				break
			}
		}
		canonical = append(canonical, id)
		canonical = binary.AppendUvarint(canonical, uint64(size))
		canonical = append(canonical, source[offset:offset+int(size)]...)
		offset += int(size)
	}
	return canonical
}
