//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"golang.org/x/sys/cpu"
	"math/bits"
	"testing"
)

func TestStreamRotateTeeTokenOwnership(t *testing.T) {
	streamRotateHomeEnable(t)
	s := streamRotateHomeState()
	home := s.residency.cells[s.residency.homes[0]].reg
	before := s.target.a.Len()
	r := wasm.ReaderFrom([]byte{0x22, 0})
	if !s.tryRotateIntoLocal(&r, 0x77) || r.Offset() != 0 {
		t.Fatal("tee producer rejected or advanced cursor")
	}
	id := s.pop()
	cell := uint32(s.node(id).slot)
	if !s.commitRotateIntoLocal(0, id, 0) {
		t.Fatal("tee commit rejected")
	}
	s.retain(id)
	s.stack = append(s.stack, id)
	if s.node(id).refs != 2 || s.residency.cells[cell].refs != 2 || s.locals[0] != id || s.node(id).constant != 0 {
		t.Fatal("tee duplicated backing ref or failed token consume")
	}
	var want encoder.Asm
	want.Rorx(Reg(home), RAX, 25, false)
	if !bytes.Equal(s.target.a.B[before:], want.B) {
		t.Fatalf("tee added MOV native%x want%x", s.target.a.B[before:], want.B)
	}
	if s.commitRotateIntoLocal(0, id, 0) {
		t.Fatal("consumed token accepted twice")
	}
	// Drop only the local reference. A detached operand still owns this generation.
	s.locals[0] = 0
	s.release(id)
	if s.node(id).refs != 1 || s.residency.cells[cell].refs != 2 {
		t.Fatal("local overwrite destroyed stacked generation")
	}
	s.evacuateBacking(cell)
	if s.operand(s.stack[0]).Kind != shared.ScalarFrame {
		t.Fatal("detached tee alias failed evacuation")
	}
	s.release(s.pop())
	if s.residency.cells[cell].refs != 1 {
		t.Fatal("final node release did not leave only map")
	}
}
func TestStreamRotateTeeRuntimeOverwrite(t *testing.T) {
	if !cpu.X86.HasBMI2 {
		t.Skip("native RORX requires BMI2")
	}
	streamRotateHomeEnable(t)
	for _, wide := range []bool{false, true} {
		typ := streamNumericType(wide)
		xor, rot, add, block := byte(0x73), byte(0x77), byte(0x6a), byte(0x7f)
		if wide {
			xor, rot, add, block = 0x85, 0x89, 0x7c, 0x7e
		}
		body := []byte{0}
		for i := 0; i < 100; i++ {
			body = append(body, 1)
		}
		body = append(body, 0x03, block, 0x20, 0, 0x20, 1, xor)
		body = append(body, streamConstBytes(wide, 7)...)
		body = append(body, rot, 0x22, 0)
		body = append(body, streamConstBytes(wide, 9)...)
		body = append(body, 0x21, 0, 0x20, 0, add, 0x0b, 0x0b)
		m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
		for _, on := range []bool{false, true} {
			streamingRotateHomeEnabled = on
			for _, intr := range []bool{false, true} {
				var stats ModuleStats
				opts := CompileOptions{Interruptible: intr, AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2, Optimizations: map[string]bool{"bmi2-rorx": true}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				x, y := uint64(0xdeadbeef87654321), uint64(0xabcdef0112345678)
				want := bits.RotateLeft64(x^y, 7) + 9
				if !wide {
					want = uint64(bits.RotateLeft32(uint32(x^y), 7) + 9)
				}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, x, y)
				if err != nil || got != want {
					t.Fatalf("wide%v on%v intr%v got%x want%x err%v", wide, on, intr, got, want, err)
				}
				if diagnosticsEnabled && !regallocCheckEnabled {
					p := stats.Funcs[0].Peephole
					if p["streaming-function"] != 1 {
						t.Fatal("fallback")
					}
					wantN := 0
					if on {
						wantN = 1
					}
					if p["stream-rotate-home"] != wantN || p["stream-rotate-home-set"] != wantN {
						t.Fatal("tee handoff activation wrong", p)
					}
				}
			}
		}
	}
}
