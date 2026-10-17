//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/runtime/abi"

var atomicNotifyEmptyEnabled = true

func (f *fn) notifyEmptyShortcut(memoryIndex uint32) int {
	var fallback [5]int
	n := 0
	cold := func(site int) { fallback[n] = site; n++ }
	f.a.Load64(RAX, R8, hcArgs)
	f.a.Load64(RCX, R8, hcArgs+16)
	f.a.Add64(RAX, RCX)
	cold(f.a.JccPlaceholder(condB))
	f.a.TestImm(RAX, 3, false)
	cold(f.a.JccPlaceholder(condNE))
	if memoryIndex == 0 && !f.threadedMemory0 {
		f.a.MovReg64(RDX, RBX)
	} else {
		f.a.Load64(RDX, RBX, -offMemoryDirPtr)
		f.a.Load64(RDX, RDX, int32(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirBaseOffset)
	}
	f.a.AluRI(0, RAX, 4, true)
	cold(f.a.JccPlaceholder(condB))
	f.a.Load64(RCX, RDX, -bdCurBytes)
	f.a.Cmp64(RAX, RCX)
	cold(f.a.JccPlaceholder(condA))
	if f.memoryShared(memoryIndex) {
		// Pair with wait's SC pending publication before its value check.
		// Earlier plain Wasm stores must become visible before testing waiters.
		f.a.Mfence()
		f.a.Load32(RCX, RDX, -abi.SharedNotifyPendingOffset)
		f.a.TestSelf(RCX, false)
		cold(f.a.JccPlaceholder(condNE))
	}
	f.a.MovImm64(RAX, 0)
	f.a.Store64(R8, hcResults, RAX)
	done := f.a.JmpPlaceholder()
	for _, site := range fallback[:n] {
		f.a.PatchRel32(site, f.a.Len())
	}
	f.stats.peep("atomic-notify-empty")
	return done
}
