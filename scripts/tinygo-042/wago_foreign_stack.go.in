//go:build scheduler.threads

package task

import "unsafe"

// WagoForeignStack is a registered, fully readable off-heap stack. The caller
// owns its mapping until Unregister returns. No Go pointers are stored in this
// metadata as integers: the list itself is Go-managed and globally rooted.
// hostSP points to the trampoline's saved host SP cell, written before switching
// SP and left intact until SP has returned to the host stack.
type WagoForeignStack struct {
	low, high, hostSP uintptr
	next              *WagoForeignStack
}

var wagoForeignStacks *WagoForeignStack

//go:linkname wagoRegisterForeignStack runtime.wagoRegisterForeignStack
func wagoRegisterForeignStack(low, high, hostSP uintptr) unsafe.Pointer {
	if low >= high || low&7 != 0 || high&7 != 0 || hostSP < low || hostSP > high-8 {
		runtimeFatal("invalid Wago foreign stack")
	}
	// Allocate before acquiring the lock: allocation may collect.
	s := &WagoForeignStack{low: low, high: high, hostSP: hostSP}
	// This is the same lock GC holds before signalling any thread, through the
	// complete root scan. A thread holding it cannot be paused by that scan.
	activeTaskLock.Lock()
	s.next = wagoForeignStacks
	wagoForeignStacks = s
	activeTaskLock.Unlock()
	return unsafe.Pointer(s)
}

//go:linkname wagoUnregisterForeignStack runtime.wagoUnregisterForeignStack
func wagoUnregisterForeignStack(token unsafe.Pointer) {
	s := (*WagoForeignStack)(token)
	activeTaskLock.Lock()
	for p := &wagoForeignStacks; *p != nil; p = &(*p).next {
		if *p == s {
			*p = s.next
			activeTaskLock.Unlock()
			return
		}
	}
	activeTaskLock.Unlock()
	runtimeFatal("unregistered Wago foreign stack")
}

// WagoScanStack is only called with the world stopped and activeTaskLock held.
// A signal frame on a foreign stack includes the interrupted register set.
// Scan that entire registered mapping separately; never traverse the unmapped
// gap between it and the host stack. The saved host SP includes its return
// address and all caller frames. Saved callee-saved registers are in the foreign
// mapping, including during partially executed prologues/epilogues.
func WagoScanStack(bottom, top uintptr) {
	for s := wagoForeignStacks; s != nil; s = s.next {
		if bottom >= s.low && bottom < s.high {
			bottom = *(*uintptr)(unsafe.Pointer(s.hostSP))
			if bottom == 0 || bottom > top {
				runtimeFatal("invalid Wago saved host stack")
			}
			break
		}
	}
	markRoots(bottom, top)
}

func wagoScanForeignStacks() {
	for s := wagoForeignStacks; s != nil; s = s.next {
		markRoots(s.low, s.high)
	}
}
