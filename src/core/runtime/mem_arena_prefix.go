//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import "unsafe"

// AllocatedPrefix returns an already allocated prefix of the arena. It returns
// nil if the arena is closed or the requested extent has not been allocated.
// The caller must hold the arena's lifetime lease while using the result.
func (a *Arena) AllocatedPrefix(n int) []byte {
	if a == nil || n <= 0 || n > a.off || n > len(a.mem) {
		return nil
	}
	return a.mem[:n:n]
}

// AllocatedRange returns an already allocated range in this arena. It does not
// follow the supplied address. The caller must hold the arena's lifetime lease.
func (a *Arena) AllocatedRange(address uintptr, n int) []byte {
	if a == nil || n <= 0 || a.off < 0 || a.off > len(a.mem) || len(a.mem) == 0 {
		return nil
	}
	base := uintptr(unsafe.Pointer(unsafe.SliceData(a.mem)))
	if address < base || address-base > uintptr(a.off) || uintptr(n) > uintptr(a.off)-(address-base) {
		return nil
	}
	start := int(address - base)
	return a.mem[start : start+n : start+n]
}
