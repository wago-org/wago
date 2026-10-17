//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/internal/runtimebridge"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"reflect"
	goruntime "runtime"
	"sync"
	"unsafe"
)

type nativeScalarLeaf struct {
	code   []byte
	direct uintptr
}

var nativeLeafHosts nativeScalarLeafRegistry

type nativeScalarLeafRegistry struct {
	mu      sync.RWMutex
	entries map[*byte]nativeScalarLeaf
}

func (r *nativeScalarLeafRegistry) Load(key *byte) (nativeScalarLeaf, bool) {
	r.mu.RLock()
	leaf, ok := r.entries[key]
	r.mu.RUnlock()
	return leaf, ok
}

func (r *nativeScalarLeafRegistry) LoadOrStore(key *byte, leaf nativeScalarLeaf) (nativeScalarLeaf, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.entries[key]; ok {
		return previous, true
	}
	if r.entries == nil {
		r.entries = make(map[*byte]nativeScalarLeaf)
	}
	r.entries[key] = leaf
	return leaf, false
}

func (r *nativeScalarLeafRegistry) LoadAndDelete(key *byte) (nativeScalarLeaf, bool) {
	r.mu.Lock()
	leaf, ok := r.entries[key]
	delete(r.entries, key)
	r.mu.Unlock()
	return leaf, ok
}

func scalarLeafInstructionRV64(w uint32, defined *uint32) bool {
	op, rd, rs1, rs2, funct := w&127, w>>7&31, w>>15&31, w>>20&31, w>>12&7
	read := func(r uint32) bool { return r == 0 || *defined&(1<<r) != 0 }
	allowed := func(r uint32) bool { return r == 0 || r >= 5 && r <= 7 || r >= 9 && r <= 23 }
	if !allowed(rd) {
		return false
	}
	switch op {
	case 0x37: // LUI writes a sign-extended constant.
	case 0x13:
		if !read(rs1) {
			return false
		}
		switch funct {
		case 0, 2, 3, 4, 6, 7:
		case 1:
			if w>>26 != 0 {
				return false
			}
		case 5:
			if w>>26 != 0 && w>>26 != 16 {
				return false
			}
		default:
			return false
		}
	case 0x1b:
		if !read(rs1) {
			return false
		}
		switch funct {
		case 0:
		case 1:
			if w>>25 != 0 {
				return false
			}
		case 5:
			if w>>25 != 0 && w>>25 != 32 {
				return false
			}
		default:
			return false
		}
	case 0x33, 0x3b:
		if !read(rs1) || !read(rs2) {
			return false
		}
		f7 := w >> 25
		if f7 == 1 {
			if funct != 0 {
				return false
			}
		} else if f7 == 32 {
			if funct != 0 && funct != 5 {
				return false
			}
		} else if f7 != 0 {
			return false
		}
		if op == 0x3b && funct != 0 && funct != 1 && funct != 5 {
			return false
		}
	default:
		return false
	}
	if rd != 0 {
		*defined |= 1 << rd
	}
	return true
}
func scalarLeafBodyRV64(fn any, params uint32) []byte {
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
	function := goruntime.FuncForPC(pc)
	if function == nil || function.Entry() != pc {
		return nil
	}
	defined := uint32(1 << 10)
	if params == 2 {
		defined |= 1 << 11
	}
	body := make([]byte, 0, 128)
	for off := uintptr(0); off < 128; {
		current := goruntime.FuncForPC(pc + off)
		if current == nil || current.Entry() != pc {
			return nil
		}
		low := *(*uint16)(offHeapPointer(pc + off))
		var word uint32
		if low&3 != 3 {
			expanded, ret, ok := scalarLeafCompressedRV64(low)
			if !ok {
				return nil
			}
			if ret {
				return body
			}
			word = expanded
			off += 2
		} else {
			if off > 124 {
				return nil
			}
			high := *(*uint16)(offHeapPointer(pc + off + 2))
			word = uint32(low) | uint32(high)<<16
			if word == 0x00008067 {
				return body
			}
			off += 4
		}
		if !scalarLeafInstructionRV64(word, &defined) {
			return nil
		}
		body = binary.LittleEndian.AppendUint32(body, word)
	}

	return nil
}

// This leaf never calls Go on a guest stack. Its per-instance executable owner
// is released only after the owning control frame becomes quiescent.
func RegisterNativeScalarLeaf(access runtimebridge.HostScalarCallAccess, ctrl []byte, fn any, params uint32) (bool, error) {
	if !access.Granted() || len(ctrl) != ctrlFrameSize {
		return false, nil
	}
	if _, exists := nativeLeafHosts.Load(unsafe.SliceData(ctrl)); exists {
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	body := scalarLeafBodyRV64(fn, params)
	if body == nil {
		return false, nil
	}
	stub, err := hostCallStubPtr()
	if err != nil {
		return false, err
	}
	var a enc.Asm
	a.Ld(enc.X28, enc.X25, -offCustomCtx)
	// A foreign table activation borrows the caller's trampoline but owns a
	// different parked control. Its import index/signature can match ours; only
	// this exact per-instance control authorizes the private Go leaf body.
	a.Constant64(enc.X29, int64(slicePtr(ctrl)))
	foreign := a.BranchLong(enc.NE, enc.X28, enc.X29)
	a.Ld(enc.X29, enc.X28, int32(hcImportIdx))
	a.Constant64(enc.X30, int64(uint64(params|1<<16)<<32))
	fallback := a.BranchLong(enc.NE, enc.X29, enc.X30)
	a.Lw(enc.X10, enc.X28, int32(hcArgs))
	if params == 2 {
		a.Lw(enc.X11, enc.X28, int32(hcArgs+8))
	}
	a.B = append(a.B, body...)
	a.ZeroExtend32(enc.X10, enc.X10)
	a.Sd(enc.X10, enc.X28, int32(hcResults))
	a.Ret()
	if err := a.PatchLong(fallback, int64(a.Len())); err != nil {
		return false, err
	}
	if err := a.PatchLong(foreign, int64(a.Len())); err != nil {
		return false, err
	}
	a.Constant64(enc.X30, int64(stub))
	a.Jalr(enc.Zero, enc.X30, 0)
	directOff := a.Len()
	a.Addi(enc.X28, enc.X13, 0)
	if params == 2 {
		a.Lw(enc.X11, enc.X10, 8)
	}
	a.Lw(enc.X10, enc.X10, 0)
	a.B = append(a.B, body...)
	a.ZeroExtend32(enc.X10, enc.X10)
	a.Sd(enc.X10, enc.X28, 0)
	a.Ret()
	if a.Err() != nil {
		return false, a.Err()
	}
	mem, err := mmapExec(a.B)
	if err != nil {
		return false, err
	}
	leaf := nativeScalarLeaf{code: mem, direct: slicePtr(mem) + uintptr(directOff)}
	if _, loaded := nativeLeafHosts.LoadOrStore(unsafe.SliceData(ctrl), leaf); loaded {
		_ = munmap(mem)
		return false, fmt.Errorf("jit: native scalar control frame already registered")
	}
	return true, nil
}
func nativeScalarLeafPtr(ctrl []byte) uintptr {
	if value, ok := nativeLeafHosts.Load(unsafe.SliceData(ctrl)); ok {
		return slicePtr(value.code)
	}
	return 0
}
func unregisterNativeScalarLeaf(ctrl []byte) {
	if value, ok := nativeLeafHosts.LoadAndDelete(unsafe.SliceData(ctrl)); ok {
		_ = munmap(value.code)
	}
}
func BindNativeScalarLeafImport(access runtimebridge.HostScalarCallAccess, ctrl, dispatch []byte) bool {
	if !access.Granted() || len(dispatch) != ImportDispatchEntryBytes {
		return false
	}
	value, ok := nativeLeafHosts.Load(unsafe.SliceData(ctrl))
	if !ok {
		return false
	}
	leaf := value
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCodePtrOffset:], uint64(leaf.direct))
	caller := binary.LittleEndian.Uint64(dispatch[ImportDispatchCallerContextOffset:])
	binary.LittleEndian.PutUint64(dispatch[ImportDispatchCallerContextOffset:], caller&^ImportDispatchCallerGoHostTag)
	return true
}
