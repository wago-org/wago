//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// streamIntegerGlobalType validates only the accessed global. Unrelated float
// or reference globals do not change the integer operation's representation.
func streamIntegerGlobalType(m *wasm.Module, index uint32, store bool) (wide, ok bool) {
	if m == nil || index > 0x7fffffff/8 {
		return false, false
	}
	gt, found := m.GlobalTypeByIndex(index)
	if !found || store && !gt.Mutable {
		return false, false
	}
	t := wasm.GlobalValueType(gt)
	return wasm.EqualValType(t, wasm.I64), streamInteger(t)
}

// integerGlobal snapshots every get eagerly: a later set or call cannot change
// an existing stack value. Pinned globals retain the normal write-back protocol.
func (s *streamState) integerGlobal(r *wasm.Reader, op byte) error {
	if op != 0x23 && op != 0x24 {
		return fmt.Errorf("stream: unsupported global opcode %#x", op)
	}
	index, err := r.U32()
	if err != nil {
		return err
	}
	f := s.target
	wide, ok := streamIntegerGlobalType(f.m, index, op == 0x24)
	if !ok {
		return fmt.Errorf("stream: unsupported integer global %d", index)
	}
	pinned, hasPin := f.pinnedGlobalValueReg(index)
	if op == 0x23 {
		out := s.alloc(0)
		if hasPin {
			f.Move(out, uint8(pinned), wide)
		} else {
			// The globals array contains cell POINTERS, not inline scalar values.
			f.a.Load64(Reg(out), RBX, -int32(abi.GlobalsPtrOffset))
			f.a.Load64(Reg(out), Reg(out), int32(index*8))
			if wide {
				f.a.Load64(Reg(out), Reg(out), 0)
			} else {
				f.a.Load32(Reg(out), Reg(out), 0)
			}
		}
		id := s.add(streamNode{kind: shared.ScalarRegister, reg: out, wide: wide, refs: 1})
		s.owners[out] = id
		s.stack = append(s.stack, id)
		f.stats.peep("stream-global-get")
		return nil
	}
	f.invalidateBoundsCertFor(2, index)
	value := s.pop()
	source := s.materialize(value, 0)
	if hasPin {
		f.Move(uint8(pinned), source, wide)
		f.globalReg[index] |= globalRegDirty
	} else {
		cell := s.alloc(uint64(1) << source)
		f.a.Load64(Reg(cell), RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(Reg(cell), Reg(cell), int32(index*8))
		if wide {
			f.a.Store64(Reg(cell), 0, Reg(source))
		} else {
			f.a.Store32(Reg(cell), 0, Reg(source))
		}
	}
	s.release(value)
	f.stats.peep("stream-global-set")
	return nil
}
