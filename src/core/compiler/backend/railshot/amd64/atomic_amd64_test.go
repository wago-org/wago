//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	goruntime "runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wRuntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// runAtomicRMW supplies the memory directory used by indexed atomic lowering.
// The general runMemAmd64 helper only binds memory zero's legacy base/limit
// fields, while atomics use the indexed descriptor so growth remains visible.
func runAtomicRMW(t *testing.T, m *wasm.Module, setup func([]byte), args ...uint64) (uint64, []byte, error) {
	t.Helper()
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := wRuntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := wRuntime.NewJobMemory(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := wRuntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	lin := jm.LinearMemory()
	if setup != nil {
		setup(lin)
	}
	dir := ar.Alloc(24)
	binary.LittleEndian.PutUint64(dir[0:], uint64(uintptr(unsafe.Pointer(&lin[0]))))
	binary.LittleEndian.PutUint64(dir[8:], uint64(len(lin)))
	binary.LittleEndian.PutUint32(dir[16:], 1)
	jm.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir[0])))
	mem, entry, err := wRuntime.MapCode(cm.Code)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	defer wRuntime.Unmap(mem)
	serArgs, results := ar.Alloc(256), ar.Alloc(256)
	trap := ar.Alloc(wRuntime.TrapBufferBytes)
	for i, arg := range args {
		binary.LittleEndian.PutUint64(serArgs[i*8:], arg)
	}
	callErr := eng.Call(entry+uintptr(cm.Entry[0]), serArgs, lin, trap, results)
	return binary.LittleEndian.Uint64(results), append([]byte(nil), lin...), callErr
}

func atomicRMWModule(tb testing.TB, sub, align uint32, valueType wasm.ValType, dropped bool) *wasm.Module {
	tb.Helper()
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xfe}
	body = append(body, wasmtest.ULEB(sub)...)
	body = append(body, wasmtest.ULEB(align)...)
	body = append(body, wasmtest.ULEB(0)...)
	if dropped {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	sharedMemory := append([]byte{0x03}, wasmtest.ULEB(1)...)
	sharedMemory = append(sharedMemory, wasmtest.ULEB(1)...)
	resultTypes := []wasm.ValType{valueType}
	if dropped {
		resultTypes = nil
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, valueType}, resultTypes))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(sharedMemory)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode atomic module: %v", err)
	}
	return m
}

func atomicRMWLoopModule(tb testing.TB, sub uint32, dropped bool) *wasm.Module {
	tb.Helper()
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 loop counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x02, // local.get counter
		0x20, 0x00, // local.get iteration count
		0x4f,       // i32.ge_u
		0x0d, 0x01, // br_if block end
		0x41, 0x00, // atomic address
		0x20, 0x01, // atomic operand
		0xfe,
	}
	body = append(body, wasmtest.ULEB(sub)...)
	body = append(body, wasmtest.ULEB(2)...)
	body = append(body, wasmtest.ULEB(0)...)
	if dropped {
		body = append(body, 0x1a)
	}
	body = append(body,
		0x20, 0x02, // local.get counter
		0x41, 0x01, // i32.const 1
		0x6a,       // i32.add
		0x21, 0x02, // local.set counter
		0x0c, 0x00, // br loop
		0x0b, // end loop
		0x0b, // end block
		0x0b, // end function
	)
	entry := append(wasmtest.ULEB(uint32(len(body))), body...)
	sharedMemory := append([]byte{0x03}, wasmtest.ULEB(1)...)
	sharedMemory = append(sharedMemory, wasmtest.ULEB(1)...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(sharedMemory)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode atomic loop module: %v", err)
	}
	return m
}

func prepareAtomicLoop(b *testing.B, sub uint32, dropped bool) func() {
	b.Helper()
	const iterations = uint32(4096)
	m := atomicRMWLoopModule(b, sub, dropped)
	cm, err := CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	if cm.CodeImage != nil {
		b.Cleanup(func() { _ = cm.CodeImage.Close() })
	}
	eng, err := wRuntime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = eng.Close() })
	jm, err := wRuntime.NewJobMemory(1 << 16)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = jm.Close() })
	ar, err := wRuntime.NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ar.Close() })
	lin := jm.LinearMemory()
	dir := ar.Alloc(24)
	binary.LittleEndian.PutUint64(dir[0:], uint64(uintptr(unsafe.Pointer(&lin[0]))))
	binary.LittleEndian.PutUint64(dir[8:], uint64(len(lin)))
	binary.LittleEndian.PutUint32(dir[16:], 1)
	jm.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir[0])))
	mem, entry, err := wRuntime.MapCode(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { wRuntime.Unmap(mem) })
	args, results := ar.Alloc(256), ar.Alloc(256)
	binary.LittleEndian.PutUint32(args[0:], iterations)
	binary.LittleEndian.PutUint32(args[8:], 1)
	trap := ar.Alloc(wRuntime.TrapBufferBytes)
	call := entry + uintptr(cm.Entry[0])
	return func() {
		if err := eng.Call(call, args, lin, trap, results); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAtomicRMWSubLoop(b *testing.B) {
	call := prepareAtomicLoop(b, 0x25, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		call()
	}
	b.ReportMetric(4096, "rmw/op")
}

func BenchmarkAtomicRMWDroppedAddLoop(b *testing.B) {
	call := prepareAtomicLoop(b, 0x1e, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		call()
	}
	b.ReportMetric(4096, "rmw/op")
}

func BenchmarkAtomicRMWDroppedAndLoop(b *testing.B) {
	call := prepareAtomicLoop(b, 0x2c, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		call()
	}
	b.ReportMetric(4096, "rmw/op")
}

func BenchmarkCompileAtomicRMWSub(b *testing.B) {
	m := atomicRMWLoopModule(b, 0x25, true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
	}
}

func BenchmarkCompileAtomicRMWDroppedAnd(b *testing.B) {
	m := atomicRMWLoopModule(b, 0x2c, true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
	}
}

func TestAtomicSubUsesLockedXadd(t *testing.T) {
	cases := []struct {
		name       string
		sub        uint32
		align      uint32
		valueType  wasm.ValType
		size       int
		resultSize int
		operand    uint64
	}{
		{"i32", 0x25, 2, wasm.I32, 4, 4, 3},
		{"i64", 0x26, 3, wasm.I64, 8, 8, 3},
		{"i32-u8", 0x27, 0, wasm.I32, 1, 4, 0x103},
		{"i32-u16", 0x28, 1, wasm.I32, 2, 4, 0x10003},
		{"i64-u8", 0x29, 0, wasm.I64, 1, 8, 0x100000003},
		{"i64-u16", 0x2a, 1, wasm.I64, 2, 8, 0x100000003},
		{"i64-u32", 0x2b, 2, wasm.I64, 4, 8, 0x100000003},
	}
	const initial = uint64(0x1122334455667788)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := atomicRMWModule(t, tc.sub, tc.align, tc.valueType, false)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := stats.Funcs[0].Peephole["atomic-sub-xadd"]; got != 1 {
				t.Fatalf("atomic-sub-xadd = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
			}
			result, memory, err := runAtomicRMW(t, m, func(memory []byte) {
				binary.LittleEndian.PutUint64(memory, initial)
			}, 0, tc.operand)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			mask := ^uint64(0)
			if tc.size < 8 {
				mask = uint64(1)<<(tc.size*8) - 1
			}
			wantOld := initial & mask
			if tc.resultSize == 4 {
				wantOld = uint64(uint32(wantOld))
			}
			if result != wantOld {
				t.Fatalf("old value = %#x, want %#x", result, wantOld)
			}
			wantNew := (initial - tc.operand) & mask
			if got := binary.LittleEndian.Uint64(memory); got&mask != wantNew {
				t.Fatalf("memory after subtraction = %#x, want low bits %#x", got, wantNew)
			}
		})
	}
}

func TestDroppedAtomicRMWUsesLockedALU(t *testing.T) {
	cases := []struct {
		name      string
		sub       uint32
		align     uint32
		valueType wasm.ValType
		size      int
		initial   uint64
		value     uint64
		want      uint64
	}{
		{"add-i32", 0x1e, 2, wasm.I32, 4, 0x12345678, 0x10203040, 0x225486b8},
		{"sub-i64", 0x26, 3, wasm.I64, 8, 0x123456789abcdef0, 0x0102030405060708, 0x1132537495b6d7e8},
		{"sub-i32-u16", 0x28, 1, wasm.I32, 2, 0x12345678, 0x10003, 0x5675},
		{"and-i32", 0x2c, 2, wasm.I32, 4, 0x12345678, 0x00ff00ff, 0x00340078},
		{"and-i64-u8", 0x30, 0, wasm.I64, 1, 0x88, 0x10000000f, 0x08},
		{"or-i64", 0x34, 3, wasm.I64, 8, 0x1200000000000000, 0x00ff, 0x12000000000000ff},
		{"xor-i32-u16", 0x40, 1, wasm.I32, 2, 0x12345678, 0x00ffff00, 0x12cba978},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := atomicRMWModule(t, tc.sub, tc.align, tc.valueType, true)
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := stats.Funcs[0].Peephole["atomic-rmw-dead-result"]; got != 1 {
				t.Fatalf("atomic-rmw-dead-result = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
			}
			_, memory, err := runAtomicRMW(t, m, func(memory []byte) {
				binary.LittleEndian.PutUint64(memory, tc.initial)
			}, 0, tc.value)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			mask := ^uint64(0)
			if tc.size < 8 {
				mask = uint64(1)<<(tc.size*8) - 1
			}
			if got := binary.LittleEndian.Uint64(memory) & mask; got != tc.want&mask {
				t.Fatalf("memory after dropped RMW = %#x, want %#x", got, tc.want&mask)
			}
		})
	}
}

func TestAtomicRMWConcurrentUpdatesDoNotGetLost(t *testing.T) {
	const workers = 4
	const iterations = uint32(1001)
	savedProcs := goruntime.GOMAXPROCS(workers)
	defer goruntime.GOMAXPROCS(savedProcs)
	tests := []struct {
		name    string
		sub     uint32
		initial uint32
		value   func(worker int) uint32
		want    func() uint32
	}{
		{
			name:    "dropped-sub-locked-alu",
			sub:     0x25,
			initial: 0x12345678,
			value:   func(int) uint32 { return 1 },
			want:    func() uint32 { return 0x12345678 - workers*iterations },
		},
		{
			name:    "dropped-xor",
			sub:     0x3a,
			initial: 0,
			value:   func(worker int) uint32 { return 1 << worker },
			want:    func() uint32 { return 0x0f },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := atomicRMWLoopModule(t, tc.sub, true)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			codeMem, codeEntry, err := wRuntime.MapCode(cm.Code)
			if err != nil {
				t.Fatalf("map code: %v", err)
			}
			defer wRuntime.Unmap(codeMem)

			sharedOwner, err := wRuntime.NewJobMemory(1 << 16)
			if err != nil {
				t.Fatal(err)
			}
			defer sharedOwner.Close()
			sharedMemory := sharedOwner.LinearMemory()
			binary.LittleEndian.PutUint32(sharedMemory, tc.initial)
			sharedBase := uintptr(unsafe.Pointer(&sharedMemory[0]))

			type workerState struct {
				engine *wRuntime.Engine
				memory *wRuntime.JobMemory
				arena  *wRuntime.Arena
				args   []byte
				out    []byte
				trap   []byte
			}
			states := make([]workerState, workers)
			for i := range states {
				states[i].engine, err = wRuntime.NewEngine()
				if err != nil {
					t.Fatal(err)
				}
				defer states[i].engine.Close()
				states[i].memory, err = wRuntime.NewJobMemory(1 << 16)
				if err != nil {
					t.Fatal(err)
				}
				defer states[i].memory.Close()
				states[i].arena, err = wRuntime.NewArena(4096)
				if err != nil {
					t.Fatal(err)
				}
				defer states[i].arena.Close()
				dir := states[i].arena.Alloc(24)
				binary.LittleEndian.PutUint64(dir, uint64(sharedBase))
				binary.LittleEndian.PutUint64(dir[8:], uint64(len(sharedMemory)))
				binary.LittleEndian.PutUint32(dir[16:], 1)
				states[i].memory.SetMemoryDirPtr(uintptr(unsafe.Pointer(&dir[0])))
				states[i].args = states[i].arena.Alloc(256)
				binary.LittleEndian.PutUint32(states[i].args, iterations)
				binary.LittleEndian.PutUint32(states[i].args[8:], tc.value(i))
				states[i].out = states[i].arena.Alloc(256)
				states[i].trap = states[i].arena.Alloc(wRuntime.TrapBufferBytes)
			}

			entry := codeEntry + uintptr(cm.Entry[0])
			var wg sync.WaitGroup
			errs := make([]error, workers)
			for i := range states {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					errs[i] = states[i].engine.Call(entry, states[i].args, states[i].memory.LinearMemory(), states[i].trap, states[i].out)
				}(i)
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					t.Fatalf("concurrent call: %v", err)
				}
			}
			if got, want := binary.LittleEndian.Uint32(sharedMemory), tc.want(); got != want {
				t.Fatalf("shared word after concurrent RMW = %#x, want %#x", got, want)
			}
		})
	}
}
