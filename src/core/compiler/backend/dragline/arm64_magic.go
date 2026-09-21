package dragline

// arm64UnsignedI32ImmediateMagic finds an exact round-up multiplier for
// unsigned 32-bit division. ARM64 can materialize the full uint32 multiplier,
// so unlike x86's signed imm32 form this includes multipliers with bit 31 set.
func arm64UnsignedI32ImmediateMagic(divisor uint32) (multiplier uint32, shift byte, ok bool) {
	if divisor < 2 {
		return 0, 0, false
	}
	for s := uint(0); s < 32; s++ {
		power := uint64(1) << (32 + s)
		m := (power + uint64(divisor) - 1) / uint64(divisor)
		if m >= 1<<32 {
			return 0, 0, false
		}
		if m*uint64(divisor)-power <= uint64(1)<<s {
			return uint32(m), byte(32 + s), true
		}
	}
	return 0, 0, false
}
