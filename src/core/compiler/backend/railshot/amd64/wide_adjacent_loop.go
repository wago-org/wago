//go:build amd64

package amd64

import "os"

// Only exact adjacent output pairs qualify. Two original iterations map to
// four lanes without reassociation; incomplete groups use the checked loop.
// Existing 16-byte stream guards cover the exact extent of even groups: the
// last 32-byte access starts one original iteration earlier. Invariant loads
// remain scalar accesses followed by register broadcasts.
var regionWideAdjacentEnabled = os.Getenv("WAGO_AMD64_WIDE_ADJACENT_LOOP") == "1"

func (e *regionLoopEmitter) broadcastPair(reg Reg) {
	e.f.a.SseRR(0x66, 0x14, reg, reg, false)
	if e.p.wide {
		e.f.a.YInsertF128(reg, reg, reg, 1)
	}
}
