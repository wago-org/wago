//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"reflect"
	goruntime "runtime"
	"strings"
)

// scalarSingleCallBody recognizes only a Go 1.27 one-i32-argument wrapper whose
// complete observable body is one direct call and return. Its exact standard
// stack-growth path and frame setup/teardown are checked, not copied. The
// callee must independently pass the existing bounded register-only proof.
// Generated code calls a private native copy of that callee; it never branches
// into Go code or touches a Go stack, closure, g, memory, or callee-saved GP/FP.
func scalarSingleCallBody(fn any, params uint32) []byte {
	version := goruntime.Version()
	if !scalarGoABI(version) || (version != "go1.27" && !strings.HasPrefix(version, "go1.27.")) || params != 1 {
		return nil
	}
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func || value.IsNil() {
		return nil
	}
	typ := value.Type()
	if typ.IsVariadic() || typ.NumIn() != 1 || typ.NumOut() != 1 || typ.In(0).Kind() != reflect.Int32 || typ.Out(0).Kind() != reflect.Int32 {
		return nil
	}
	pc := value.Pointer()
	if pc&3 != 0 || pc > ^uintptr(0)-60 {
		return nil
	}
	words := [15]uint32{}
	for i := range words {
		at := pc + uintptr(i*4)
		first, last := goruntime.FuncForPC(at), goruntime.FuncForPC(at+3)
		if first == nil || last == nil || first.Entry() != pc || last.Entry() != pc {
			return nil
		}
		words[i] = *(*uint32)(offHeapPointer(at))
	}
	// LDR g.stackguard; CMP SP; BLS spill; save LR/FP; establish FP;
	// BL helper; restore FP/LR/SP; RET; spill arg; morestack; reload; restart.
	expected := [15]uint32{0xf9400b90, 0xeb3063ff, 0x54000109, 0xf81e0ffe, 0xf81f83fd, 0xd10023fd, 0, 0xf85f83fd, 0xf84207fe, 0xd65f03c0, 0xb9000be0, 0xaa1e03e3, 0, 0xb9800be0, 0}
	for i, w := range expected {
		if i != 6 && i != 12 && i != 14 && words[i] != w {
			return nil
		}
	}
	target, ok := scalarRelativeTarget(pc+24, words[6], 0x94000000)
	if !ok || target == pc {
		return nil
	}
	slow, ok := scalarRelativeTarget(pc+48, words[12], 0x94000000)
	if !ok {
		return nil
	}
	restart, ok := scalarRelativeTarget(pc+56, words[14], 0x14000000)
	if !ok || restart != pc {
		return nil
	}
	sf := goruntime.FuncForPC(slow)
	if sf == nil || sf.Entry() != slow || (sf.Name() != "runtime.morestack_noctxt" && sf.Name() != "runtime.morestack_noctxt.abi0") {
		return nil
	}
	leaf := scalarLeafBodyAtPC(target, 1)
	if leaf == nil {
		return nil
	}
	written := uint32(0)
	for off := 0; off < len(leaf); off += 4 {
		written |= 1 << (binary.LittleEndian.Uint32(leaf[off:]) & 31)
	}
	// One untouched caller-saved register carries LR across the private helper.
	// Leave another for the existing argument-buffer wrapper's result pointer.
	scratch, free := a64.ZR, 0
	for r := a64.X17; r >= a64.X2; r-- {
		if written&(1<<r) == 0 {
			free++
			if scratch == a64.ZR {
				scratch = r
			}
		}
	}
	if free < 2 {
		return nil
	}
	var a a64.Asm
	a.MovReg64(scratch, a64.LR)
	call := a.Bl()
	a.MovReg64(a64.LR, scratch)
	done := a.Branch()
	callee := a.Len()
	a.B = append(a.B, leaf...)
	a.Ret()
	if !a.PatchBranch26(call, callee) || !a.PatchBranch26(done, a.Len()) {
		return nil
	}
	return a.B
}

func scalarRelativeTarget(pc uintptr, word, opcode uint32) (uintptr, bool) {
	if word&0xfc000000 != opcode {
		return 0, false
	}
	delta := int64(int32(word<<6)>>6) * 4
	if delta < 0 {
		if uintptr(-delta) > pc {
			return 0, false
		}
		return pc - uintptr(-delta), true
	}
	if pc > ^uintptr(0)-uintptr(delta) {
		return 0, false
	}
	return pc + uintptr(delta), true
}
