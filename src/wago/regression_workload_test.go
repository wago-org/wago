//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	wago "github.com/wago-org/wago/src/wago"
)

// TestRuntimeRegressionPortRustFannkuchExecution adds an execution oracle to Regression's
// compile-only misc_testsuite/rust_fannkuch.wast regression.
func TestRuntimeRegressionPortRustFannkuchExecution(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	data, err := os.ReadFile("../../tests/regressions/runtime/core/rust_fannkuch/commands.0.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := wago.Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := wago.Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	for _, tc := range []struct {
		n    int32
		want int32
	}{
		{n: 3, want: 2},
		{n: 5, want: 11},
		{n: 7, want: 228},
		{n: 9, want: 8629},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := in.Call(ctx, "run_fannkuch", wago.ValueI32(tc.n))
		cancel()
		if err != nil || len(got) != 1 || got[0].Type() != wago.ValI32 || got[0].I32() != tc.want {
			t.Fatalf("run_fannkuch(%d) = %v, %v; want %d", tc.n, got, err, tc.want)
		}
	}
}

// TestRuntimeRegressionPortEmbenchenExecution turns Regression's four compile/link-only
// Emscripten fixtures into end-to-end execution tests. The shim implements the
// small legacy env surface these fixed workloads use, including writev output.
func TestRuntimeRegressionPortEmbenchenExecution(t *testing.T) {
	for _, tc := range []struct {
		name       string
		wantResult int32
		wantLen    int
		wantSHA256 string
	}{
		{name: "embenchen_fannkuch", wantLen: 321, wantSHA256: "7bc936836cb617d9902cc8322e06e13f2f68ede5632e08fc764e4d4d0432f222"},
		{name: "embenchen_fasta", wantLen: 560, wantSHA256: "4379c3244bbf2492f835399c85788cfb315d467a1c4c35fe981fd807b373745b"},
		{name: "embenchen_ifs", wantResult: 59695240, wantLen: 3, wantSHA256: "dc51b8c96c2d745df3bd5590d990230a482fd247123599548e0632fdbf97fc22"},
		{name: "embenchen_primes", wantLen: 19, wantSHA256: "617eba5df661b7abf7aca6cbb07992f5a1f3660c6068be2a3becb4fa2fd76fc7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runRegressionIsolatedPortTest(t) {
				return
			}
			result, output := runRegressionEmbenchen(t, tc.name)
			if result != tc.wantResult {
				t.Fatalf("_main result = %d, want %d", result, tc.wantResult)
			}
			if len(output) != tc.wantLen {
				t.Fatalf("captured output length = %d, want %d", len(output), tc.wantLen)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(output)); got != tc.wantSHA256 {
				t.Fatalf("captured output SHA-256 = %s, want %s", got, tc.wantSHA256)
			}
		})
	}
}

func runRegressionEmbenchen(t *testing.T, name string) (int32, []byte) {
	t.Helper()
	rt := wago.NewRuntime()
	t.Cleanup(func() { _ = rt.Close() })
	data, err := os.ReadFile("../../tests/regressions/runtime/core/" + name + "/commands.1.wasm")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mod.Compiled().Close() })
	if !mod.Compiled().MemHasMax || mod.Compiled().MemMinPages != 2 || mod.Compiled().MemMaxPages != 2 {
		t.Fatalf("Emscripten workload memory limits = %d..%d (hasMax=%v), want 2..2", mod.Compiled().MemMinPages, mod.Compiled().MemMaxPages, mod.Compiled().MemHasMax)
	}
	memory, err := wago.NewMemory(mod.Compiled().MemMinPages, mod.Compiled().MemMaxPages)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memory.Close() })

	var table *wago.Table
	for _, spec := range mod.Imports() {
		if spec.Kind == wago.ImportTable {
			table, err = wago.NewTable(uint32(spec.Min), uint32(spec.Max))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if table == nil {
		t.Fatal("Emscripten workload has no table import")
	}
	t.Cleanup(func() { _ = table.Close() })

	maxData := uint32(0)
	for _, data := range mod.Compiled().Data {
		end := data.Offset.Base + uint32(len(data.Bytes))
		if end > maxData {
			maxData = end
		}
	}
	dynamicTopPtr := alignRegressionEmbenchen(maxData, 4)
	argv := dynamicTopPtr + 96
	prog := argv + 20
	arg := prog + 20
	stackTop := alignRegressionEmbenchen(arg+256, 256)
	const stackMax = uint32(32768)
	if stackTop >= stackMax {
		t.Fatalf("Emscripten fixture static data leaves no test stack: top=%d max=%d", stackTop, stackMax)
	}
	binary.LittleEndian.PutUint32(memory.Bytes()[dynamicTopPtr:], stackMax)

	imports := wago.Imports{
		"env.DYNAMICTOP_PTR": wago.GlobalImport{Type: wago.ValI32, Bits: uint64(dynamicTopPtr)},
		"env.STACKTOP":       wago.GlobalImport{Type: wago.ValI32, Bits: uint64(stackTop)},
		"env.STACK_MAX":      wago.GlobalImport{Type: wago.ValI32, Bits: uint64(stackMax)},
		"env.memoryBase":     wago.GlobalImport{Type: wago.ValI32},
		"env.tableBase":      wago.GlobalImport{Type: wago.ValI32},
		"env.memory":         memory,
		"env.table":          table,
	}
	var output []byte
	for _, spec := range mod.Imports() {
		if spec.Kind != wago.ImportFunc {
			continue
		}
		importName := spec.Name
		imports[spec.Key()] = wago.HostFunc(func(m wago.HostModule, params, results []uint64) {
			switch importName {
			case "abort", "_abort", "_pthread_cleanup_pop", "_pthread_cleanup_push", "___setErrNo":
				// The upstream WAST env provider defines these as no-op functions.
			case "enlargeMemory", "abortOnCannotGrowMemory", "___syscall6", "___syscall140":
				// The upstream provider defines these result-bearing stubs as
				// unreachable. If a fixed workload starts calling one, do not
				// silently manufacture a zero result and weaken the oracle.
				panic(fmt.Errorf("unexpected Emscripten host call %q", importName))
			case "___syscall54":
				// These historical Emscripten workloads issue ioctl(fd,
				// TCGETS, ...) while initializing stdout. Model only that known
				// terminal query; any other syscall54 shape remains fail-closed.
				mem := m.Memory()
				args := uint32(params[1])
				if uint32(params[0]) != 54 || uint64(args)+12 > uint64(len(mem)) {
					panic(fmt.Errorf("unexpected Emscripten syscall54 parameters %v", params))
				}
				fd := binary.LittleEndian.Uint32(mem[args:])
				op := binary.LittleEndian.Uint32(mem[args+4:])
				dst := binary.LittleEndian.Uint32(mem[args+8:])
				if (fd != 1 && fd != 2) || op != 21505 {
					panic(fmt.Errorf("unexpected Emscripten ioctl fd=%d op=%d", fd, op))
				}
				_ = regressionEmbenchenSlice(mem, dst, 36, "ioctl TCGETS destination")
				results[0] = 0
			case "getTotalMemory":
				results[0] = wago.I32(int32(len(m.Memory())))
			case "_emscripten_memcpy_big":
				dst, src, n := uint32(params[0]), uint32(params[1]), uint32(params[2])
				mem := m.Memory()
				copy(regressionEmbenchenSlice(mem, dst, n, "memcpy destination"), regressionEmbenchenSlice(mem, src, n, "memcpy source"))
				results[0] = uint64(dst)
			case "___syscall146":
				mem := m.Memory()
				args := uint32(params[1])
				argBytes := regressionEmbenchenSlice(mem, args, 12, "writev arguments")
				iov := binary.LittleEndian.Uint32(argBytes[4:])
				count := binary.LittleEndian.Uint32(argBytes[8:])
				if count > 1024 {
					panic(fmt.Errorf("unexpected Emscripten writev count %d", count))
				}
				iovBytes := regressionEmbenchenSlice(mem, iov, count*8, "writev iovec array")
				total := uint64(0)
				for i := uint32(0); i < count; i++ {
					base := binary.LittleEndian.Uint32(iovBytes[i*8:])
					n := binary.LittleEndian.Uint32(iovBytes[i*8+4:])
					chunk := regressionEmbenchenSlice(mem, base, n, "writev chunk")
					if uint64(len(output))+uint64(len(chunk)) > 1<<20 {
						panic(fmt.Errorf("unexpected Emscripten output larger than 1 MiB"))
					}
					output = append(output, chunk...)
					total += uint64(n)
					if total > uint64(^uint32(0)) {
						panic(fmt.Errorf("Emscripten writev result overflow"))
					}
				}
				results[0] = total
			default:
				panic(fmt.Errorf("unsupported Emscripten host import %q", importName))
			}
		})
	}

	in, err := rt.Instantiate(context.Background(), mod, wago.WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	binary.LittleEndian.PutUint32(memory.Bytes()[argv:], prog)
	binary.LittleEndian.PutUint32(memory.Bytes()[argv+4:], arg)
	copy(memory.Bytes()[prog:], "bench\x00")
	copy(memory.Bytes()[arg:], "1\x00")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := in.Call(ctx, "_main", wago.ValueI32(2), wago.ValueI32(int32(argv)))
	if err != nil {
		t.Fatalf("_main: %v", err)
	}
	if len(got) != 1 || got[0].Type() != wago.ValI32 {
		t.Fatalf("_main result = %v, want one i32", got)
	}
	return got[0].I32(), output
}

func regressionEmbenchenSlice(memory []byte, offset, length uint32, what string) []byte {
	end := uint64(offset) + uint64(length)
	if end > uint64(len(memory)) {
		panic(fmt.Errorf("Emscripten %s range [%d,%d) exceeds memory size %d", what, offset, end, len(memory)))
	}
	return memory[offset:uint32(end)]
}

func FuzzRegressionEmbenchenSliceBounds(f *testing.F) {
	f.Add(uint32(0), uint32(0), uint16(0))
	f.Add(uint32(65535), uint32(1), uint16(65535))
	f.Add(^uint32(0), uint32(2), uint16(16))
	f.Fuzz(func(t *testing.T, offset, length uint32, size uint16) {
		memory := make([]byte, int(size))
		valid := uint64(offset)+uint64(length) <= uint64(len(memory))
		panicked := false
		var got []byte
		func() {
			defer func() { panicked = recover() != nil }()
			got = regressionEmbenchenSlice(memory, offset, length, "fuzz")
		}()
		if valid && (panicked || len(got) != int(length)) {
			t.Fatalf("valid range offset=%d length=%d size=%d panicked=%v len=%d", offset, length, size, panicked, len(got))
		}
		if !valid && !panicked {
			t.Fatalf("invalid range offset=%d length=%d size=%d did not panic", offset, length, size)
		}
	})
}

func alignRegressionEmbenchen(value, alignment uint32) uint32 {
	return (value + alignment - 1) &^ (alignment - 1)
}
