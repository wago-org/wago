//go:build windows

package build

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var buildGetSecurityInfo = windows.NewLazySystemDLL("advapi32.dll").NewProc("GetSecurityInfo")

// getBuildSecurityInfo keeps the requested security components unchanged while
// avoiding x/sys's minimum-sized read from a shorter self-relative descriptor.
func getBuildSecurityInfo(handle windows.Handle, objectType windows.SE_OBJECT_TYPE, information windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
	var descriptor *windows.SECURITY_DESCRIPTOR
	code, _, _ := buildGetSecurityInfo.Call(uintptr(handle), uintptr(objectType), uintptr(information),
		0, 0, 0, 0, uintptr(unsafe.Pointer(&descriptor)))
	if code != 0 {
		// GetSecurityInfo returns its error directly, not through GetLastError.
		return nil, syscall.Errno(code)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(descriptor)))
	return copyBuildSecurityDescriptor(descriptor), nil
}

// A self-relative header uses 32-bit offsets even on 64-bit Windows. Its
// reported length can therefore be less than sizeof(SECURITY_DESCRIPTOR).
// Keep the Go destination large and aligned enough for the Go type, but never
// use that destination capacity as the number of bytes read from Windows.
func copyBuildSecurityDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) *windows.SECURITY_DESCRIPTOR {
	if descriptor == nil {
		return nil
	}
	length := int(descriptor.Length())
	capacity := max(length, int(unsafe.Sizeof(windows.SECURITY_DESCRIPTOR{})))
	const pointerSize = int(unsafe.Sizeof(uintptr(0)))
	allocation := make([]uintptr, (capacity+pointerSize-1)/pointerSize)
	destination := unsafe.Slice((*byte)(unsafe.Pointer(&allocation[0])), capacity)
	source := unsafe.Slice((*byte)(unsafe.Pointer(descriptor)), length)
	copy(destination, source)
	runtime.KeepAlive(descriptor)
	return (*windows.SECURITY_DESCRIPTOR)(unsafe.Pointer(&allocation[0]))
}
