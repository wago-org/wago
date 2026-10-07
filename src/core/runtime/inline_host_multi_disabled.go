//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

func (e *Engine) tryInlineBoundedMultiHostView(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, signatures []uint32, fixed FixedHostCallView) (bool, error) {
	return false, nil
}
