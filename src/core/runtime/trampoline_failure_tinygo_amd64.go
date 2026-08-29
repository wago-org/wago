//go:build tinygo && linux && amd64

package runtime

import "unsafe"

func tinyGoSavedLinMem(ctrl uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(ctrl + hcSavedRBX))
}
