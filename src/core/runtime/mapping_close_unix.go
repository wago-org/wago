//go:build (linux || darwin) && (amd64 || arm64)

package runtime

// Unix's syscall mapper owns the original slice until Munmap succeeds.
func closeOwnedMapping(mem *[]byte, base, size uintptr) error {
	var err error
	if base != 0 {
		err = munmapRange(base, size)
	} else {
		err = munmap(*mem)
	}
	if err == nil {
		*mem = nil
	}
	return err
}
