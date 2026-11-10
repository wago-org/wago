//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailHostRV64DynamicTargetParkReentry(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, mode := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			owned, sparse := mode[0], mode[1]
			for _, counts := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {4, 4}, {64, 64}} {
				t.Run(fmt.Sprintf("sparse%t-shared%t-owned%t-params%d-results%d", sparse, shared, owned, counts[0], counts[1]), func(t *testing.T) {
					scalar := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
					sig := wasm.CompType{Kind: wasm.CompFunc, Params: make([]wasm.ValType, counts[0]), Results: make([]wasm.ValType, counts[1])}
					for i := range sig.Params {
						sig.Params[i] = scalar[i%4]
					}
					for i := range sig.Results {
						sig.Results[i] = scalar[i%4]
					}
					f, view := tailInvocationFixtureForTest(t, shared, counts[0], counts[1])
					a := tailRANativeFrameInvocation(t, &sig, f.a, f.ca, f.da, 0, true, func(e *integerEmitter) {
						// A bounded native countdown reaches the host through the same
						// frozen dispatcher, exercising record reuse across target kinds.
						e.a.Constant64(enc.X29, int64(tailRAPointer(f.witness)))
						e.a.Ld(enc.X5, enc.X29, 0)
						host := e.a.BranchLong(enc.EQ, enc.X5, enc.Zero)
						e.a.Addi(enc.X5, enc.X5, -1)
						e.a.Sd(enc.X5, enc.X29, 0)
						tailDynamicReferenceForTest(e, tailRAPointer(f.witness)+232)
						e.tailRAProofCall(0, &sig, 0, func(i int) int32 { return int32(48 + 16*i) })
						if err := e.a.PatchLong(host, int64(e.a.Len())); err != nil {
							t.Fatal(err)
						}
						tailDynamicReferenceForTest(e, tailRAPointer(f.witness)+224)
						e.tailRAProofCall(0, &sig, 0, func(i int) int32 { return int32(48 + 16*i) })
					})
					empty := wasm.CompType{Kind: wasm.CompFunc}
					leaf := tailRANativeFrameInvocation(t, &empty, f.b, f.cb, f.db, 1, true, func(*integerEmitter) {})
					index := uint32(37)
					if owned {
						index |= 0x80000000
					}
					code, certificate, err := NativeTailHostThunkWithCertificate(index, counts[0], counts[1], owned)
					if err != nil {
						t.Fatal(err)
					}
					host := tailRAFunction{code: tailRAProofBuffer(t, code), snapshot: code, signature: &sig, job: f.b, context: f.cb, directory: f.db, info: FunctionInfo{CodeBytes: len(code)}}
					host, _ = tailPublishedFunctionForTest(t, host)
					oa := tailDynamicOwnerForTest(t, []tailRAFunction{a})
					ob := tailDynamicOwnerForTest(t, []tailRAFunction{host, leaf})
					a = oa.functions[0]
					host, leaf = ob.functions[0], ob.functions[1]
					f.da, f.db = oa.descriptors, ob.descriptors
					binary.LittleEndian.PutUint64(ob.directory[32+24:], abi.RV64TailTargetsHost)
					binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
					binary.LittleEndian.PutUint64(f.witness[232:], uint64(tailRAPointer(oa.descriptors)+40))
					a, _ = tailPublishedFunctionForTest(t, a)
					leaf, _ = tailPublishedFunctionForTest(t, leaf)
					hostTarget, err := NewNativeTailHostFunction(host.published, certificate, f.b, f.cb, ob.descriptors, 0, index, owned, sig)
					if err != nil {
						t.Fatal(err)
					}
					caller, err := NewNativeTailDefinedOwner(a.published, f.a, f.ca, oa.descriptors, oa.directory, []int{0}, []wasm.CompType{sig})
					if err != nil {
						t.Fatal(err)
					}
					trampoline, err := coreruntime.HostCallTrampolinePointer()
					if err != nil {
						t.Fatal(err)
					}
					imports := []NativeTailImport{{Image: host.published, CodeBytes: len(code), Host: hostTarget}}
					var target *NativeTailOwner
					if sparse {
						clear(ob.descriptors[80:120])
						clear(ob.directory[64:96])
						target, err = NewNativeTailSparseImportedOwner(leaf.published, f.b, f.cb, ob.descriptors, ob.directory, []int{0}, []*wasm.CompType{&sig, nil}, imports, trampoline)
					} else {
						target, err = NewNativeTailImportedOwner(leaf.published, f.b, f.cb, ob.descriptors, ob.directory, []int{0}, []wasm.CompType{sig, empty}, imports, trampoline)
					}
					if err != nil {
						t.Fatal(err)
					}
					root := tailRANativeRoot(t, a, f.witness)
					if err := root.Seal(); err != nil {
						t.Fatal(err)
					}
					site := tailDynamicSiteForTest(t, a, nil, a.info.FrameBytes-8)
					site.dynamic = nil
					site.Continuations = append(site.Continuations, NativeTailCodeRange{root.Base(), uintptr(len(root.Bytes()))})
					site, err = NativeTailDefinedSite(site, a.info.FrameBytes-8, []*NativeTailOwner{caller, target})
					if err != nil {
						t.Fatal(err)
					}
					dispatcher := tailDynamicBindForTest(t, site, len(a.snapshot)+len(leaf.snapshot)+len(code)+len(root.Bytes()))
					if err := dispatcher.Bind(caller); err != nil {
						t.Fatal(err)
					}
					nested, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
					if err != nil {
						t.Fatal(err)
					}
					defer nested.Close()
					arena, err := coreruntime.NewArena(128)
					if err != nil {
						t.Fatal(err)
					}
					defer arena.Close()
					nestedTrap, nestedResult := arena.Alloc(coreruntime.TrapBufferBytes), arena.Alloc(8)
					nestedSig := wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I64}}
					nestedFn := tailRANativeFrameInvocation(t, &nestedSig, f.a, f.ca, f.da, 2, true, func(e *integerEmitter) {
						e.restoreCallTransport()
						e.a.Constant64(enc.X5, 49)
						e.a.Sd(enc.X5, enc.X13, 0)
					})
					if err := nestedFn.code.Seal(); err != nil {
						t.Fatal(err)
					}
					input := make([]byte, len(f.args))
					expectedInput := make([]uint64, counts[0])
					expectedOutput := make([]uint64, counts[1])
					value := func(i int) uint64 { return 0xcafe12347f800123 ^ uint64(i)*0x101010101 }
					for i, typ := range sig.Params {
						v := value(i)
						binary.LittleEndian.PutUint64(input[i*8:], v)
						if swarClassOf(typ).narrow() {
							v = uint64(uint32(v))
						}
						expectedInput[i] = v
					}
					for i, typ := range sig.Results {
						v := value(i + 7)
						if swarClassOf(typ).narrow() {
							v = uint64(uint32(v))
						}
						expectedOutput[i] = v
					}
					exit, err := nativeTailExitPointer()
					if err != nil {
						t.Fatal(err)
					}
					calls := 0
					callback := func(ctrl uintptr, actual uint32, in, out []uint64) {
						calls++
						if actual != index || len(in) != len(expectedInput) || len(out) != len(expectedOutput) || ctrl < f.e.StackLimit() || ctrl+HostFixedFrameBytes > f.e.StackTop() {
							panic("dynamic host resolver/record extent")
						}
						for i, v := range expectedInput {
							if in[i] != v {
								panic("dynamic host narrow input")
							}
						}
						if nativeImportWordRV64(f.b.LinMemBase(), abi.RV64NativeContextOffset) != uint64(tailRAPointer(f.cb)) {
							panic("dynamic host owner context")
						}
						activeView := uintptr(nativeImportWordRV64(f.b.LinMemBase(), abi.RV64InvocationViewOffset))
						if activeView == view || activeView < f.e.StackLimit() || activeView+abi.RV64InvocationViewBytes > f.e.StackTop() {
							panic("dynamic host activation view")
						}
						if nativeImportWordRV64(activeView, -abi.RV64InvocationFirstRAOffset) != uint64(exit) {
							panic("dynamic host common-exit return identity")
						}
						var before [abi.RV64InvocationViewBytes / 8]uint64
						for i := range before {
							before[i] = nativeImportWordRV64(activeView, -int32(i*8))
						}
						f.a.BindInstanceContextBytes(f.ca)
						f.a.SetRV64NativeContext(tailRAPointer(f.ca))
						f.a.SetRV64InvocationView(nested.RV64InvocationViewPointer())
						f.a.SetCustomCtx(tailRAPointer(f.ctrl))
						f.a.SetStackFence(nested.StackLimit())
						if err := f.a.RebindTrapCell(nestedTrap); err != nil {
							panic(err)
						}
						if err := nested.CallPrepared(nestedFn.code.Base(), nil, f.a.LinMemBase(), nestedTrap, nestedResult); err != nil || binary.LittleEndian.Uint64(nestedResult) != 49 {
							panic("dynamic host nested native entry")
						}
						runtime.GC()
						for i, word := range before {
							if nativeImportWordRV64(activeView, -int32(i*8)) != word {
								panic("nested entry changed suspended host view")
							}
						}
						f.b.BindInstanceContextBytes(f.cb)
						f.b.SetRV64NativeContext(tailRAPointer(f.cb))
						f.b.SetRV64InvocationView(activeView)
						f.b.SetCustomCtx(ctrl)
						for i := range out {
							out[i] = value(i + 7)
						}
					}
					prepare := func() {
						f.bind(t, f.e.StackLimit())
						f.a.SetRV64InvocationView(view)
						copy(f.args, input)
						for i := range f.results {
							f.results[i] = 0xa5
						}
					}
					for run := range 3 {
						prepare()
						if run == 1 && counts[0] == 4 {
							binary.LittleEndian.PutUint64(f.witness, 2049)
						}
						if err := f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, callback); err != nil {
							t.Fatal("dynamic host tail return", err)
						}
						if binary.LittleEndian.Uint64(f.witness) != 0 {
							t.Fatal("native countdown failed to reach host")
						}
						for i, v := range expectedOutput {
							if got := binary.LittleEndian.Uint64(f.results[i*8:]); got != v {
								t.Fatal("dynamic host normalized result", i, got, v)
							}
						}
						f.assertReturned(t)
					}
					if calls != 3 {
						t.Fatal("host callback count", calls)
					}
					// A live row's kind must match its frozen host target before any source
					// argument publication, even though its code/home/context still match.
					binary.LittleEndian.PutUint64(ob.directory[32+24:], 0)
					prepare()
					beforeArgs, beforeResults := bytes.Clone(f.args), bytes.Clone(f.results)
					err = f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, callback)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapCalledFnNotLinked || calls != 3 || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) {
						t.Fatal("dynamic host row classification guard", err, calls)
					}
					binary.LittleEndian.PutUint64(ob.directory[32+24:], abi.RV64TailTargetsHost)
					if sparse {
						for _, rewritten := range []bool{false, true} {
							beforeCalls := calls
							desc, row := ob.descriptors[80:120], ob.directory[64:96]
							if rewritten {
								copy(desc, ob.descriptors[40:80])
								copy(row, ob.directory[32:64])
								binary.LittleEndian.PutUint64(desc[24:], uint64(tailRAPointer(desc)))
							}
							binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(desc)))
							prepare()
							beforeArgs, beforeResults := bytes.Clone(f.args), bytes.Clone(f.results)
							err = f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, callback)
							clear(desc)
							clear(row)
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapCalledFnNotLinked || calls != beforeCalls || !bytes.Equal(f.args, beforeArgs) || !bytes.Equal(f.results, beforeResults) {
								t.Fatal("internal frame selected as host", rewritten, err, calls)
							}
							f.assertCanaries(t)
							binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+40))
							prepare()
							if err := f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, callback); err != nil {
								t.Fatal("host reuse after rejected hole", err)
							}
							f.assertReturned(t)
							if calls != beforeCalls+1 {
								t.Fatal("callback reuse count", calls, beforeCalls)
							}
						}
					}
				})
			}
		}
	}
}
