//go:build amd64

package amd64

import (
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// The four retained modules have maxima of 34,635 loop bytes and 13,889
// operations. Round up to 40 KiB / 16,384 operations. Each of the register
// reservation and lowering phases has a 256 KiB total lookahead budget.
const gcLoopScanLimit = 40 * 1024
const gcLoopScanOps = 16384
const gcLoopScanBudget = 256 * 1024

type gcResolvedLoopPlan struct {
	objects [gcResolvedLimit]gcResolvedObject
	endPC   uint32
	n       int
}

// Inspect only direct, noncollecting reads and scalar/control instructions.
// Calls, allocations, host effects, unknown operations, EH, custom semantics,
// inline readers and interruptible headers cannot acquire a loop certificate.
func (f *fn) inspectGCResolvedLoop(reader *wasm.Reader) (p gcResolvedLoopPlan) {
	if !gcResolveReuseEnabled || f.interruptible || f.moduleEH || f.localBase != 0 || f.inlineDepth != 0 ||
		len(f.customInstructions) != 0 || !f.gcStructHelpers || f.gcLoopScanBytes >= gcLoopScanBudget {
		return
	}
	look := *reader
	window, err := look.Bytes(min(gcLoopScanLimit, min(look.BytesLeft(), (gcLoopScanBudget-int(f.gcLoopScanBytes))/2)))
	if err != nil {
		return
	}
	r := wasm.ReaderFrom(window)
	defer func() { f.gcLoopScanBytes += uint32(r.Offset()) }()
	depth, previous := 1, -1
	var imm wasm.InstructionImmediate
	closed := false
	for ops := 0; ops < gcLoopScanOps && r.BytesLeft() > 0; ops++ {
		op, e := r.Byte()
		if e != nil || f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return gcResolvedLoopPlan{}
		}
		source := previous
		previous = -1
		switch op {
		case 0x02, 0x04:
			depth++
		case 0x0b:
			depth--
			if depth == 0 {
				closed = true
			}
		case 0x20:
			previous = int(imm.Index)
		case 0x21, 0x22, 0x00, 0x01, 0x05, 0x0c, 0x0d, 0x0f, 0x1a, 0x41, 0x42, 0xd1, 0xd4:
		case 0xfb:
			switch imm.Subopcode {
			case 2, 3, 4:
				if layout, ok := f.gcTypeLayout(imm.Index, wasm.CompStruct); !ok || int(imm.Index2) >= len(layout.FieldLayout) {
					return gcResolvedLoopPlan{}
				}
				off, scalar, final, ok := f.directGCStructLayout(imm.Index, imm.Index2)
				size := uint32(scalar.size)
				if !ok && imm.Subopcode == 2 {
					field, fieldFinal, found := f.gcStructFieldLayout(imm.Index, imm.Index2)
					if found && field.CollectorRef && field.Size == 4 {
						off, size, final, ok = field.Offset, field.Size, fieldFinal, true
					}
				}
				if !ok || !final && !gcSharedStubsEnabled || uint64(gc.PayloadOffset)+uint64(off)+uint64(size) > math.MaxInt32 {
					return gcResolvedLoopPlan{}
				}
				if source >= 0 {
					required := gc.PayloadOffset + off + size
					i := 0
					for ; i < p.n; i++ {
						if p.objects[i].local == source {
							break
						}
					}
					if i == p.n {
						if p.n == gcResolvedLimit {
							break
						}
						p.objects[i] = gcResolvedObject{valid: true, loop: true, local: source, typeIndex: imm.Index, requiredBytes: required}
						p.n++
					} else {
						if p.objects[i].typeIndex != imm.Index {
							return gcResolvedLoopPlan{}
						}
						p.objects[i].requiredBytes = min(p.objects[i].requiredBytes, required)
					}
				}
			case 11, 12, 13:
				if !f.gcArrayHelpers {
					return gcResolvedLoopPlan{}
				}
				layout, cached := f.gcTypeLayout(imm.Index, wasm.CompArray)
				if !cached {
					return gcResolvedLoopPlan{}
				}
				_, final, ok := f.directGCArrayAccessLayout(imm.Index)
				if !ok && imm.Subopcode == 11 && layout.ElemLayout.CollectorRef && layout.ElemLayout.Size == 4 {
					final, ok = layout.Type.Final, true
				}
				if !ok || !final {
					return gcResolvedLoopPlan{}
				}
			default:
				return gcResolvedLoopPlan{}
			}
		case 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
			0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
			0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		default:
			return gcResolvedLoopPlan{}
		}
		if closed {
			break
		}
	}
	if !closed || p.n == 0 {
		return gcResolvedLoopPlan{}
	}
	end := r.Offset()
	// A second bounded pass catches writes before as well as after the first read.
	r = wasm.ReaderFrom(window[:end])
	for r.BytesLeft() > 0 {
		op, _ := r.Byte()
		if f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return gcResolvedLoopPlan{}
		}
		if op == 0x21 || op == 0x22 {
			for i := 0; i < p.n; i++ {
				if p.objects[i].local == int(imm.Index) {
					p.objects[i].valid = false
				}
			}
		}
	}
	f.gcLoopScanBytes += uint32(end)
	n := 0
	for i := 0; i < p.n; i++ {
		if p.objects[i].valid {
			p.objects[n] = p.objects[i]
			n++
		}
	}
	p.n = n
	pc := uint64(f.tracePCBase) + uint64(reader.Offset()) + uint64(end-1)
	if pc == 0 || pc > math.MaxUint32 {
		return gcResolvedLoopPlan{}
	}
	p.endPC = uint32(pc)
	return
}

// Reserve only for a function with a proved loop, before local pins are chosen.
// No register is reserved globally and no compact GC local becomes a raw root.
func (f *fn) reserveGCResolvedLoopRegisters(code []byte, pool []Reg) []Reg {
	if !f.hasLoop || !gcResolveReuseEnabled || f.interruptible || !f.gcStructHelpers || f.moduleEH || len(f.customInstructions) != 0 {
		return pool
	}
	r := wasm.ReaderFrom(code[:min(len(code), gcLoopScanLimit)])
	var imm wasm.InstructionImmediate
	for r.BytesLeft() > 0 && f.gcLoopScanBytes < gcLoopScanBudget {
		op, err := r.Byte()
		if err != nil || f.classifier.ClassifyInto(&r, op, &imm) != nil {
			break
		}
		if op != 3 {
			continue
		}
		p := f.inspectGCResolvedLoop(&r)
		if p.n == 0 {
			continue
		}
		for _, reg := range [...]Reg{RBP, R12, R13, R14} {
			if f.reserved.has(reg) {
				continue
			}
			f.gcLoopRegisters = f.gcLoopRegisters.add(reg)
			pool = withoutReg(pool, reg)
			p.n--
			if p.n == 0 {
				break
			}
		}
		break
	}
	f.gcLoopScanBytes = 0 // the lowering phase has its own fixed budget
	return pool
}

func (f *fn) beginGCResolvedLoop(reader *wasm.Reader) {
	if f.gcLoopEndPC != 0 || f.gcLoopRegisters == 0 || f.unreachable {
		return
	}
	p := f.inspectGCResolvedLoop(reader)
	if p.n == 0 {
		return
	}
	f.invalidateGCResolvedObject()
	i := 0
	for _, reg := range [...]Reg{RBP, R12, R13, R14} {
		if i == p.n {
			break
		}
		if !f.gcLoopRegisters.has(reg) || f.regUser[reg] != nil || f.pinned.union(f.pinnedLocalMask).union(f.reserved).has(reg) {
			continue
		}
		c := f.gcCertificate(i)
		*c = p.objects[i]
		c.reg = reg
		f.a.XorSelf32(reg)
		f.pinned = f.pinned.add(reg)
		i++
	}
	if i != 0 {
		f.gcLoopEndPC = p.endPC
		f.stats.peep("gc-resolve-loop")
	}
}
