// Package shared contains small, architecture-neutral building blocks used by
// Railshot backends. Instruction selection, registers, ABI details, and native
// encoding remain in the architecture packages.
package shared

// StackArenaCapacity estimates operand nodes for one function. An opcode-based
// hint avoids reserving nodes for immediate bytes while a body-size floor keeps
// malformed or incomplete hints from causing allocation cliffs.
func StackArenaCapacity(bodyLen, nLocals, nodeHint int) int {
	legacy := bodyLen + nLocals/4 + 1
	if nodeHint <= 0 {
		return legacy
	}
	precise := nodeHint + nodeHint/2 + nLocals/4 + 1
	if floor := bodyLen/4 + nLocals/4 + 1; precise < floor {
		precise = floor
	}
	if precise > legacy {
		precise = legacy
	}
	return precise
}

// ModuleCodeCapacity estimates the final native image capacity. Expansion is an
// architecture-specific upper estimate of native bytes per Wasm body byte;
// per-function headroom covers alignment and adapters.
func ModuleCodeCapacity(bodyBytes, functions, expansion int) int {
	const maxInt = int(^uint(0) >> 1)
	if bodyBytes < 0 || functions < 0 || expansion <= 0 || functions > (maxInt-64)/16 {
		return 0
	}
	overhead := functions*16 + 64
	if bodyBytes > (maxInt-overhead)/expansion {
		return 0
	}
	return bodyBytes*expansion + overhead
}
