//go:build arm64

package arm64

import "os"

var regionalMemoryReadEnabled = os.Getenv("WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ") == "1"

// Detached regional references ordinarily need snapshots because eviction can
// no longer find them on the operand stack. An immediate memory address instead
// lends its register only for memAddr, pinned until lowering finishes. A load
// then publishes a tracked memRef; a store pins the returned address immediately.
func (f *fn) popRegionalMemoryAddress() (*elem, bool) {
	e := f.s.back()
	if e != nil && e.elemKind() == ekValue && e.st.kind == stLocalReg {
		x, reg := e.st.index(), e.st.reg
		if x >= 0 && x < len(f.locals) && reg != regNone && int(reg) < len(f.intervalOwner) && f.locals[x].reg == reg && f.intervalOwner[reg] == x {
			f.pinned = f.pinned.add(reg)
			f.erase(e)
			f.stats.peep("regional-memory-read")
			return e, true
		}
	}
	return f.popValue(), false
}
