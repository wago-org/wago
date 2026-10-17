//go:build (linux || darwin || windows) && amd64 && !tinygo

package runtime

import (
	"reflect"
	goruntime "runtime"

	"github.com/wago-org/wago/internal/runtimebridge"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// scalarLeafInstruction accepts only bounded register arithmetic. Registers
// start undefined except Go's AX/BX arguments. No memory operand, stack/frame
// register, flags input, branch, call, relocation or FP operation is copied.
// LEA is address arithmetic only: its base/index must also be defined scratch.
func scalarLeafInstruction(code []byte, defined *uint32) (int, bool) {
	pos := 0
	take := func() (byte, bool) {
		if pos >= len(code) {
			return 0, false
		}
		b := code[pos]
		pos++
		return b, true
	}
	skip := func(n int) bool {
		if len(code)-pos < n {
			return false
		}
		pos += n
		return true
	}
	rex := byte(0)
	op, ok := take()
	if !ok {
		return 0, false
	}
	if op&0xf0 == 0x40 {
		rex = op
		op, ok = take()
		if !ok {
			return 0, false
		}
	}
	read := func(r byte) bool { return r < 12 && r != 4 && r != 5 && *defined&(1<<r) != 0 }
	write := func(r byte) bool {
		if r >= 12 || r == 4 || r == 5 {
			return false
		}
		*defined |= 1 << r
		return true
	}
	finish := func(valid bool) (int, bool) { return pos, valid }
	if op >= 0xb8 && op <= 0xbf {
		n := 4
		if rex&8 != 0 {
			n = 8
		}
		if !skip(n) {
			return 0, false
		}
		return finish(write((op - 0xb8) | ((rex & 1) << 3)))
	}
	if op == 0x05 || op == 0x0d || op == 0x25 || op == 0x2d || op == 0x35 {
		return finish(skip(4) && read(0) && write(0)) // accumulator immediate arithmetic
	}
	if op == 0x98 && rex&8 != 0 {
		return finish(read(0) && write(0))
	} // CDQE
	if op == 0x0f {
		op, ok = take()
		if !ok {
			return 0, false
		}
		if op >= 0xc8 && op <= 0xcf {
			r := (op - 0xc8) | ((rex & 1) << 3)
			return finish(read(r) && write(r))
		}
		if op != 0xaf && op != 0xb6 && op != 0xb7 && op != 0xbe && op != 0xbf {
			return 0, false
		}
		modrm, ok := take()
		if !ok || modrm>>6 != 3 {
			return 0, false
		}
		dst := ((modrm >> 3) & 7) | ((rex & 4) << 1)
		src := (modrm & 7) | ((rex & 1) << 3)
		if (op == 0xb6 || op == 0xbe) && rex == 0 && src >= 4 {
			src -= 4
		} // AH/CH/DH/BH, not SP/BP
		valid := read(src)
		if op == 0xaf {
			valid = valid && read(dst)
		}
		return finish(valid && write(dst))
	}
	switch op {
	case 0x01, 0x03, 0x09, 0x0b, 0x21, 0x23, 0x29, 0x2b, 0x31, 0x33, 0x89, 0x8b, 0x8d, 0x63, 0x69, 0x6b, 0x81, 0x83, 0xc1, 0xd1, 0xd3, 0xf7, 0xff:
	default:
		return 0, false
	}
	modrm, ok := take()
	if !ok {
		return 0, false
	}
	mode := modrm >> 6
	digit := (modrm >> 3) & 7
	reg := digit | ((rex & 4) << 1)
	rm := (modrm & 7) | ((rex & 1) << 3)
	if op == 0x8d {
		if mode == 3 || mode == 0 && modrm&7 == 5 {
			return 0, false
		}
		base := rm
		valid := true
		if modrm&7 == 4 {
			sib, ok := take()
			if !ok {
				return 0, false
			}
			base = (sib & 7) | ((rex & 1) << 3)
			index := ((sib >> 3) & 7) | ((rex & 2) << 2)
			if mode == 0 && sib&7 == 5 {
				return 0, false
			}
			if index != 4 {
				valid = read(index)
			}
		}
		valid = valid && read(base)
		if mode == 1 {
			valid = valid && skip(1)
		} else if mode == 2 {
			valid = valid && skip(4)
		}
		return finish(valid && write(reg))
	}
	if mode != 3 {
		return 0, false
	}
	switch op {
	case 0x89:
		return finish(read(reg) && write(rm))
	case 0x8b:
		return finish(read(rm) && write(reg))
	case 0x63:
		return finish(rex&8 != 0 && read(rm) && write(reg))
	case 0x69, 0x6b:
		n := 4
		if op == 0x6b {
			n = 1
		}
		return finish(skip(n) && read(rm) && write(reg))
	case 0x81, 0x83:
		if rex&4 != 0 || digit != 0 && digit != 1 && digit != 4 && digit != 5 && digit != 6 {
			return 0, false
		}
		n := 4
		if op == 0x83 {
			n = 1
		}
		return finish(skip(n) && read(rm) && write(rm))
	case 0xc1, 0xd1, 0xd3:
		if rex&4 != 0 || digit != 0 && digit != 1 && digit != 4 && digit != 5 && digit != 7 {
			return 0, false
		}
		valid := read(rm)
		if op == 0xc1 {
			valid = valid && skip(1)
		}
		if op == 0xd3 {
			valid = valid && read(1)
		}
		return finish(valid && write(rm))
	case 0xf7:
		return finish(rex&4 == 0 && (digit == 2 || digit == 3) && read(rm) && write(rm))
	case 0xff:
		return finish(rex&4 == 0 && digit <= 1 && read(rm) && write(rm))
	}
	dst, src := rm, reg
	if op&2 != 0 {
		dst, src = reg, rm
	}
	// XOR/SUB of a register with itself defines zero without reading its input.
	zero := dst == src && (op == 0x31 || op == 0x33 || op == 0x29 || op == 0x2b)
	return finish((zero || read(dst) && read(src)) && write(dst))
}

func scalarLeafBody(fn any, params uint32) []byte {
	if !scalarGoABI(goruntime.Version()) || params < 1 || params > 2 {
		return nil
	}
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func || value.IsNil() {
		return nil
	}
	typ := value.Type()
	if typ.IsVariadic() || typ.NumIn() != int(params) || typ.NumOut() != 1 || typ.Out(0).Kind() != reflect.Int32 {
		return nil
	}
	for i := 0; i < typ.NumIn(); i++ {
		if typ.In(i).Kind() != reflect.Int32 {
			return nil
		}
	}
	pc := value.Pointer()
	f := goruntime.FuncForPC(pc)
	if f == nil || f.Entry() != pc {
		return nil
	}
	defined := uint32(1)
	if params == 2 {
		defined |= 1 << 3
	} // ABIInternal: AX, BX
	body := make([]byte, 0, 128)
	for off := uintptr(0); off < 128; {
		var bytes [15]byte
		n := 0
		for ; n < len(bytes) && off+uintptr(n) < 128; n++ {
			at := pc + off + uintptr(n)
			owner := goruntime.FuncForPC(at)
			if owner == nil || owner.Entry() != pc {
				break
			}
			bytes[n] = *(*byte)(offHeapPointer(at))
		}
		if n == 0 {
			return nil
		}
		if bytes[0] == 0xc3 {
			return body
		}
		size, ok := scalarLeafInstruction(bytes[:n], &defined)
		if !ok || size == 0 {
			return nil
		}
		body = append(body, bytes[:size]...)
		off += uintptr(size)
	}
	return nil
}

// Copy only proven register arithmetic, never Go code that can allocate, trap,
// inspect resources or reenter. The VM retains syscall scheduling for unbounded
// native execution. Instance quiescence releases its retained code reference.
func RegisterNativeScalarLeaf(access runtimebridge.HostScalarCallAccess, ctrl []byte, fn any, params uint32) (bool, error) {
	if !access.Granted() || len(ctrl) != ctrlFrameSize {
		return false, nil
	}
	return registerSharedNativeScalarLeaf(ctrl, fn, params, buildNativeScalarLeaf)
}

func buildNativeScalarLeaf(fn any, params uint32) (nativeScalarLeaf, bool, error) {
	body := scalarLeafBody(fn, params)
	if body == nil {
		return nativeScalarLeaf{}, false, nil
	}
	defined := uint32(1)
	if params == 2 {
		defined |= 1 << 3
	}
	for off := 0; off < len(body); {
		n, ok := scalarLeafInstruction(body[off:], &defined)
		if !ok {
			return nativeScalarLeaf{}, false, nil
		}
		off += n
	}
	scratch := x64.Reg(0)
	for _, r := range []x64.Reg{x64.R11, x64.R10, x64.R9, x64.R8, x64.RDX, x64.RSI} {
		if defined&(1<<r) == 0 {
			scratch = r
			break
		}
	}
	if scratch == x64.RAX {
		return nativeScalarLeaf{}, false, nil
	}
	saveBase := defined&(1<<x64.RBX) != 0
	stub, err := hostCallStubPtr()
	if err != nil {
		return nativeScalarLeaf{}, false, err
	}
	var a x64.Asm
	a.Load64(scratch, x64.RBX, -offCustomCtx)
	a.Load64(x64.RAX, scratch, hcImportIdx)
	a.MovImm64(x64.RCX, uint64(params|1<<16)<<32)
	a.Cmp64(x64.RAX, x64.RCX)
	fallback := a.JccPlaceholder(x64.CondNE)
	if saveBase {
		a.Push(x64.RBX)
	}
	a.Load32(x64.RAX, scratch, hcArgs)
	if params == 2 {
		a.Load32(x64.RBX, scratch, hcArgs+8)
	}
	a.B = append(a.B, body...)
	a.MovRegReg32(x64.RAX, x64.RAX)
	a.Store64(scratch, hcResults, x64.RAX)
	if saveBase {
		a.Pop(x64.RBX)
	}
	a.Ret()
	a.PatchRel32(fallback, a.Len())
	a.MovImm64(x64.RAX, uint64(stub))
	a.JmpReg(x64.RAX)
	directOff := a.Len()
	if saveBase {
		a.Push(x64.RBX)
	}
	a.MovReg64(scratch, x64.RCX)
	if params == 2 {
		a.Load32(x64.RBX, x64.RDI, 8)
	}
	a.Load32(x64.RAX, x64.RDI, 0)
	a.B = append(a.B, body...)
	a.MovRegReg32(x64.RAX, x64.RAX)
	a.Store64(scratch, 0, x64.RAX)
	if saveBase {
		a.Pop(x64.RBX)
	}
	a.Ret()
	mem, err := mmapExec(a.B)
	if err != nil {
		return nativeScalarLeaf{}, false, err
	}
	leaf := nativeScalarLeaf{code: mem, direct: slicePtr(mem) + uintptr(directOff)}
	return leaf, true, nil
}
