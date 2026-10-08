//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// One immutable imported-first directory drives operand types and memarg
// grammar in both admission and emission. Mixed modules cannot use one global
// offset-width flag. Count/shape bounds precede this classifier's construction.
type memoryProfile struct {
	count      int
	indexed    bool
	address64  [2]uint64
	classifier wasm.ModuleInstructionClassifier
}

func makeMemoryProfile(memories []wasm.MemType, indexed bool) memoryProfile {
	p := memoryProfile{count: len(memories), indexed: indexed}
	for i, memory := range memories {
		if memory.Limits.Addr64 {
			p.address64[i>>6] |= uint64(1) << (uint(i) & 63)
		}
	}
	p.classifier = wasm.NewModuleInstructionClassifier(&wasm.Module{Memories: memories}, indexed)
	return p
}

func (p memoryProfile) wide(index uint32) bool {
	return uint64(index) < uint64(p.count) && p.address64[index>>6]&(uint64(1)<<(index&63)) != 0
}

// The caller has already admitted both memory indices. init's data source and
// count stay I32; copy's count is I64 only when both addresses are I64.
func (p memoryProfile) bulkWidths(kind wasm.InstrKind, dst, src uint32) [3]bool {
	d := p.wide(dst)
	switch kind {
	case wasm.InstrMemoryInit:
		return [3]bool{d, false, false}
	case wasm.InstrMemoryCopy:
		s := p.wide(src)
		return [3]bool{d, s, d && s}
	default: // memory.fill: byte value remains I32.
		return [3]bool{d, false, d}
	}
}
