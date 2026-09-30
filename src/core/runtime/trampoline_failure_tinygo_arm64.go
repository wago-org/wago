//go:build tinygo && (linux || darwin) && arm64

package runtime

import "unsafe"

func tinyGoSavedLinMem(ctrl uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(ctrl + hcSavedLinMem))
}
