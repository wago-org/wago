//go:build amd64

package amd64

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/internal/jitprofile"
)

// collectProfileUnwind runs after frame patching and before native compaction.
// Admission in compileFuncAttempt excludes every body path with transient RSP
// changes. Adapters, traps, GC stubs, and literal pools are outside these ranges.
func (f *fn) collectProfileUnwind(internalOff int) error {
	if f.stats == nil || !f.stats.RecordUnwind {
		return nil
	}
	// These direct-local lowerings keep RSP fixed in the caller, including
	// wrapper argument/result staging. Dynamic, host, indirect, tail, inline,
	// and helper lowerings are deliberately not admitted by their names.
	knownCalls := false
	for kind, count := range f.stats.Calls {
		if count == 0 {
			continue
		}
		switch kind {
		case callKindRegisterABI, callKindMixed, callKindWrapper:
			knownCalls = true
		default:
			f.stats.RecordUnwind = false
			return nil
		}
	}
	if f.stats.UnwindHasCalls && !knownCalls {
		f.stats.RecordUnwind = false
		return nil
	}
	code := f.a.B
	sub, add, frame := f.subRspAt, f.addRspAt, f.frameSize()
	if internalOff < 0 || sub-3 != internalOff || add < sub+7 || add+5 > len(code) || frame < 0 || uint64(frame) > 0x7fffffff ||
		code[sub-3] != 0x48 || code[sub-2] != 0x81 || code[sub-1] != 0xec ||
		code[add-3] != 0x48 || code[add-2] != 0x81 || code[add-1] != 0xc4 || code[add+4] != 0xc3 ||
		binary.LittleEndian.Uint32(code[sub:]) != uint32(frame) || binary.LittleEndian.Uint32(code[add:]) != uint32(frame) {
		return fmt.Errorf("amd64: profiling unwind frame layout mismatch")
	}
	f.stats.UnwindInternalOffset = internalOff
	f.stats.UnwindRanges = []jitprofile.UnwindRange{
		{Offset: uint64(internalOff), Size: uint64(sub + 4 - internalOff), CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
		{Offset: uint64(sub + 4), Size: uint64(add - sub), CFARegister: 7, CFAOffset: int64(frame) + 8, ReturnOffset: -8},
		{Offset: uint64(add + 4), Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
	}
	return nil
}
