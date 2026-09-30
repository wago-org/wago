//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// The bounded vector cache exchanges four whole-function pins for four regional
// leases. Set WAGO_AMD64_VECTOR_REGIONS=0 to use whole-function pins alone.
var vectorRegionsEnabled = os.Getenv("WAGO_AMD64_VECTOR_REGIONS") != "0"

type vectorRegionState struct {
	locals  [4]uint16
	next    uint8
	enabled bool
}

func (f *fn) vectorRegionBoundary(op byte) {
	switch op {
	case 0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x1f, 0x40, 0xfb, 0xfc, 0xfe:
		for i := range f.vectorRegion.locals {
			f.homeVectorLocal(i)
		}
	}
}

// The lookahead does not retain an event tape. It asks only whether the new
// value is read before a write or boundary, within 16 operations and 128 bytes.
func (f *fn) vectorRegionWillRead(reader *wasm.Reader, x int) bool {
	if reader == nil {
		return false
	}
	r := *reader
	start := r.Offset()
	for n := 0; n < 16 && r.HasNext() && r.Offset()-start < 128; n++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x00, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x1f, 0x40, 0xfb, 0xfc, 0xfe:
			return false
		}
		var imm wasm.InstructionImmediate
		if err = f.classifier.ClassifyInto(&r, op, &imm); err != nil {
			return false
		}
		if op == 0x20 && int(imm.Index)+f.localBase == x {
			return true
		}
		if (op == 0x21 || op == 0x22) && int(imm.Index)+f.localBase == x {
			return false
		}
	}
	return false
}
func (f *fn) homeVectorLocal(i int) {
	packed := f.vectorRegion.locals[i]
	if packed == 0 {
		return
	}
	x := int(packed) - 1
	reg := f.locals[x].reg
	f.storeFrameVector(f.localAddr(x), reg)
	// All vector operations are eager. Pending reads can become lazy frame reads
	// after writeback, since this boundary does not change the local's value.
	for e := f.s.head.next; e != f.s.head; e = e.next {
		if e.isValue() && e.st.kind == stLocalReg && e.st.typ == mtV128 && e.st.index() == x {
			f.replaceStorage(e, storage{kind: stLocalRef, typ: mtV128, idx: uint32(x)})
		}
	}
	f.fpinnedLocalMask = f.fpinnedLocalMask.remove(reg)
	f.locals[x].reg = regNone
	f.locals[x].isFloat = false
	f.locals[x].state = lsMem
	f.vectorRegion.locals[i] = 0
	f.stats.peep("vector-region-writeback")
}
func (f *fn) cacheVectorLocal(e *elem, x int, tee bool) bool {
	i := -1
	for k, v := range f.vectorRegion.locals {
		if v == 0 {
			i = k
			break
		}
	}
	if i < 0 {
		i = int(f.vectorRegion.next)
		f.homeVectorLocal(i)
	}
	reg := f.materializeV128(e)
	// A materialization may use a relinquished whole-function pin. Its original
	// owner can recover it later, so it must never become a regional home.
	if f.fpinnedLocalMask.has(reg) {
		return false
	}
	f.releaseF(reg)
	f.locals[x].reg = reg
	f.locals[x].isFloat = true
	f.locals[x].state = lsReg
	f.fpinnedLocalMask = f.fpinnedLocalMask.add(reg)
	f.vectorRegion.locals[i] = uint16(x + 1)
	f.vectorRegion.next = uint8((i + 1) % len(f.vectorRegion.locals))
	if tee {
		f.replaceStorage(e, storage{kind: stLocalReg, typ: mtV128, reg: reg, idx: uint32(x)})
	} else {
		f.erase(e)
	}
	f.stats.peep("vector-region-admit")
	return true
}
