//go:build windows && (amd64 || arm64)

package runtime

import "unsafe"

// closeOwnedMapping drops the owner's GC-scanned address before VirtualFree can
// make that range available to the Go heap. Keep only integer addresses across
// release; reconstruct the still-mapped view if release fails.
func closeOwnedMapping(mem *[]byte, base, size uintptr) error {
	length, capacity := len(*mem), cap(*mem)
	address := slicePtr(*mem)
	if base == 0 {
		base, size = address, uintptr(length)
	}
	*mem = nil
	if err := munmapRange(base, size); err != nil {
		if capacity != 0 {
			*mem = unsafe.Slice((*byte)(offHeapPointer(address)), capacity)[:length]
		}
		return err
	}
	return nil
}
