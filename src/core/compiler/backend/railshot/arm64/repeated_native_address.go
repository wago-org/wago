//go:build arm64

package arm64

import "os"

var repeatedNativeAddressEnabled = os.Getenv("WAGO_ARM64_NO_REPEATED_NATIVE_ADDRESS") != "1"

// reuseRepeatedNativeAddress removes only redundant, identical address setup.
// Both original loads stay in place and retain their original fault order.
func reuseRepeatedNativeAddressAt(b []byte, pc int, targets []uint64, entries []int) bool {
	if pc < 0 || pc+20 > len(b) {
		return false
	}
	a, displacement, load := rdWord(b, pc), rdWord(b, pc+4), rdWord(b, pc+8)
	if a&0xffe0fc00 != 0x8b000000 || a&31 != 16 || a>>5&31 != 26 || a>>16&31 == 16 || a>>16&31 == 31 ||
		displacement&0xff800000 != 0x91000000 || displacement&31 != 16 || displacement>>5&31 != 16 ||
		load&0x3b000000 != 0x39000000 || load&(1<<26) != 0 || load>>22&3 != 1 || load>>5&31 != 16 ||
		load&31 == 16 || load&31 == 26 || load&31 == a>>16&31 ||
		rdWord(b, pc+12) != a || rdWord(b, pc+16) != displacement {
		return false
	}
	for at := pc + 4; at <= pc+16; at += 4 {
		if branchTargeted(targets, at) {
			return false
		}
		for _, entry := range entries {
			if entry == at {
				return false
			}
		}
	}
	wrWord(b, pc+12, nopWord)
	wrWord(b, pc+16, nopWord)
	return true
}

// Used by focused tests to exercise every possible local site.
func reuseRepeatedNativeAddress(b []byte, targets []uint64, entries []int) int {
	hits := 0
	for pc := 0; pc+20 <= len(b); pc += 4 {
		if reuseRepeatedNativeAddressAt(b, pc, targets, entries) {
			hits++
			pc += 16
		}
	}
	return hits
}
