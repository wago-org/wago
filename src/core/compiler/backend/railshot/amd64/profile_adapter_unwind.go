//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func adapterUnwindRange(offset, size int, cfa int64) jitprofile.UnwindRange {
	return jitprofile.UnwindRange{Offset: uint64(offset), Size: uint64(size), CFARegister: 7, CFAOffset: cfa, ReturnOffset: -8}
}

// The adapter emitter has one PUSH RCX and one POP RCX/RDI/RSI. Record the push
// at emission, and validate the pop at the recorded call-return address. Argument
// loads, result stores, and global synchronization do not change RSP. Body
// qualification is independent: an adapter can be known around an unknown callee.
func (f *fn) collectProfileAdapterUnwind() error {
	if f.stats == nil || !f.stats.RecordAdapterUnwind || f.adapterEndOff == 0 {
		return nil
	}
	pushEnd, pop, end := f.stats.unwindAdapterPushEnd, f.adapterReturnOff, f.adapterEndOff
	if pushEnd <= 0 || pop < pushEnd || end <= pop+1 || end > len(f.a.B) || f.a.B[pushEnd-1] != 0x51 || f.a.B[end-1] != 0xc3 {
		return fmt.Errorf("amd64: profiling adapter unwind layout mismatch")
	}
	switch f.a.B[pop] {
	case 0x59, 0x5e, 0x5f: // POP RCX, RSI, RDI
	default:
		return fmt.Errorf("amd64: profiling adapter unwind pop mismatch")
	}
	f.stats.AdapterUnwind = []jitprofile.UnwindRange{
		adapterUnwindRange(0, pushEnd, 8),
		adapterUnwindRange(pushEnd, pop+1-pushEnd, 16),
		adapterUnwindRange(pop+1, end-pop-1, 8),
	}
	return nil
}

// Full sharing replaces an entry with LEA/JMP or PUSH-delta/JMP. The shared
// template replaces CALL rel32 with CALL RBP, and optionally consumes the delta
// in an eleven-byte prefix. The ordinary finalizer owns these exact decisions;
// metadata follows them before the old adapter bytes are overwritten.
func recordSharedAdapterUnwind(ms *ModuleStats, groups []sharedAdapterGroup, infos []sharedAdapterInfo) error {
	if ms == nil {
		return nil
	}
	var recorded map[uint32]bool
	for _, info := range infos {
		if int(info.function) >= len(ms.Funcs) || ms.Funcs[info.function] == nil {
			continue
		}
		stats := ms.Funcs[info.function]
		if len(stats.AdapterUnwind) == 0 {
			continue
		}
		g := &groups[info.group]
		if !recorded[info.group] {
			mapper, err := shared.NewOffsetMap(g.length, []shared.DeletedRange{{Off: uint32(g.dispOff + 1), Len: 3}})
			if err != nil {
				return err
			}
			rows, err := shared.RemapNativeUnwind(stats.AdapterUnwind, &mapper)
			if err != nil {
				return err
			}
			if g.stackDelta {
				ms.SharedAdapterUnwind = append(ms.SharedAdapterUnwind,
					adapterUnwindRange(g.sharedOff, 1, 16), // before POP RBP
					adapterUnwindRange(g.sharedOff+1, g.prefixBytes()-1, 8))
			}
			for _, row := range rows {
				row.Offset += uint64(g.sharedOff + g.prefixBytes())
				ms.SharedAdapterUnwind = append(ms.SharedAdapterUnwind, row)
			}
			if recorded == nil {
				recorded = make(map[uint32]bool)
			}
			recorded[info.group] = true
		}
		if g.stackDelta {
			stats.AdapterUnwind = []jitprofile.UnwindRange{adapterUnwindRange(0, 5, 8), adapterUnwindRange(5, g.thunkBytes()-5, 16)}
		} else {
			stats.AdapterUnwind = []jitprofile.UnwindRange{adapterUnwindRange(0, g.thunkBytes(), 8)}
		}
	}
	return nil
}

// Tail sharing moves the result-pointer POP and return stores into an island.
// The replacement JMP keeps the pointer on the stack until the shared POP.
func recordSharedAdapterTailUnwind(ms *ModuleStats, groups []adapterTailGroup, infos []adapterTailInfo) error {
	if ms == nil {
		return nil
	}
	var recorded map[uint32]bool
	for _, info := range infos {
		if int(info.function) >= len(ms.Funcs) || ms.Funcs[info.function] == nil {
			continue
		}
		stats := ms.Funcs[info.function]
		if len(stats.AdapterUnwind) == 0 {
			continue
		}
		g := &groups[info.group]
		var head []jitprofile.UnwindRange
		for _, row := range stats.AdapterUnwind {
			start, end := row.Offset, row.Offset+row.Size
			if start < uint64(info.returnOff) {
				r := row
				if end > uint64(info.returnOff) {
					r.Size = uint64(info.returnOff) - start
				}
				head = append(head, r)
			}
			if !recorded[info.group] && end > uint64(info.returnOff) {
				if start < uint64(info.returnOff) {
					start = uint64(info.returnOff)
				}
				row.Offset = uint64(g.sharedOff) + start - uint64(info.returnOff)
				row.Size = end - start
				ms.SharedAdapterUnwind = append(ms.SharedAdapterUnwind, row)
			}
		}
		if recorded == nil {
			recorded = make(map[uint32]bool)
		}
		recorded[info.group] = true
		atReturn, ok := jitprofile.LookupUnwind(stats.AdapterUnwind, uint64(info.returnOff))
		if !ok || atReturn.CFAOffset != 16 || atReturn.ReturnOffset != -8 {
			return fmt.Errorf("amd64: profiling shared adapter tail has invalid return state")
		}
		stats.AdapterUnwind = append(head, adapterUnwindRange(int(info.returnOff), sharedAdapterTailJumpBytesAMD64, 16))
	}
	return nil
}
