//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Immutable leases exist only inside proven call-free loops. The process
// default is opt-in; the per-compilation policy owns each selected state.
var scopedLoopConstsEnabled = os.Getenv("WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST") != "0"

// Keep only the lease metadata in compiler scratch state. A pointer
// to the full function hint view can make scan-local views escape even when the
// optimization is disabled in a particular compilation.
type scopedLoopConstHints struct {
	loopIntConst      [4]int64
	flags             funcHintFlags
	loopIntConstTypes uint8
	loopIntConstCount uint8
}

func scopedConstHintsFrom(h *funcHintView) scopedLoopConstHints {
	return scopedLoopConstHints{loopIntConst: h.loopIntConst, flags: h.flags, loopIntConstTypes: h.loopIntConstTypes, loopIntConstCount: h.loopIntConstCount}
}

type scopedLoopConstUses struct {
	hints   *scopedLoopConstHints
	mask    uint8
	blocked bool
}

func (f *fn) scopedLoopConstUses() (scopedLoopConstUses, bool) {
	h := &f.scopedConstHints
	if h.loopIntConstCount == 0 || !f.opt(optLoopIntConst) || f.interruptible || f.moduleEH || len(f.customInstructions) != 0 || h.flags.has(hintMutatesTable) || f.m != nil && len(f.m.Tables) != 0 {
		return scopedLoopConstUses{}, false
	}
	return scopedLoopConstUses{hints: h}, true
}
func (u *scopedLoopConstUses) note(op byte, source *wasm.Reader) {
	immediate := *source
	_, _ = u.consume(op, &immediate)
}

func (u *scopedLoopConstUses) consume(op byte, immediate *wasm.Reader) (wasm.InstrKind, error) {
	typ := uint8(1)
	kind := wasm.InstrI32Const
	var value int64
	var err error
	if op == 0x41 {
		var v int32
		v, err = immediate.I32()
		value = int64(v)
	} else {
		typ = 2
		kind = wasm.InstrI64Const
		value, err = immediate.I64()
	}
	if err != nil {
		return kind, err
	}
	if u.blocked || u.mask == uint8(1<<u.hints.loopIntConstCount)-1 {
		return kind, nil
	}
	var matches uint8
	for i := 0; i < int(u.hints.loopIntConstCount); i++ {
		bit := uint8(1 << i)
		if u.mask&bit == 0 && u.hints.loopIntConstTypes>>(2*i)&3 == typ && u.hints.loopIntConst[i] == value {
			matches |= bit
		}
	}
	if matches == 0 {
		return kind, nil
	}
	consumer, ok := immediate.Peek()
	if !ok || !loopIntConstNeedsRegister(consumer, value, typ) {
		return kind, nil
	}
	// The native operator disappears before instruction selection for x*2^n.
	// An existing function hint may include it; a new scoped lease must not
	// reserve a register for a consumer that has already become a shift.
	if consumer == 0x6c || consumer == 0x7e {
		v := uint64(value)
		if typ == 1 {
			v = uint64(uint32(value))
		}
		if v != 0 && v&(v-1) == 0 {
			return kind, nil
		}
	}
	u.mask |= matches
	return kind, nil
}
func (f *fn) preloadScopedLoopConsts(fr *ctrlFrame) {
	uses := uint8(fr.flags >> 12)
	fr.flags &^= ctrlFlags(15 << 12)
	if uses == 0 || !fr.has(ctrlLoopCallFree) || f.scopedConstHints.loopIntConstCount == 0 {
		return
	}
	h := funcHintView{}
	// The loop scan rejects every fixed-scratch helper within this lease.
	h.flags = f.scopedConstHints.flags &^ hintUsesBulkMem
	for i := 0; i < int(f.scopedConstHints.loopIntConstCount); i++ {
		if uses&(1<<i) != 0 {
			j := h.loopIntConstCount
			h.loopIntConst[j] = f.scopedConstHints.loopIntConst[i]
			h.loopIntConstTypes |= (f.scopedConstHints.loopIntConstTypes >> (2 * i) & 3) << (2 * j)
			h.loopIntConstCount++
		}
	}
	base := f.iconstN
	f.reserveLoopIntConsts(&h)
	if f.iconstN != base {
		fr.flags |= ctrlLoopConstScope | ctrlFlags(base)<<12
		f.stats.peep("scoped-loop-int-const")
	}
}
func (f *fn) releaseScopedLoopConsts(base uint8) {
	for i := base; i < f.iconstN; i++ {
		c := f.iconsts[i]
		f.reserved = f.reserved.remove(c.reg)
		if regallocCheckEnabled {
			f.checkReleaseImmutableGP(c.reg)
		}
		f.iconsts[i] = intConstReg{}
	}
	f.iconstN = base
	if f.intervalControl {
		f.intervalRegLimit = f.currentIntervalRegLimit()
	}
}
