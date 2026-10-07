//go:build (linux || darwin || windows) && (amd64 || arm64) && (windows || tinygo)

package runtime

func (e *Engine) tryInlineBoundedHost(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) (bool, error) {
	return false, nil
}

func (e *Engine) tryInlineBoundedHostView(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedHostCallView) (bool, error) {
	return false, nil
}
