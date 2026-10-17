package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ProvenAtomicAddress checks a constant memory32 access against the declared
// minimum, which is guaranteed at instantiation and cannot shrink. This avoids
// relying on a size snapshot that another agent may change through growth.
func ProvenAtomicAddress(mt wasm.MemType, address uint32, offset uint64, size int) (uint64, bool) {
	if mt.Limits.Addr64 || mt.Limits.Min > 65536 || size <= 0 || size&(size-1) != 0 {
		return 0, false
	}
	ea := uint64(address) + offset
	minimum := uint64(mt.Limits.Min) << 16
	if ea < offset || minimum < uint64(size) || ea > minimum-uint64(size) || ea&uint64(size-1) != 0 {
		return 0, false
	}
	return ea, true
}
