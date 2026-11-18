//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wRuntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestStreamMemoryShapes(t *testing.T) {
	cases := []struct {
		op                  byte
		size                int
		signed, wide, store bool
	}{
		{0x28, 4, false, false, false}, {0x29, 8, false, true, false},
		{0x2c, 1, true, false, false}, {0x2d, 1, false, false, false},
		{0x2e, 2, true, false, false}, {0x2f, 2, false, false, false},
		{0x30, 1, true, true, false}, {0x31, 1, false, true, false},
		{0x32, 2, true, true, false}, {0x33, 2, false, true, false},
		{0x34, 4, true, true, false}, {0x35, 4, false, true, false},
		{0x36, 4, false, false, true}, {0x37, 8, false, true, true},
		{0x3a, 1, false, false, true}, {0x3b, 2, false, false, true},
		{0x3c, 1, false, true, true}, {0x3d, 2, false, true, true}, {0x3e, 4, false, true, true},
	}
	var supported [256]bool
	for _, c := range cases {
		supported[c.op] = true
		size, signed, wide, store := streamMemoryShape(c.op)
		if size != c.size || signed != c.signed || wide != c.wide || store != c.store {
			t.Fatalf("opcode %#x: got %d %t %t %t", c.op, size, signed, wide, store)
		}
	}
	for op := 0; op < 256; op++ {
		if !supported[op] {
			if size, _, _, _ := streamMemoryShape(byte(op)); size != 0 {
				t.Fatalf("unsupported opcode %#x admitted", op)
			}
		}
	}
}

func TestStreamMemoryAddressChecks(t *testing.T) {
	for _, off := range []uint32{0, 1, 0x7ffffff7, 0x7ffffff8, 0x7fffffff, 0x80000000, 0xffffffff} {
		for _, size := range []int{1, 2, 4, 8} {
			sc := newScratch()
			f := fn{a: sc.asm, sc: sc, wasmPC: 37, traceFuncIdx: 11}
			got := f.streamCheckedAddress(R9, R10, off, size)
			var want encoder.Asm
			disp := int32(0)
			if uint64(off)+uint64(size) <= 0x7fffffff {
				disp = int32(off)
				want.LeaDisp(R10, R9, int32(off)+int32(size))
			} else {
				want.MovImm32(R10, int32(off))
				want.Add64(R9, R10)
				want.LeaDisp(R10, R9, int32(size))
			}
			want.AluRM(cmpRMcode, R10, RBX, -bdCurBytes, true)
			branch := want.JccPlaceholder(condA)
			if got != disp || !bytes.Equal(f.a.B, want.B) {
				t.Fatalf("off=%#x size=%d: incorrect address/check encoding", off, size)
			}
			sites := sc.trapSites[trapMemOOB]
			if len(sites) != 1 || sites[0].branch != uint32(branch) || sites[0].pc != 37 || sites[0].function != 11 {
				t.Fatalf("incorrect memory trap attribution: %+v", sites)
			}
		}
	}
}

func streamMemoryTestEnable(t *testing.T) {
	t.Helper()
	previous := streamingOperandsEnabled
	streamingOperandsEnabled = true
	t.Cleanup(func() { streamingOperandsEnabled = previous })
}

func streamMemoryTestAdmitted(t *testing.T, m *wasm.Module) {
	t.Helper()
	if !admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
		t.Fatal("memory fixture did not reach streaming admission")
	}
}

func TestStreamMemoryExecuteLoads(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x28, 0x29, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			size, signed, wide, _ := streamMemoryShape(op)
			result := wasm.I32
			if wide {
				result = wasm.I64
			}
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{result}, []byte{0, 0x20, 0, op, 0, 0, 0xb})
			streamMemoryTestAdmitted(t, m)
			pattern := []byte{0x80, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32}
			raw := binary.LittleEndian.Uint64(pattern)
			want := raw
			if size < 8 {
				want &= (uint64(1) << uint(size*8)) - 1
			}
			if signed {
				want = uint64(int64(want<<uint(64-size*8)) >> uint(64-size*8))
			}
			if !wide {
				want = uint64(uint32(want))
			}
			for _, address := range []uint64{0xffffffff00000000, uint64(65536 - size)} {
				got, _, err := runMemAmd64(t, m, func(mem []byte) { copy(mem[uint32(address):], pattern) }, address)
				if err != nil || got != want {
					t.Fatalf("addr=%x got=%x want=%x err=%v", address, got, want, err)
				}
			}
			if _, _, err := runMemAmd64(t, m, nil, uint64(65537-size)); err == nil {
				t.Fatal("OOB load did not trap")
			}
		})
	}
}

func TestStreamMemoryExecuteOffsetsAndStores(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x36, 0x37, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e} {
		size, _, wide, _ := streamMemoryShape(op)
		typ := wasm.I32
		if wide {
			typ = wasm.I64
		}
		body := []byte{0, 0x20, 0, 0x20, 1, op, 0, 0, 0x20, 1, 0xb}
		m := modMem(t, 1, []wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ}, body)
		streamMemoryTestAdmitted(t, m)
		value := uint64(0x123456789abcdef0)
		if !wide {
			value = uint64(uint32(value))
		}
		got, mem, err := runMemAmd64(t, m, nil, uint64(65536-size), value)
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], value)
		if err != nil || got != value || !bytes.Equal(mem[65536-size:], b[:size]) {
			t.Fatalf("store %x got=%x err=%v", op, got, err)
		}
		_, mem, err = runMemAmd64(t, m, func(mem []byte) {
			for i := range mem {
				mem[i] = 0xa5
			}
		}, uint64(65537-size), value)
		if err == nil || !bytes.Equal(mem, bytes.Repeat([]byte{0xa5}, 65536)) {
			t.Fatalf("store %x was non-atomic at bounds", op)
		}
	}
	for _, off := range []uint32{0x7ffffffb, 0x7fffffff, 0x80000000, 0xffffffff} {
		body := []byte{0, 0x20, 0, 0x28, 0}
		body = append(body, wasmtest.ULEB(off)...)
		body = append(body, 0xb)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		streamMemoryTestAdmitted(t, m)
		for _, addr := range []uint64{0, 1, 0xffffffff} {
			if _, _, err := runMemAmd64(t, m, nil, addr); err == nil {
				t.Fatalf("offset %x address %x wrapped", off, addr)
			}
		}
	}
	// Aliased store address and value retain the local's owner until both reads.
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 0, 0x36, 0, 0, 0x20, 0, 0xb})
	streamMemoryTestAdmitted(t, m)
	got, mem, err := runMemAmd64(t, m, nil, 32)
	if err != nil || got != 32 || binary.LittleEndian.Uint32(mem[32:]) != 32 {
		t.Fatalf("aliased store got=%d err=%v", got, err)
	}
}

func TestStreamMemoryExecuteBulk(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, sub := range []byte{10, 11} {
		body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, sub, 0}
		if sub == 10 {
			body = append(body, 0)
		}
		// All three local snapshots must survive bulk fixed-register clobbers.
		body = append(body, 0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x6a, 0xb)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		streamMemoryTestAdmitted(t, m)
		for _, n := range []uint64{0, 1, 7, 16, 31, 96, 128, 255, 256, 511, 1024} {
			for _, dst := range []uint64{31, 32, 33, 2048} {
				original := make([]byte, 65536)
				for i := range original {
					original[i] = byte(i*19 + 7)
				}
				want := append([]byte(nil), original...)
				if sub == 10 {
					copy(want[dst:dst+n], want[32:32+n])
				} else {
					for i := dst; i < dst+n; i++ {
						want[i] = 32
					}
				}
				got, mem, err := runMemAmd64(t, m, func(mem []byte) { copy(mem, original) }, dst, 32, n)
				if err != nil || got != dst+32+n || !bytes.Equal(mem, want) {
					t.Fatalf("bulk=%d dst=%d n=%d got=%d err=%v", sub, dst, n, got, err)
				}
			}
		}
		for _, args := range [][3]uint64{{65536, 65536, 0}, {65537, 0, 0}, {65535, 0, 2}, {0, 65535, 2}, {0xffffffff, 1, 1}} {
			invalid := args[0]+args[2] > 65536 || sub == 10 && args[1]+args[2] > 65536
			original := bytes.Repeat([]byte{0xa5}, 65536)
			_, mem, err := runMemAmd64(t, m, func(mem []byte) { copy(mem, original) }, args[:]...)
			if invalid {
				if err == nil || !bytes.Equal(mem, original) {
					t.Fatalf("bulk=%d args=%v invalid range modified memory", sub, args)
				}
			} else if err != nil {
				t.Fatalf("bulk=%d args=%v unexpected trap %v", sub, args, err)
			}
		}
	}
}

func TestStreamMemoryDroppedLoadTrapPC(t *testing.T) {
	streamMemoryTestEnable(t)
	// The dropped load remains observable and must precede unreachable.
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, []byte{0, 0x20, 0, 0x28, 0, 0, 0x1a, 0x00, 0x0b})
	streamMemoryTestAdmitted(t, m)
	for _, interruptible := range []bool{false, true} {
		for _, tc := range []struct {
			address uint64
			code    wRuntime.TrapCode
			pc      uint32
		}{
			{65535, wRuntime.TrapLinMemOutOfBounds, m.Code[0].LocalDeclBytes + 2},
			{0, wRuntime.TrapUnreachable, m.Code[0].LocalDeclBytes + 6},
		} {
			_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Interruptible: interruptible}, nil, tc.address)
			var trap *wRuntime.TrapError
			if !errors.As(err, &trap) || trap.Code != tc.code {
				t.Fatalf("interruptible=%v address=%d: wrong trap %v", interruptible, tc.address, err)
			}
			if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != tc.pc {
				t.Fatalf("interruptible=%v address=%d: incorrect source %+v wantpc=%d", interruptible, tc.address, trap.Frames, tc.pc)
			}
		}
	}
}

func TestStreamBulkRegisterPreconditions(t *testing.T) {
	f := &fn{}
	s := streamState{target: f}
	regs, err := s.streamBulkRegisters(true)
	if err != nil || regs != ([4]Reg{0, 1, 2, 3}) {
		t.Fatalf("free bank=%v err=%v", regs, err)
	}
	f.fpinned = maskOf(0)
	f.fpinnedLocalMask = maskOf(1)
	f.fconsts = []floatConstReg{{reg: 2}}
	f.fregUser[3] = &elem{}
	regs, err = s.streamBulkRegisters(true)
	if err != nil || regs != ([4]Reg{4, 5, 6, 7}) {
		t.Fatalf("occupied bank=%v err=%v", regs, err)
	}
	for _, reg := range []Reg{RAX, RCX, RDX, RSI, RDI, R8} {
		f.reserved = maskOf(reg)
		if _, err = s.streamBulkRegisters(true); err == nil {
			t.Fatalf("persistent GP %d accepted as scratch", reg)
		}
	}
	f.reserved = 0
	f.fpinned = regMask(0xffff)
	if _, err = s.streamBulkRegisters(true); err == nil {
		t.Fatal("exhausted vector bank accepted")
	}
	// Fill needs only one vector register; do not require four unnecessarily.
	f.fpinned = regMask(0xffef)
	regs, err = s.streamBulkRegisters(false)
	if err != nil || regs[0] != 4 {
		t.Fatalf("single fill scratch=%v err=%v", regs, err)
	}
}

func TestStreamMemoryStoreAddressShiftClobber(t *testing.T) {
	streamMemoryTestEnable(t)
	original := scalarRegisterOrder
	t.Cleanup(func() { scalarRegisterOrder = original })
	forced := []uint8{uint8(RCX)}
	for _, r := range original {
		if r != uint8(RCX) {
			forced = append(forced, r)
		}
	}
	// Stock order covers production; RCX-first makes the fixed-register hazard
	// deterministic rather than depending on incidental pressure heuristics.
	for _, order := range [][]uint8{original, forced} {
		scalarRegisterOrder = order
		for _, alias := range []bool{false, true} {
			addressLocal := byte(0)
			if alias {
				addressLocal = 3
			}
			body := []byte{0, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x20, addressLocal, 0x20, 1, 0x74, 0x20, 3, 0x36, 0, 0, 0x20, 3, 0xb}
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			streamMemoryTestAdmitted(t, m)
			value := uint64(0x12345677)
			if alias {
				value = 31
			}
			args := []uint64{1, 2, 0, 0xaabbccdd00000000 | value}
			address := uint32(4)
			if alias {
				address = uint32(value+1) << 2
			}
			for _, interruptible := range []bool{false, true} {
				var stats ModuleStats
				opts := CompileOptions{Interruptible: interruptible}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, mem, err := runMemAmd64WithOptions(t, m, opts, nil, args...)
				if err != nil || got != value+1 || binary.LittleEndian.Uint32(mem[address:]) != uint32(value+1) {
					t.Fatalf("firstreg=%d alias=%v interruptible=%v got=%x stored=%x err=%v", order[0], alias, interruptible, got, binary.LittleEndian.Uint32(mem[address:]), err)
				}
				if diagnosticsEnabled && !regallocCheckEnabled && stats.Funcs[0].Peephole["streaming-function"] != 1 {
					t.Fatal("shifted store bypassed stream engine")
				}
			}
		}
	}
}
