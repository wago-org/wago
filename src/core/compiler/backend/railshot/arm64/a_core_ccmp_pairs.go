//go:build arm64

package arm64

// deadFlagsPath proves overwrite before reuse along a short straight path,
// following direct jumps. Unknown instructions and conditional control decline.
func deadFlagsPath(b []byte, pc, n int) bool {
	for steps := 0; steps < 12 && pc >= 0 && pc+4 <= n; steps++ {
		w := rdWord(b, pc)
		if w == 0xD65F03C0 {
			return true
		} // RET: Wasm call boundaries have no NZCV value.
		if w&0xFC000000 == 0x14000000 {
			target, ok := branchTarget(pc, w)
			if !ok {
				return false
			}
			pc = target
			continue
		}
		add := w&0x1F000000 == 0x11000000 || w&0x1F000000 == 0x0B000000
		logic := w&0x1F000000 == 0x0A000000 || w&0x1F800000 == 0x12000000
		if add && w&(1<<29) != 0 || logic && w&0x60000000 == 0x60000000 {
			return true
		}
		move := w&0x1F800000 == 0x12800000
		load := w&0x3B000000 == 0x38000000 || w&0x3B000000 == 0x39000000 || w&0x3B000000 == 0x29000000
		bitfield := w&0x1F800000 == 0x13000000
		mul := w&0x1F000000 == 0x1B000000
		if !add && !logic && !move && !load && !bitfield && !mul && w != nopWord {
			return false
		}
		pc += 4
	}
	return false
}
