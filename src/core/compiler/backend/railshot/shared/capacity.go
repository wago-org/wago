// Package shared contains small, architecture-neutral building blocks used by
// Railshot backends. Instruction selection, registers, ABI details, and native
// encoding remain in the architecture packages.
package shared

// MaxNativeFrameBytes matches the execution stack's fence margin. Inbound
// cross-instance wrappers reserve 64 bytes and the target's offset-0 adapter
// reserves another 16 bytes before entering the target body, so the generated
// frame must leave both inside the checked headroom.
const (
	MaxNativeFrameBytes       = 256 << 10
	MaxNativeInboundCallBytes = 64 + 16
)

// NativeFrameFitsStackFence reports whether a body frame plus fixed entry
// overhead fits inside the already-checked stack-fence headroom.
func NativeFrameFitsStackFence(frameBytes, entryOverhead int) bool {
	return frameBytes >= 0 && entryOverhead >= 0 && entryOverhead <= MaxNativeFrameBytes && frameBytes <= MaxNativeFrameBytes-entryOverhead
}

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

// TaperedModuleCodeCapacity keeps the conservative small-module expansion while
// capping how much of that headroom a large module can retain. Expansion values
// are expressed in eighths (32 == 4x, 40 == 5x), avoiding floating point in the
// compiler and allowing architecture-specific measured ratios.
func TaperedModuleCodeCapacity(bodyBytes, functions, smallEighths, largeEighths, maxHeadroom int) int {
	const maxInt = int(^uint(0) >> 1)
	if bodyBytes < 0 || functions < 0 || largeEighths <= 0 || smallEighths < largeEighths || maxHeadroom < 0 || functions > (maxInt-64)/16 {
		return 0
	}
	overhead := functions*16 + 64
	body := uint64(bodyBytes)
	if body > (^uint64(0)-7)/uint64(smallEighths) {
		return 0
	}
	base := (body*uint64(largeEighths) + 7) / 8
	headroom := (body*uint64(smallEighths-largeEighths) + 7) / 8
	if headroom > uint64(maxHeadroom) {
		headroom = uint64(maxHeadroom)
	}
	total := base + headroom + uint64(overhead)
	if total > uint64(maxInt) {
		return 0
	}
	return int(total)
}
