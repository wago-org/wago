//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The initial proof covers only plain i32 pairs, never an atomic access.
// Only the immediately following identical pure address and full-width ordinary
// load may reuse this store's value. The real store and its bounds check remain.
// No intervening access, synchronization, callback, local write or trap is
// skipped. In the relaxed model this load may read its own preceding write;
// there is no intervening synchronization to order another write between them.
func (f *fn) inspectFixedSharedStoredLoad(source *wasm.Reader, memoryIndex uint32, off uint64, size int, typ machineType) (int, bool) {
	if !f.sharedStoreForward || memoryIndex != 0 || len(f.customInstructions) != 0 || size != 4 || typ != mtI32 {
		return 0, false
	}
	r := *source
	start, budget := r.Offset(), 32
	if !f.matchRepeatedAddress(&r, f.s.back(), false, &budget) || r.Offset()-start > 256 {
		return 0, false
	}
	op, err := r.Byte()
	if err != nil || op != 0x28 {
		return 0, false
	}
	index, offset, err := f.readMemArg(&r)
	return r.Offset(), err == nil && index == memoryIndex && offset == off
}
