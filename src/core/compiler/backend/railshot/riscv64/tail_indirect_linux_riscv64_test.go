//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func localIndirectNativeImage(t *testing.T, m *wasm.Module, vector bool) *swarModuleImage {
	t.Helper()
	stage, err := CompileModuleWith(m, localTailOptions())
	if err != nil {
		t.Fatal(err)
	}
	var image *swarModuleImage
	if vector {
		image, err = compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true, localTailCalls: true}, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, indexedMemories: true})
	} else {
		opts := localTailOptions()
		opts.production, opts.publicStarts = true, true
		var c *Compiled
		c, err = compileDecodedModule(m, scalarRegisters[:], opts)
		if err == nil {
			table, _ := m.TableType(0)
			var elements []swarElement
			elements, err = swarModuleElements(m.Elements)
			image = &swarModuleImage{code: c.code, functions: c.functions, table: &table, elements: elements, functionKeys: make([]uint64, len(c.functions))}
			for i := range c.functions {
				image.functionKeys[i] = m.StructuralTypeKey(uint32(i))
			}
		}
	}
	if err != nil || image == nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stage.Code, image.code) {
		t.Fatal("private owned-table image differs from production staging")
	}
	return image
}

// Scalar production may retain the small 16-byte shared leaf frame. The SWAR
// execution owner requires 48-byte frames throughout its own profile, so this
// fixture binds the same bounded local descriptor ABI without changing that
// owner's admission. Code is sealed before any entry is bound into basedata.
type localIndirectOwner struct {
	*swarTableOwner
	tableStorage, functionStorage []byte
}

func (owner *localIndirectOwner) Close() error {
	err := owner.swarTableOwner.Close()
	if err == nil {
		owner.tableStorage, owner.functionStorage = nil, nil
	}
	return err
}

func localIndirectNativeOwner(t *testing.T, image *swarModuleImage, job *coreruntime.JobMemory, code *coreruntime.CodeBuffer) *localIndirectOwner {
	t.Helper()
	if len(image.functions) == 0 || len(image.functions) > MaxFunctions || !bytes.Equal(image.code, code.Bytes()) || image.table == nil || image.table.Limits.Min != 3 || image.table.Limits.Max != 3 {
		t.Fatal("bounded owned-table fixture")
	}
	if err := code.Seal(); err != nil {
		t.Fatal(err)
	}
	ar, err := coreruntime.NewArena(8 + 3*32 + (len(image.functions)+1)*40 + 8)
	if err != nil {
		t.Fatal(err)
	}
	table, functions, context := ar.Alloc(8+3*32), ar.Alloc((len(image.functions)+1)*40), ar.Alloc(8)
	clear(table)
	clear(functions)
	clear(context)
	binary.LittleEndian.PutUint32(table, 3)
	binary.LittleEndian.PutUint32(table[4:], 3)
	identity := uint64(thunkPointerRV64(context))
	binary.LittleEndian.PutUint64(functions[32:], identity)
	for i, info := range image.functions {
		if info.Entry < 0 || info.Entry&15 != 0 || info.Entry+info.CodeBytes > len(image.code) || !info.mode.production || info.FrameHeaderBytes != 16 && info.FrameHeaderBytes != callFrameHeader {
			t.Fatal("framed owned-table entry", i)
		}
		row := functions[(i+1)*40:]
		binary.LittleEndian.PutUint64(row, uint64(code.Base())+uint64(info.Entry))
		binary.LittleEndian.PutUint64(row[8:], image.functionKeys[i])
		binary.LittleEndian.PutUint64(row[16:], uint64(job.LinMemBase())|abi.FuncRefLocalWrapperHomeTag)
		binary.LittleEndian.PutUint64(row[24:], uint64(thunkPointerRV64(row)))
		binary.LittleEndian.PutUint64(row[32:], identity)
	}
	for _, element := range image.elements {
		if element.kind != wasm.ElemActive || element.offset != 0 || len(element.functions) != 2 {
			t.Fatal("fixture element profile")
		}
		for i, index := range element.functions {
			copy(table[8+i*32:], functions[(int(index)+1)*40:(int(index)+1)*40+32])
		}
	}
	owner := &swarTableOwner{arena: ar, job: job, code: code, table: thunkPointerRV64(table), functions: thunkPointerRV64(functions)}
	job.SetTablePtr(owner.table)
	job.SetFuncRefDesc(owner.functions)
	return &localIndirectOwner{swarTableOwner: owner, tableStorage: table, functionStorage: functions}
}

func localIndirectCountdown(vector, mutual bool, index byte, locals int) localTailFixture {
	params := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		params = append(params, wasm.V128)
	}
	results := append([]wasm.ValType(nil), params[1:]...)
	results = append(results, wasm.I32)
	body := []byte{0x20, 0, 0x45, 0x04, 0x40}
	for i := 1; i < len(params); i++ {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x20, 0, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b)
	for i := 1; i < len(params); i++ {
		body = append(body, 0x20, byte(i))
	}
	if !mutual {
		index = 0
	}
	body = append(body, 0x41, index, 0x13, 0, 0, 0x0b)
	return localTailFixture{params: params, results: results, body: body, locals: locals}
}

func TestLocalIndirectTailRV64ConstantStackAndRawCarriers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mutual := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-mutual%t", vector, mutual), func(t *testing.T) {
				e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
				a, b := localIndirectCountdown(vector, mutual, 1, 0), localIndirectCountdown(vector, mutual, 0, 60)
				image := localIndirectNativeImage(t, localTailDecode(t, localIndirectTailSource(vector, a, b)), vector)
				ar, err := coreruntime.NewArena(16)
				if err != nil {
					t.Fatal(err)
				}
				defer ar.Close()
				witness := ar.Alloc(16)
				probe := localTailSPProbe(thunkPointerRV64(witness))
				code, entries := localTailInstrument(t, image.code, image.functions, probe)
				image.code = code
				for i := range image.functions {
					image.functions[i].Entry = entries[i]
					image.functions[i].CodeBytes += len(probe)
				}
				buffer := tailRAProofBuffer(t, code)
				owner := localIndirectNativeOwner(t, image, j, buffer)
				defer owner.Close()
				input := []uint64{0, 0xdeadbeef7f800001, 0xfedcba9876543210, 0x7ff0000000000001}
				want := []uint64{0x7f800001, input[2], input[3]}
				if vector {
					input = append(input, 0x8123456789abcdef, 0xfedcba9876543210)
					want = append(want, input[4], input[5])
				}
				want = append(want, 0)
				entry := buffer.Base() + uintptr(entries[0])
				var observed uint64
				invoke := func(depth uint32) {
					binary.LittleEndian.PutUint64(witness, ^uint64(0))
					binary.LittleEndian.PutUint64(witness[8:], 0)
					input[0] = 0xfeedface00000000 | uint64(depth)
					for i, value := range input {
						binary.LittleEndian.PutUint64(args[i*8:], value)
					}
					for i := range results {
						results[i] = 0xa5
					}
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
						t.Fatal(depth, err)
					}
					for i, value := range want {
						if binary.LittleEndian.Uint64(results[i*8:]) != value {
							t.Fatal("raw result", depth, i)
						}
					}
					if !bytes.Equal(results[len(want)*8:], bytes.Repeat([]byte{0xa5}, len(results)-8*len(want))) {
						t.Fatal("result tail overwrite")
					}
					lo, hi := binary.LittleEndian.Uint64(witness), binary.LittleEndian.Uint64(witness[8:])
					if lo == 0 || lo == ^uint64(0) || lo != hi || observed != 0 && observed != lo {
						t.Fatal("indirect tails grew stack", depth, lo, hi, observed)
					}
					observed = lo
				}
				for _, depth := range []uint32{0, 1, 33, 20000, 2} {
					invoke(depth)
				}
				if n := testing.AllocsPerRun(10, func() {
					input[0] = 41
					for i, value := range input {
						binary.LittleEndian.PutUint64(args[i*8:], value)
					}
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm indirect tail allocations", n)
				}
			})
		}
	}
}

func TestLocalIndirectTailRV64GuardsBeforeTransportWrites(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
			// The source preserves an unrelated operand prefix, drops it when
			// tailing, and changes the target parameter count from two to one.
			caller := localTailFixture{params: []wasm.ValType{wasm.I32, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 99, 0x20, 0, 0x20, 1, 0x13, 1, 0, 0x0b}}
			leaf := localTailFixture{params: []wasm.ValType{wasm.I32}, results: caller.results, body: []byte{0x20, 0, 0x0b}}
			image := localIndirectNativeImage(t, localTailDecode(t, localIndirectTailSource(vector, caller, leaf)), vector)
			buffer := tailRAProofBuffer(t, image.code)
			owner := localIndirectNativeOwner(t, image, j, buffer)
			var err error
			defer owner.Close()
			table := owner.tableStorage
			directory := owner.functionStorage
			original := bytes.Clone(table)
			for _, name := range []string{"valid-dirty-index", "null", "bounds", "signature", "context", "tag", "unaligned"} {
				copy(table, original)
				clear(trap)
				index := uint64(0xfeedface00000001)
				wantTrap := coreruntime.TrapIndirectOutOfBounds
				switch name {
				case "null":
					index = 0xfeedface00000002
				case "bounds":
					index = 0xfeedfaceffffffff
				case "signature":
					index = 0xfeedface00000000
					wantTrap = coreruntime.TrapIndirectWrongSig
				case "context":
					binary.LittleEndian.PutUint64(directory[112:], binary.LittleEndian.Uint64(directory[112:])+8)
				case "tag":
					binary.LittleEndian.PutUint64(table[8+32+16:], binary.LittleEndian.Uint64(table[8+32+16:])^abi.FuncRefLocalWrapperHomeTag)
				case "unaligned":
					binary.LittleEndian.PutUint64(table[8+32:], binary.LittleEndian.Uint64(table[8+32:])+4)
				}
				binary.LittleEndian.PutUint64(args, 0xdeadbeef80000001)
				binary.LittleEndian.PutUint64(args[8:], index)
				before := bytes.Clone(args)
				for i := range results {
					results[i] = 0xa5
				}
				err = e.CallPrepared(buffer.Base(), args, j.LinMemBase(), trap, results)
				if name == "valid-dirty-index" {
					if err != nil || binary.LittleEndian.Uint64(results) != 0x80000001 {
						t.Fatal(name, err)
					}
				} else {
					var tr *coreruntime.TrapError
					if !errors.As(err, &tr) || tr.Code != wantTrap || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 0 || tr.Frames[0].ProgramCounter != 7 || !bytes.Equal(args, before) || !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
						t.Fatal("guard must precede exchange writes", name, err)
					}
				}
				if name == "context" {
					binary.LittleEndian.PutUint64(directory[112:], binary.LittleEndian.Uint64(directory[112:])-8)
				}
			}
		})
	}
}

func TestLocalIndirectTailRV64OriginalLinkedContinuationAndVoid(t *testing.T) {
	for _, vector := range []bool{false, true} {
		funcs := []localTailFixture{
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0, 0x10, 1, 0x20, 0, 0x6a, 0x0b}},
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x10, 3, 0x1a, 0x20, 0, 0x41, 1, 0x13, 2, 0, 0x0b}},
			{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}},
			{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 9, 0x0b}},
		}
		m := localTailDecode(t, localIndirectTailSource(vector, funcs...))
		m.Elements[0].Kind.Funcs[1] = 2
		image := localIndirectNativeImage(t, m, vector)
		e, j, args, results, trap, _ := thunkFixtureRV64(t, 256)
		buffer := tailRAProofBuffer(t, image.code)
		owner := localIndirectNativeOwner(t, image, j, buffer)
		var err error
		ar, err := coreruntime.NewArena(256)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ar.Close() })
		witness := ar.Alloc(256)
		root := tailRANativeRoot(t, tailRAFunction{code: buffer, info: image.functions[0]}, witness)
		if err = root.Seal(); err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint64(args, 0xdeadbeef00000029)
		if err = e.CallPrepared(root.Base(), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 83 {
			t.Fatal("saved linked RA after indirect tail", vector, err)
		}
		if !bytes.Equal(witness[96:128], witness[192:224]) || !bytes.Equal(witness[128:160], witness[160:192]) {
			t.Fatal("indirect tail changed Go GP/TP/CTXT/g")
		}
		if err = owner.Close(); err != nil {
			t.Fatal(err)
		}
		void := localTailDecode(t, localIndirectTailSource(vector, localTailFixture{body: []byte{0x41, 1, 0x13, 0, 0, 0x0b}}, localTailFixture{body: []byte{0x0b}}))
		image = localIndirectNativeImage(t, void, vector)
		buffer = tailRAProofBuffer(t, image.code)
		owner = localIndirectNativeOwner(t, image, j, buffer)
		defer owner.Close()
		if err = e.CallPrepared(buffer.Base(), nil, j.LinMemBase(), trap, nil); err != nil {
			t.Fatal("zero directions", vector, err)
		}
	}
}

func TestLocalIndirectTailRV64ActiveCycleInterruptAndReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			e, _, args, results, trap, _ := thunkFixtureRV64(t, 256)
			j, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			j.SetStackFence(e.StackLimit())
			if err = j.RebindTrapCell(trap); err != nil {
				t.Fatal(err)
			}
			funcs := []localTailFixture{
				{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x41, 0, 0x13, 0, 0, 0x0b}},
				{results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 41, 0x0b}},
			}
			m, err := wasm.DecodeModule(localIndirectTailSource(vector, funcs...))
			if err != nil {
				t.Fatal(err)
			}
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			image := localIndirectNativeImage(t, m, vector)
			buffer := tailRAProofBuffer(t, image.code)
			owner := localIndirectNativeOwner(t, image, j, buffer)
			defer owner.Close()
			for i := range results {
				results[i] = 0xa5
			}
			done := make(chan error, 1)
			go func() { done <- e.CallPrepared(buffer.Base(), args, j.LinMemBase(), trap, results) }()
			word := (*uint32)(unsafe.Pointer(&j.CurrentBytes()[0]))
			deadline, started := time.Now().Add(2*time.Second), false
			for time.Now().Before(deadline) {
				if atomic.LoadUint32(word) != 0 {
					started = true
					break
				}
				select {
				case err := <-done:
					t.Fatal("cycle returned before cancellation", err)
				default:
				}
				time.Sleep(100 * time.Microsecond)
			}
			stop := coreruntime.RequestInterruptAsync(trap)
			err = <-done
			stop()
			var tr *coreruntime.TrapError
			if !started || !errors.As(err, &tr) || tr.Code != coreruntime.TrapInterrupted || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 0 || tr.Frames[0].ProgramCounter != 1 {
				t.Fatal("active indirect tail entry poll", err)
			}
			if !bytes.Equal(results, bytes.Repeat([]byte{0xa5}, len(results))) {
				t.Fatal("cancelled indirect tail committed results")
			}
			clear(trap)
			if err = e.CallPrepared(buffer.Base()+uintptr(image.functions[1].Entry), args, j.LinMemBase(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != 41 {
				t.Fatal("cold cancellation reuse", err)
			}
		})
	}
}
