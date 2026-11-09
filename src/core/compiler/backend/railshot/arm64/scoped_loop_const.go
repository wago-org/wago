//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Immutable leases exist only inside proven call-free loops. The process
// default is opt-in; the per-compilation policy owns each selected state.
var wideScopedLoopConstsEnabled = os.Getenv("WAGO_ARM64_NO_WIDE_SCOPED_CONST") != "1"

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
	work    *uint16
}

func (f *fn) scopedLoopConstUses() (scopedLoopConstUses, bool) {
	h := &f.scopedConstHints
	if h.loopIntConstCount == 0 || !f.opt(optLoopIntConst) || f.interruptible || f.moduleEH || len(f.customInstructions) != 0 || h.flags.has(hintMutatesTable) || !wideScopedLoopConstsEnabled && f.m != nil && len(f.m.Tables) != 0 {
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
	consumer, ok := immediate.Peek()
	if !ok {
		return kind, nil
	}
	value, typ, ok = loopIntConstRegisterValue(consumer, value, typ)
	if !ok {
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

// Table helpers are a boundary even when the selected loop otherwise makes no
// calls. A module's unused tables do not require rejecting a pure numeric loop.
func scopedConstTableBarrier(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrTableGet, wasm.InstrTableSet, wasm.InstrTableSize, wasm.InstrTableGrow, wasm.InstrTableInit, wasm.InstrTableCopy, wasm.InstrTableFill, wasm.InstrElemDrop:
		return true
	default:
		return false
	}
}

const scopedLoopConstWorkLimit = 4096

// Admit detailed scanning only when a short copied-reader prefix contains a
// selected costly constant. Independent bounded accounting prevents earlier
// map/reduction probes from starving a late loop, without repeated full scans.
func (f *fn) hasWideScopedConstPrefix(source *wasm.Reader) bool {
	if !f.usesCalls || f.scopedConstWork >= scopedLoopConstWorkLimit {
		return false
	}
	uses, ok := f.scopedLoopConstUses()
	if !ok {
		return false
	}
	r := *source
	start := r.Offset()
	var imm wasm.InstructionImmediate
	for ops := 0; ops < 16 && r.Offset()-start < 128 && f.scopedConstWork < scopedLoopConstWorkLimit; ops++ {
		f.scopedConstWork++
		op, e := r.Byte()
		if e != nil {
			return false
		}
		if op == 0x41 || op == 0x42 {
			if _, e = uses.consume(op, &r); e != nil {
				return false
			}
			if uses.mask != 0 {
				return true
			}
			continue
		}
		if e = f.classifier.ClassifyInto(&r, op, &imm); e != nil {
			return false
		}
		if loopInstructionMayCall(imm.Kind) || usesBulkScratch(imm.Kind) || scopedConstTableBarrier(imm.Kind) || op == 0x0b {
			return false
		}
	}
	return false
}
