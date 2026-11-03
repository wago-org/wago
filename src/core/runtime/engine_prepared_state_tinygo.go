//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import "unsafe"

type tinygoPreparedIntState struct {
	foreignStack unsafe.Pointer
	mem          []byte
	stackTop     uintptr
}

func (state *tinygoPreparedIntState) close() error {
	if state.foreignStack != nil {
		unregisterTinyGoForeignStack(state.foreignStack)
		state.foreignStack = nil
	}
	// Engine.Close attempts the stack unmap even if the thunk unmap fails.
	// Remove the GC registration first; the Close lease excludes active users.
	if err := munmap(state.mem); err != nil {
		return err
	}
	state.mem = nil
	state.stackTop = 0
	return nil
}
