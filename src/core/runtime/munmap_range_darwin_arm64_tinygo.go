//go:build darwin && arm64 && tinygo

package runtime

import "unsafe"

func munmapRange(base, length uintptr) error {
	return munmap(unsafe.Slice((*byte)(unsafe.Pointer(base)), int(length)))
}
