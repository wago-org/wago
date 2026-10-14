//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import (
	"reflect"
	goruntime "runtime"
	"strings"
)

// scalarSingleCallBody recognizes only a Go 1.27 one-i32-argument wrapper whose
// complete body is one direct call and return. Its exact standard
// stack-growth path and frame setup/teardown are checked, not copied. The
// callee must independently pass the existing bounded register-only proof.
// Generated code copies that pure callee into the native callback body. The
// import call remains; the checked Go wrapper and helper frames are fused.
// It never branches into Go code or touches a Go stack, closure, g, memory,
// or callee-saved GP/FP.
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
	return scalarLeafBodyAtPC(target, 1)
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
