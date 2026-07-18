package embedded32

// ScalarLoadOp identifies one scalar WebAssembly memory load at the generated
// code/helper ABI boundary. Results use one 32-bit slot for i32/f32 and two
// little-endian slots for i64/f64.
type ScalarLoadOp uint8

const (
	ScalarI32Load ScalarLoadOp = iota
	ScalarI32Load8S
	ScalarI32Load8U
	ScalarI32Load16S
	ScalarI32Load16U
	ScalarI64Load
	ScalarI64Load8S
	ScalarI64Load8U
	ScalarI64Load16S
	ScalarI64Load16U
	ScalarI64Load32S
	ScalarI64Load32U
	ScalarF32Load
	ScalarF64Load
)

// ScalarLoadForWasmOpcode maps every core scalar memory-load opcode to the
// target-independent embedded lowering operation.
func ScalarLoadForWasmOpcode(op byte) (ScalarLoadOp, bool) {
	switch op {
	case 0x28:
		return ScalarI32Load, true
	case 0x29:
		return ScalarI64Load, true
	case 0x2a:
		return ScalarF32Load, true
	case 0x2b:
		return ScalarF64Load, true
	case 0x2c:
		return ScalarI32Load8S, true
	case 0x2d:
		return ScalarI32Load8U, true
	case 0x2e:
		return ScalarI32Load16S, true
	case 0x2f:
		return ScalarI32Load16U, true
	case 0x30:
		return ScalarI64Load8S, true
	case 0x31:
		return ScalarI64Load8U, true
	case 0x32:
		return ScalarI64Load16S, true
	case 0x33:
		return ScalarI64Load16U, true
	case 0x34:
		return ScalarI64Load32S, true
	case 0x35:
		return ScalarI64Load32U, true
	default:
		return 0, false
	}
}

// ScalarLoadInfo returns the complete memory width, serialized result width,
// and narrow-load signedness for op.
func ScalarLoadInfo(op ScalarLoadOp) (width uint32, resultWords uint8, signed bool, ok bool) {
	switch op {
	case ScalarI32Load:
		return 4, 1, false, true
	case ScalarI32Load8S:
		return 1, 1, true, true
	case ScalarI32Load8U:
		return 1, 1, false, true
	case ScalarI32Load16S:
		return 2, 1, true, true
	case ScalarI32Load16U:
		return 2, 1, false, true
	case ScalarI64Load:
		return 8, 2, false, true
	case ScalarI64Load8S:
		return 1, 2, true, true
	case ScalarI64Load8U:
		return 1, 2, false, true
	case ScalarI64Load16S:
		return 2, 2, true, true
	case ScalarI64Load16U:
		return 2, 2, false, true
	case ScalarI64Load32S:
		return 4, 2, true, true
	case ScalarI64Load32U:
		return 4, 2, false, true
	case ScalarF32Load:
		return 4, 1, false, true
	case ScalarF64Load:
		return 8, 2, false, true
	default:
		return 0, 0, false, false
	}
}

// ScalarStoreOp identifies one scalar WebAssembly memory store. Store values
// arrive in one or two little-endian 32-bit slots, while accessWidth is always
// preflighted as one complete Wasm operation before any byte is written.
type ScalarStoreOp uint8

const (
	ScalarI32Store ScalarStoreOp = iota
	ScalarI32Store8
	ScalarI32Store16
	ScalarI64Store
	ScalarI64Store8
	ScalarI64Store16
	ScalarI64Store32
	ScalarF32Store
	ScalarF64Store
)

// ScalarStoreForWasmOpcode maps every core scalar memory-store opcode to the
// target-independent embedded lowering operation.
func ScalarStoreForWasmOpcode(op byte) (ScalarStoreOp, bool) {
	switch op {
	case 0x36:
		return ScalarI32Store, true
	case 0x37:
		return ScalarI64Store, true
	case 0x38:
		return ScalarF32Store, true
	case 0x39:
		return ScalarF64Store, true
	case 0x3a:
		return ScalarI32Store8, true
	case 0x3b:
		return ScalarI32Store16, true
	case 0x3c:
		return ScalarI64Store8, true
	case 0x3d:
		return ScalarI64Store16, true
	case 0x3e:
		return ScalarI64Store32, true
	default:
		return 0, false
	}
}

// ScalarStoreInfo returns the complete memory width and serialized input width
// for op.
func ScalarStoreInfo(op ScalarStoreOp) (accessWidth uint32, valueWords uint8, ok bool) {
	switch op {
	case ScalarI32Store:
		return 4, 1, true
	case ScalarI32Store8:
		return 1, 1, true
	case ScalarI32Store16:
		return 2, 1, true
	case ScalarI64Store:
		return 8, 2, true
	case ScalarI64Store8:
		return 1, 2, true
	case ScalarI64Store16:
		return 2, 2, true
	case ScalarI64Store32:
		return 4, 2, true
	case ScalarF32Store:
		return 4, 1, true
	case ScalarF64Store:
		return 8, 2, true
	default:
		return 0, 0, false
	}
}
