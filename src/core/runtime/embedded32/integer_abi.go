package embedded32

// I32DivRemOp identifies the four trapping WebAssembly i32 division and
// remainder operations used by generated 32-bit code.
type I32DivRemOp uint8

const (
	I32DivS I32DivRemOp = iota
	I32DivU
	I32RemS
	I32RemU
)

func I32DivRemForWasmOpcode(op byte) (I32DivRemOp, bool) {
	if op < 0x6d || op > 0x70 {
		return 0, false
	}
	return I32DivRemOp(op - 0x6d), true
}

func I32DivRemInfo(op I32DivRemOp) (signed, remainder, ok bool) {
	switch op {
	case I32DivS:
		return true, false, true
	case I32DivU:
		return false, false, true
	case I32RemS:
		return true, true, true
	case I32RemU:
		return false, true, true
	default:
		return false, false, false
	}
}
