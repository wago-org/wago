//go:build windows

package build

import (
	"bytes"
	"encoding/binary"
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	jit "github.com/wago-org/wago/src/core/runtime"
	"golang.org/x/sys/windows"
)

// Keep short test descriptors: padding their SDDL would hide the overread that
// also affects filtered production queries with no matching SACL components.
func buildSecurityDescriptorFromString(sddl string) (*windows.SECURITY_DESCRIPTOR, error) {
	text, err := windows.UTF16PtrFromString(sddl)
	if err != nil {
		return nil, err
	}
	var descriptor *windows.SECURITY_DESCRIPTOR
	convert := windows.NewLazySystemDLL("advapi32.dll").NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	ok, _, callErr := convert.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	if ok == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return nil, callErr
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(descriptor)))
	return copyBuildSecurityDescriptor(descriptor), nil
}

func TestCopyBuildSecurityDescriptorStopsAtGuardPage(t *testing.T) {
	for _, owner := range []bool{false, true} {
		name, length := "empty", 20
		if owner {
			name, length = "owner", 32
		}
		t.Run(name, func(t *testing.T) {
			pageSize := os.Getpagesize()
			arena, err := jit.NewArena(2 * pageSize)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if arena != nil {
					if err := arena.Close(); err != nil {
						t.Error(err)
					}
				}
			})
			memory := arena.Alloc(2 * pageSize)
			var oldProtection uint32
			if err := windows.VirtualProtect(uintptr(unsafe.Pointer(&memory[pageSize])), uintptr(pageSize), windows.PAGE_NOACCESS, &oldProtection); err != nil {
				t.Fatal(err)
			}
			// Put the last valid descriptor byte immediately before PAGE_NOACCESS.
			// A sizeof-based source copy reads past it and faults on 64-bit Windows.
			source := memory[pageSize-length : pageSize]
			source[0] = 1 // SECURITY_DESCRIPTOR_REVISION
			binary.LittleEndian.PutUint16(source[2:4], uint16(windows.SE_SELF_RELATIVE))
			if owner {
				binary.LittleEndian.PutUint32(source[4:8], 20)
				copy(source[20:], []byte{1, 1, 0, 0, 0, 0, 0, 5, 18, 0, 0, 0}) // S-1-5-18
			}
			want := append([]byte(nil), source...)
			got := copyBuildSecurityDescriptor((*windows.SECURITY_DESCRIPTOR)(unsafe.Pointer(&source[0])))
			if err := arena.Close(); err != nil {
				t.Fatal(err)
			}
			arena = nil
			runtime.GC() // Only the returned Go allocation may own these bytes now.
			if uintptr(unsafe.Pointer(got))%unsafe.Alignof(windows.SECURITY_DESCRIPTOR{}) != 0 {
				t.Fatal("copied descriptor is not pointer-aligned")
			}
			if got.Length() != uint32(length) || !got.IsValid() {
				t.Fatalf("copied descriptor length=%d valid=%v", got.Length(), got.IsValid())
			}
			copied := unsafe.Slice((*byte)(unsafe.Pointer(got)), max(length, int(unsafe.Sizeof(*got))))
			if !bytes.Equal(copied[:length], want) {
				t.Fatalf("descriptor bytes=%x, want %x", copied[:length], want)
			}
			for _, padding := range copied[length:] {
				if padding != 0 {
					t.Fatal("descriptor destination padding is not zero")
				}
			}
			runtime.KeepAlive(got)
		})
	}
}

func TestGetBuildSecurityInfoPreservesError(t *testing.T) {
	// InvalidHandle is also the current-process pseudo handle, so Windows can
	// accept it. A null handle cannot name an object whose owner or DACL is read.
	const information = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION
	_, want := windows.GetSecurityInfo(0, windows.SE_FILE_OBJECT, information)
	if want != windows.ERROR_INVALID_HANDLE {
		t.Fatalf("reference null handle error=%v, want %v", want, windows.ERROR_INVALID_HANDLE)
	}
	descriptor, got := getBuildSecurityInfo(0, windows.SE_FILE_OBJECT, information)
	if got != want {
		t.Fatalf("null handle error=%v, want %v", got, want)
	}
	if descriptor != nil {
		t.Fatal("failed security query returned a descriptor")
	}
}
