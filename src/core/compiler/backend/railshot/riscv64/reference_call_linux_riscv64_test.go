//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestReferenceCallRV64PreflightBeforeArgumentWrites(t *testing.T) {
	for _, escaped := range []bool{false, true} {
		t.Run(fmt.Sprintf("escaped%t", escaped), func(t *testing.T) {
			f := tailRANativeFixtureNew(t, false, 1, 1)
			trampoline, err := coreruntime.HostCallTrampolinePointer()
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint64(f.ctrl[HostTrampolineOffset:], uint64(trampoline))
			arena, err := coreruntime.NewArena(2 * abi.RV64InstanceContextBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer arena.Close()
			ca, cb := arena.Alloc(abi.RV64InstanceContextBytes), arena.Alloc(abi.RV64InstanceContextBytes)
			copy(ca, f.ca)
			copy(cb, f.cb)
			f.ca, f.cb = ca, cb
			binary.LittleEndian.PutUint64(f.da[32:], uint64(tailRAPointer(ca)))
			binary.LittleEndian.PutUint64(f.db[32:], uint64(tailRAPointer(cb)))
			binary.LittleEndian.PutUint64(f.db[16:], uint64(f.b.LinMemBase()))
			sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
			target := tailRANativeFrameInvocation(t, sig, f.b, cb, f.db, 0, true, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
			d := f.witness[:40]
			key := wasm.StructuralFuncTypeKey(sig)
			binary.LittleEndian.PutUint64(d, uint64(target.code.Base()))
			binary.LittleEndian.PutUint64(d[8:], key)
			binary.LittleEndian.PutUint64(d[16:], uint64(f.b.LinMemBase())|abi.FuncRefLocalWrapperHomeTag)
			binary.LittleEndian.PutUint64(d[24:], uint64(tailRAPointer(d)))
			binary.LittleEndian.PutUint64(d[32:], uint64(tailRAPointer(cb)))
			slot, extra := 1, 0
			if escaped {
				slot, extra = 150, 300
			}
			root := tailRANativeFrameInvocationLocals(t, sig, f.a, ca, f.da, 1, true, extra, func(e *integerEmitter) {
				e.mode.referenceCalls, e.mode.foreignTableCalls = true, true
				e.a.Constant64(enc.X5, int64(tailRAPointer(d)))
				e.Store(int32(48+16*slot), 5, true)
				e.referenceCall(sig, key, 0, slot, func(i int) int32 { return int32(48 + 16*i) }, 16)
				tailRANativeSerialize(e, sig.Results, 0)
			})
			for _, code := range []*coreruntime.CodeBuffer{target.code, root.code} {
				if err := code.Seal(); err != nil {
					t.Fatal(err)
				}
			}
			prepare := func() {
				f.bind(t, f.e.StackLimit())
				f.a.SetRV64InvocationView(f.e.RV64InvocationViewPointer())
				binary.LittleEndian.PutUint64(f.args, 0xcafe123480000001)
				for i := range f.results {
					f.results[i] = 0xa5
				}
			}
			prepare()
			if err := f.e.CallPrepared(root.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 0x80000001 {
				t.Fatal("reference normal continuation", err)
			}
			original, originalContext, originalAnchor := bytes.Clone(d), bytes.Clone(cb), bytes.Clone(f.db)
			for _, fault := range []string{"key", "self", "code-alignment", "home-zero", "home-alignment", "home-identity", "context-zero", "context-alignment", "anchor-zero", "anchor-alignment", "anchor-context"} {
				t.Run(fault, func(t *testing.T) {
					copy(d, original)
					copy(cb, originalContext)
					copy(f.db, originalAnchor)
					switch fault {
					case "key":
						binary.LittleEndian.PutUint64(d[8:], key^1)
					case "self":
						binary.LittleEndian.PutUint64(d[24:], 0)
					case "code-alignment":
						binary.LittleEndian.PutUint64(d, binary.LittleEndian.Uint64(d)+4)
					case "home-zero":
						binary.LittleEndian.PutUint64(d[16:], abi.FuncRefLocalWrapperHomeTag)
					case "home-alignment":
						binary.LittleEndian.PutUint64(d[16:], binary.LittleEndian.Uint64(d[16:])+1)
					case "home-identity":
						binary.LittleEndian.PutUint64(d[16:], uint64(f.a.LinMemBase())|abi.FuncRefLocalWrapperHomeTag)
					case "context-zero":
						binary.LittleEndian.PutUint64(d[32:], 0)
					case "context-alignment":
						binary.LittleEndian.PutUint64(d[32:], uint64(tailRAPointer(cb))+1)
					case "anchor-zero":
						binary.LittleEndian.PutUint64(cb[16:], 0)
					case "anchor-alignment":
						binary.LittleEndian.PutUint64(cb[16:], uint64(tailRAPointer(f.db))+1)
					case "anchor-context":
						binary.LittleEndian.PutUint64(f.db[32:], uint64(tailRAPointer(ca)))
					}
					prepare()
					beforeArgs, beforeResults := bytes.Clone(f.args), bytes.Clone(f.results)
					err := f.e.CallPrepared(root.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
					var trap *coreruntime.TrapError
					want := coreruntime.TrapIndirectOutOfBounds
					if fault == "key" {
						want = coreruntime.TrapIndirectWrongSig
					}
					if !errors.As(err, &trap) || trap.Code != want || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 37 {
						t.Fatal("call_ref wrote before preflight or lost attribution", err)
					}
				})
			}
			copy(d, original)
			copy(cb, originalContext)
			copy(f.db, originalAnchor)
			if a := testing.AllocsPerRun(4, func() {
				prepare()
				if err := f.e.CallPrepared(root.code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
					panic(err)
				}
			}); a != 0 {
				t.Fatal("warm ordinary reference allocation", a)
			}
		})
	}
}
