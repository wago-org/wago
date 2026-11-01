//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import "unsafe"

type tinygoPreparedIntState struct {
	foreignStack unsafe.Pointer
	mem          []byte
	stackTop     uintptr
}

func (state *tinygoPreparedIntState) close() error {
	if err := munmap(state.mem); err != nil {
		return err
	}
	if state.foreignStack != nil {
		unregisterTinyGoForeignStack(state.foreignStack)
		state.foreignStack = nil
	}
	state.mem = nil
	state.stackTop = 0
	return nil
}
