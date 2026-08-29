//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

type tinygoPreparedIntState struct {
	mem      []byte
	stackTop uintptr
}

func (state *tinygoPreparedIntState) close() error {
	if err := munmap(state.mem); err != nil {
		return err
	}
	state.mem = nil
	state.stackTop = 0
	return nil
}
