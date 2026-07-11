//go:build amd64

package amd64

// Stack values that alias mutable module state (locals/globals) are realized
// before that state is overwritten by scanning the operand stack directly
// (realizeLocalRefs in driver.go, realizeGlobalRefs in globals.go). Those scans
// are the only consumers, and they read each elem's storage, not any auxiliary
// index — so no separate occurrence map is kept. The stack is shallow, so the
// scan is cheap; a per-key map only added hashing + linked-list maintenance on
// every push/pop/replace with no reader on the other side.

func (f *fn) replaceStorage(e *elem, st storage) {
	e.st = st
}

func (f *fn) pushValue(st storage) *elem {
	return f.s.pushValue(st)
}

func (f *fn) erase(e *elem) {
	f.s.erase(e)
}
