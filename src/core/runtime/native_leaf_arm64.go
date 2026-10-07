//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"reflect"
	goruntime "runtime"
	"sync"

	"github.com/wago-org/wago/internal/runtimebridge"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

type nativeScalarLeaf struct {
	code   []byte
	direct uintptr
}

var nativeLeafHosts sync.Map // control-frame address -> immutable nativeScalarLeaf

// scalarLeafInstruction admits only straight-line register arithmetic. Every
// source must be an argument or a previously defined scratch register. Memory,
// stack, closure/g, PC-relative, branch, call and flag-dependent instructions
// are excluded. This validates the copied instructions, rather than relying on
// a function name, Go version, benchmark input, or a promise by the callback.
func scalarLeafInstruction(w uint32, defined *uint32) bool {
	dst, n, m := w&31, (w>>5)&31, (w>>16)&31
	read := func(r uint32) bool { return r == 31 || r < 18 && *defined&(1<<r) != 0 }
	write := func(r uint32) bool {
		if r >= 18 {
			return false
		}
		*defined |= 1 << r
		return true
	}
	width := uint32(32)
	if w>>31 != 0 {
		width = 64
	}
	switch {
	case w&0x1f800000 == 0x11000000: // ADD/SUB immediate; register 31 means SP
		if dst == 31 || n == 31 || !read(n) {
			return false
		}
	case w&0x1f200000 == 0x0b000000: // ADD/SUB shifted register
		if (w>>22)&3 == 3 || (w>>10)&63 >= width || !read(n) || !read(m) {
			return false
		}
	case w&0x1f000000 == 0x0a000000: // logical shifted register
		if (w>>10)&63 >= width || !read(n) || !read(m) {
			return false
		}
	case w&0x1f800000 == 0x12800000: // MOVN/MOVZ/MOVK
		opc := (w >> 29) & 3
		if opc == 1 || (w>>21)&3 >= width/16 || opc == 3 && !read(dst) {
			return false
		}
	case w&0x1f800000 == 0x13000000: // SBFM/BFM/UBFM, including constant shifts
		opc := (w >> 29) & 3
		if opc == 3 || (w>>22)&1 != w>>31 || (w>>16)&63 >= width || (w>>10)&63 >= width || !read(n) || opc == 1 && !read(dst) {
			return false
		}
	case w&0x1fe00000 == 0x1b000000: // MADD/MSUB; excludes widening and high multiply
		if w&0x60000000 != 0 || !read(n) || !read(m) || !read((w>>10)&31) {
			return false
		}
	default:
		return false
	}
	return write(dst)
}

func scalarLeafBody(fn any, params uint32) []byte {
	if !scalarGoABI(goruntime.Version()) {
		return nil
	}
	if params < 1 || params > 2 {
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
	defined := uint32(1<<params) - 1
	body := make([]byte, 0, 128)
	for off := uintptr(0); off < 128; off += 4 {
		current := goruntime.FuncForPC(pc + off)
		if current == nil || current.Entry() != pc {
			return nil
		}
		w := *(*uint32)(offHeapPointer(pc + off))
		if w == 0xd65f03c0 { // RET LR, copied body leaves the native caller LR intact
			if defined&1 == 0 {
				return nil
			}
			return body
		}
		if !scalarLeafInstruction(w, &defined) {
			return nil
		}
		body = binary.LittleEndian.AppendUint32(body, w)
	}
	return nil
}

// RegisterNativeScalarLeaf lowers a proven ordinary Go i32 leaf into a native
// host trampoline. It copies arithmetic, never calls Go code on a foreign
// stack. The caller retains the ordinary Go binding as fallback and must use
// syscall scheduling for this frame: a native leaf is not a Go safe point.
// UnregisterHostCtrlFrame releases the mapping only after instance quiescence.
func RegisterNativeScalarLeaf(access runtimebridge.HostScalarCallAccess, ctrl []byte, fn any, params uint32) (bool, error) {
	if !access.Granted() || len(ctrl) != ctrlFrameSize {
		return false, nil
	}
	if _, exists := nativeLeafHosts.Load(slicePtr(ctrl)); exists {
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	body := scalarLeafBody(fn, params)
	if body == nil {
		return false, nil
	}
	stub, err := hostCallStubPtr()
	if err != nil {
		return false, err
	}
	var a a64.Asm
	a.SubImm64(a64.X9, a64.X26, offCustomCtx)
	mustEncode(a.Load64(a64.X9, a64.X9, 0))
	// A foreign/owned host call still has its own dispatch namespace and ABI.
	// The staged route must never reinterpret it as the sole ordinary import.
	mustEncode(a.Load64(a64.X16, a64.X9, hcImportIdx))
	a.MovImm64(a64.X17, uint64(params|1<<16)<<32)
	a.CmpReg64(a64.X16, a64.X17)
	fallback := a.Bcond(a64.CondNE)
	mustEncode(a.Load32(a64.X0, a64.X9, hcArgs))
	if params == 2 {
		mustEncode(a.Load32(a64.X1, a64.X9, hcArgs+8))
	}
	a.B = append(a.B, body...)
	// Go i32 arithmetic may use 64-bit registers; the ABI result is its low word.
	a.MovReg32(a64.X0, a64.X0)
	a.SubImm64(a64.X9, a64.X26, offCustomCtx)
	mustEncode(a.Load64(a64.X9, a64.X9, 0))
	mustEncode(a.Store64(a64.X0, a64.X9, hcResults))
	a.Ret()
	mustEncode(a.PatchBranch19(fallback, a.Len()))
	a.MovImm64(a64.X16, uint64(stub))
	a.Br(a64.X16)
	// The per-instance import table accepts a wrapper ABI function directly:
	// X0=args, X3=results. Retain results in an untouched caller-saved register.
	// This avoids the shared Go host thunk's control-frame staging entirely.
	written := uint32(0)
	for off := 0; off < len(body); off += 4 {
		written |= 1 << (binary.LittleEndian.Uint32(body[off:]) & 31)
	}
	directOff := -1
	for reg := uint32(2); reg < 18; reg++ {
		if written&(1<<reg) != 0 {
			continue
		}
		directOff = a.Len()
		a.MovReg64(a64.Reg(reg), a64.X3)
		if params == 2 {
			mustEncode(a.Load32(a64.X1, a64.X0, 8))
		}
		mustEncode(a.Load32(a64.X0, a64.X0, 0))
		a.B = append(a.B, body...)
		a.MovReg32(a64.X0, a64.X0)
		mustEncode(a.Store64(a64.X0, a64.Reg(reg), 0))
		a.Ret()
		break
	}
	mem, err := mmapExec(a.B)
	if err != nil {
		return false, err
	}
	leaf := nativeScalarLeaf{code: mem}
	if directOff >= 0 {
		leaf.direct = slicePtr(mem) + uintptr(directOff)
	}
	if _, loaded := nativeLeafHosts.LoadOrStore(slicePtr(ctrl), leaf); loaded {
		_ = munmap(mem)
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	return true, nil
}

func nativeScalarLeafPtr(ctrl []byte) uintptr {
	if mem, ok := nativeLeafHosts.Load(slicePtr(ctrl)); ok {
		return slicePtr(mem.(nativeScalarLeaf).code)
	}
	return 0
}
func unregisterNativeScalarLeaf(ctrl []byte) {
	if mem, ok := nativeLeafHosts.LoadAndDelete(slicePtr(ctrl)); ok {
		_ = munmap(mem.(nativeScalarLeaf).code)
	}
}

// BindNativeScalarLeafImport replaces only an instance-owned single-import
// dispatch cell. Shared compiled host thunks and reference descriptors retain
// their normal staged route. The caller proves this cell belongs to the frame.
func BindNativeScalarLeafImport(access runtimebridge.HostScalarCallAccess, ctrl, dispatch []byte) bool {
	if !access.Granted() || len(dispatch) != ImportDispatchEntryBytes {
		return false
	}
	value, ok := nativeLeafHosts.Load(slicePtr(ctrl))
	if !ok || value.(nativeScalarLeaf).direct == 0 {
		return false
	}
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCodePtrOffset:], uint64(value.(nativeScalarLeaf).direct))
	// The native leaf benefits from its direct wrapper ABI, not control-frame staging.
	caller := binary.LittleEndian.Uint64(dispatch[ImportDispatchCallerContextOffset:])
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCallerContextOffset:], caller&^ImportDispatchCallerGoHostTag)
	return true
}
