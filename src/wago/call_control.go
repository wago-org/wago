package wago

import wruntime "github.com/wago-org/wago/src/core/runtime"

// refreshNativeControl re-establishes the per-invocation basedata fields that a
// cross-instance native call temporarily replaces in its callee. Prepared calls
// normally bind these once, but that assumption stops being true as soon as an
// instance is entered from another instance's execution stack.
func refreshNativeControl(shared bool, eng *wruntime.Engine, jm *wruntime.JobMemory, trap []byte) error {
	if !shared {
		return nil
	}
	if jm.HasTrapCell(trap) {
		return nil
	}
	jm.SetStackFence(eng.StackLimit())
	return jm.BindTrapCell(trap)
}
