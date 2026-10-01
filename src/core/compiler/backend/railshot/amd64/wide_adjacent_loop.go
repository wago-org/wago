//go:build amd64

package amd64

import "os"

// Only exact adjacent output pairs qualify. Two original iterations map to
// four lanes without reassociation; odd trip counts retain the paired fast path.
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

// Both bodies consume the same guarded entry image and publish the same exit
// contract. Emit each once with fixed scratch; do not rerun allocation or the
// bytecode parser. Zero counts have already selected the checked loop.
func (e *regionLoopEmitter) bodyWithWidths() {
	if !e.p.wide {
		e.body()
		return
	}
	f := e.f
	f.a.Load32(e.gp[0], RSP, e.off(8))
	f.a.TestImm(e.gp[0], 1, false)
	narrow := f.a.JccPlaceholder(condNE)
	e.body()
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(narrow, f.a.Len())
	e.p.wide = false
	e.body()
	e.p.wide = true
	f.a.PatchRel32(done, f.a.Len())
	f.stats.peep("region-loop-wide-odd-pair")
}
