//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"testing"
)

func TestStreamIntegerGlobalType(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{
		{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}},
		{Type: wasm.GlobalType{Type: wasm.I64}},
		{Type: wasm.GlobalType{Type: wasm.F64, Mutable: true}},
	}}
	for _, c := range []struct {
		x             uint32
		set, wide, ok bool
	}{{0, false, false, true}, {0, true, false, true}, {1, false, true, true}, {1, true, false, false}, {2, false, false, false}, {3, false, false, false}, {0xffffffff, false, false, false}} {
		wide, ok := streamIntegerGlobalType(m, c.x, c.set)
		if ok != c.ok || ok && wide != c.wide {
			t.Fatalf("index=%d set=%v got=%v,%v", c.x, c.set, wide, ok)
		}
	}
	if _, ok := streamIntegerGlobalType(nil, 0, false); ok {
		t.Fatal("nil module accepted")
	}
}

func TestStreamIntegerGlobalEmission(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, pin := range []bool{false, true} {
			for _, set := range []bool{false, true} {
				typ := wasm.I32
				if wide {
					typ = wasm.I64
				}
				m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: typ, Mutable: true}}}}
				f := &fn{m: m, a: &encoder.Asm{}}
				f.initGlobalRegs(1)
				s := streamState{target: f, nodes: []streamNode{{}}, regs: []uint8{uint8(R8), uint8(R9)}, reserved: uint64(1) << R12}
				if pin {
					f.globalReg[0] = R12
				}
				var input streamID
				if set {
					input = s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(R8), wide: wide, refs: 2})
					s.owners[R8] = input
					s.stack = append(s.stack, input)
				}
				op := byte(0x23)
				if set {
					op = 0x24
				}
				r := wasm.ReaderFrom([]byte{0})
				if err := s.integerGlobal(&r, op); err != nil {
					t.Fatal(err)
				}
				var want encoder.Asm
				if pin {
					dst, src := R8, R12
					if set {
						dst, src = R12, R8
					}
					if wide {
						want.MovReg64(dst, src)
					} else {
						want.MovRegReg32(dst, src)
					}
				} else {
					ptr := R8
					if set {
						ptr = R9
					}
					want.Load64(ptr, RBX, -int32(abi.GlobalsPtrOffset))
					want.Load64(ptr, ptr, 0)
					if set {
						if wide {
							want.Store64(ptr, 0, R8)
						} else {
							want.Store32(ptr, 0, R8)
						}
					} else {
						if wide {
							want.Load64(ptr, ptr, 0)
						} else {
							want.Load32(ptr, ptr, 0)
						}
					}
				}
				if !bytes.Equal(f.a.B, want.B) {
					t.Fatalf("wide=%v pinned=%v set=%v got=%x want=%x", wide, pin, set, f.a.B, want.B)
				}
				if set {
					if len(s.stack) != 0 || s.node(input).refs != 1 || s.owners[R8] != input {
						t.Fatal("set destroyed surviving value alias")
					}
					if pin && f.globalReg[0]&globalRegDirty == 0 {
						t.Fatal("pinned set not marked dirty")
					}
				} else {
					if len(s.stack) != 1 || s.node(s.stack[0]).reg != uint8(R8) || s.node(s.stack[0]).constant != 0 {
						t.Fatal("get did not publish normalized owned snapshot")
					}
				}
			}
		}
	}
}
