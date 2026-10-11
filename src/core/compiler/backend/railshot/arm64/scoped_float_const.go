//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var scopedLoopFloatConstsEnabled = true

// Only compact scalar loops qualify. Reject control nesting, calls, growth,
// bulk operations and extensions before reserving any register. The lease ends
// at this loop's end, so calls elsewhere in the function may clobber its cache.
func (f *fn) preloadScopedFloatConsts(source *wasm.Reader) {
	if !f.makesCalls || !f.scopedConstHints.flags.has(hintHasFloatConst) || !f.opt(optScopedLoopFloatConst) || len(f.fconsts) != 0 || f.moduleEH || f.interruptible || len(f.customInstructions) != 0 || len(f.ctrl) >= 65535 {
		return
	}
	scan := *source
	start := scan.Offset()
	var constants [2]storage
	n := 0
	var imm wasm.InstructionImmediate
	for scan.Offset()-start < 512 {
		op, err := scan.Byte()
		if err != nil {
			return
		}
		if op == 0x0b {
			available := 0
			blocked := f.blockedFRegs(0)
			for _, reg := range fpAllocRegs {
				if !blocked.has(reg) && f.fregUser[reg] == nil {
					available++
				}
			}
			if available < n+4 {
				return
			}
			for i := 0; i < n; i++ {
				if _, ok := f.preloadFloatConst(constants[i]); ok {
					f.fconsts[len(f.fconsts)-1].loopDepth = uint16(len(f.ctrl) + 1)
				}
			}
			if n != 0 {
				f.stats.peep("scoped-loop-float-const")
			}
			return
		}
		switch {
		case op == 0x43 || op == 0x44:
			st := storage{kind: stConst, typ: mtF32}
			if op == 0x43 {
				v, e := scan.LEU32()
				err = e
				st.cval = int64(v)
			} else {
				v, e := scan.LEU64()
				err = e
				st.typ = mtF64
				st.cval = int64(v)
			}
			if err != nil {
				return
			}
			found := false
			for i := 0; i < n; i++ {
				if constants[i].typ == st.typ && constants[i].cval == st.cval {
					found = true
				}
			}
			if !found {
				if n == len(constants) {
					return
				}
				constants[n] = st
				n++
			}
		case op == 0x0d:
			label, e := scan.U32()
			if e != nil || label != 0 {
				return
			}
		case op == 0x1a || op == 0x1b || op >= 0x20 && op <= 0x22 || op >= 0x28 && op <= 0x3e || op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0xbf:
			if f.classifier.ClassifyInto(&scan, op, &imm) != nil {
				return
			}
		default:
			return
		}
	}
}

func (f *fn) releaseScopedFloatConsts(depth int) {
	if len(f.fconsts) == 0 || int(f.fconsts[0].loopDepth) != depth {
		return
	}
	for _, c := range f.fconsts {
		if regallocCheckEnabled {
			f.checkReleaseImmutableFP(c.reg)
		}
	}
	clear(f.fconsts)
	f.fconsts = f.fconsts[:0]
}
