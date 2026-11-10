//go:build amd64 && wago_profile

package amd64

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Follow actual final adapter bytes and jump targets. CALL is modeled as a
// callee returning normally; PUSH/POP independently determine CFA at each PC.
func checkAdapterStackRecovery(t *testing.T, code []byte, start int, rows []jitprofile.UnwindRange) {
	t.Helper()
	pc, depth := start, 0
	for steps := 0; steps < 200; steps++ {
		length, delta, target := 0, 0, -1
		switch code[pc] {
		case 0x51:
			length, delta = 1, 8
		case 0x59, 0x5d, 0x5e, 0x5f:
			length, delta = 1, -8
		case 0x68:
			length, delta = 5, 8
		case 0xe8:
			length = 5
		case 0xff:
			if code[pc+1] != 0xd5 {
				t.Fatal("unexpected indirect call")
			}
			length = 2
		case 0xe9:
			length = 5
			target = pc + 5 + int(int32(binary.LittleEndian.Uint32(code[pc+1:])))
		case 0xc3:
			length = 1
		case 0x48:
			switch code[pc+1] {
			case 0x8d:
				length = 7
			case 0x01:
				if code[pc+2] != 0xc5 {
					t.Fatal("unexpected ADD")
				}
				length = 3
			case 0x89:
				mod := code[pc+2] >> 6
				switch mod {
				case 0, 3:
					length = 3
				case 1:
					length = 4
				case 2:
					length = 7
				}
			default:
				t.Fatalf("unexpected adapter opcode %x at %d", code[pc:pc+3], pc)
			}
		default:
			t.Fatalf("unexpected adapter opcode %x at %d", code[pc], pc)
		}
		if length == 0 || pc+length > len(code) {
			t.Fatal("invalid adapter instruction")
		}
		for at := pc; at < pc+length; at++ {
			row, ok := jitprofile.LookupUnwind(rows, uint64(at))
			if !ok || row.CFARegister != 7 || row.CFAOffset != int64(depth+8) || row.ReturnOffset != -8 {
				t.Fatalf("PC %d: depth=%d row=%+v present=%v", at, depth, row, ok)
			}
		}
		if code[pc] == 0xc3 {
			if depth != 0 {
				t.Fatal("adapter returns with unbalanced stack")
			}
			return
		}
		depth += delta
		if depth < 0 {
			t.Fatal("adapter pops caller return address")
		}
		pc += length
		if target >= 0 {
			pc = target
		}
		if pc < 0 || pc >= len(code) {
			t.Fatal("adapter jumped out of image")
		}
	}
	t.Fatal("adapter did not return")
}

func TestAdapterUnwindFollowsModuleSharing(t *testing.T) {
	for _, variant := range []string{"plain", "tail", "legacy", "delta", "delta-disabled", "tail-multigroup", "legacy-multigroup", "delta-multigroup"} {
		for _, pop := range []encoder.Reg{RCX, RDI, RSI} {
			t.Run(fmt.Sprintf("%s/pop=%d", variant, pop), func(t *testing.T) {
				mode := strings.TrimSuffix(variant, "-multigroup")
				old := stackDeltaAdapterThunkEnabled
				stackDeltaAdapterThunkEnabled = mode != "delta-disabled"
				defer func() { stackDeltaAdapterThunkEnabled = old }()
				count := 8
				if mode == "legacy" {
					count = 2
				}
				groupSize := count
				if variant != mode {
					count *= 2
				}
				var code []byte
				var entry, internal []int
				var adapters []sharedAdapterInfo
				var tails []adapterTailInfo
				stats := &ModuleStats{}
				for i := 0; i < count; i++ {
					resultPointer := pop
					if i >= groupSize {
						// Use two byte-distinct shapes so both islands must
						// receive their own offsets and stack-state transitions.
						resultPointer = RCX
						if pop == RCX {
							resultPointer = RDI
						}
					}
					a := &encoder.Asm{}
					a.MovReg64(RBX, RSI)
					a.Push(RCX)
					pushEnd := a.Len()
					for j := 0; j < 8; j++ {
						a.MovReg64(RAX, RDX)
					}
					call := a.CallRel32()
					ret := a.Len()
					a.Pop(resultPointer)
					for j := 0; j < 8; j++ {
						a.Store64(resultPointer, int32(j*8), RAX)
					}
					a.Ret()
					end := a.Len()
					a.SubRsp(8)
					a.AddRsp(8)
					a.Ret()
					a.PatchRel32(call, end)
					f := &fn{a: a, adapterReturnOff: ret, adapterEndOff: end, stats: &CodegenStats{RecordAdapterUnwind: true, unwindAdapterPushEnd: pushEnd}}
					if err := f.collectProfileAdapterUnwind(); err != nil {
						t.Fatal(err)
					}
					f.stats.NativeSize = NativeFunctionSizeReport{TotalBytes: a.Len(), HostAdapterBytes: end, InternalFunctionBytes: a.Len() - end}
					f.stats.CodeBytes = a.Len()
					adapters = append(adapters, sharedAdapterInfo{function: uint32(i), dispOff: uint32(call), endOff: uint32(end)})
					tails = append(tails, adapterTailInfo{function: uint32(i), returnOff: uint32(ret), endOff: uint32(end)})
					entry = append(entry, len(code))
					internal = append(internal, len(code)+end)
					code = append(code, a.B...)
					stats.Funcs = append(stats.Funcs, f.stats)
				}
				var err error
				sharedBytes := 0
				relocs := make([][]callReloc, count)
				if mode == "tail" {
					code, sharedBytes, err = shareAdapterTailsAMD64(code, entry, internal, relocs, nil, nil, tails, nil, stats)
				}
				if mode != "tail" && mode != "plain" {
					code, sharedBytes, err = shareAdaptersAMD64(code, entry, internal, relocs, nil, nil, adapters, nil, stats)
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode != "plain" && sharedBytes == 0 {
					t.Fatal("fixture did not share adapters")
				}
				var rows []jitprofile.UnwindRange
				for i, f := range stats.Funcs {
					for _, r := range f.AdapterUnwind {
						r.Offset += uint64(entry[i])
						rows = append(rows, r)
					}
					if mode == "delta" && code[entry[i]] != 0x68 {
						t.Fatal("fixture did not use delta thunk")
					}
					if (mode == "legacy" || mode == "delta-disabled") && code[entry[i]+1] != 0x8d {
						t.Fatal("fixture did not use LEA thunk")
					}
				}
				for _, r := range stats.SharedAdapterUnwind {
					r.Offset += uint64(len(code) - sharedBytes)
					rows = append(rows, r)
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].Offset < rows[j].Offset })
				if err := jitprofile.ValidateUnwind(rows, uint64(len(code))); err != nil {
					t.Fatal(err)
				}
				for _, at := range entry {
					checkAdapterStackRecovery(t, code, at, rows)
				}
				for _, at := range internal {
					if _, ok := jitprofile.LookupUnwind(rows, uint64(at)); ok {
						t.Fatal("adapter rules leaked into body")
					}
				}
			})
		}
	}
}
