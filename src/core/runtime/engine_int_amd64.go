//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

// enterNativeInt enters a register-ABI integer leaf on the Engine foreign stack.
// RBX carries linMem, RAX/RCX/RDX/R8 carry up to four arguments, and RAX returns
// the optional scalar result.
func enterNativeInt(code, linMem, a0, a1, a2, a3, foreignStackTop uintptr) uintptr

func (e *Engine) CallPreparedInt(code, linMemBase uintptr, a0, a1, a2, a3 uint64, trap []byte) (uint64, error) {
	result := uint64(enterNativeInt(code, linMemBase, uintptr(a0), uintptr(a1), uintptr(a2), uintptr(a3), e.stackTop))
	if len(trap) >= 4 {
		if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
			storeTrap(trap, 0)
			return 0, trapErrorFromBuffer(tc, trap)
		}
	}
	return result, nil
}
