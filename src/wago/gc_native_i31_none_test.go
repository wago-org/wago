//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeI31NoneModule() []byte {
	types := [][]byte{{0x5f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32})}
	funcs := [][]byte{{1}}
	exports := [][]byte{wasmtest.ExportEntry("init", 0, 0)}
	bodies := [][]byte{wasmtest.Code([]byte{
		0x41, 1, 0x20, 0, 0xfb, 28, 0x26, 0, // table[1] = i31(parameter)
		0x41, 2, 0xfb, 1, 0, 0x26, 0, // table[2] = struct.new_default
		0x0b,
	})}
	for _, heap := range []byte{0x6c, 0x71, 0x6b} { // i31, none, struct (helper control)
		for _, op := range []byte{20, 21, 22, 23} {
			body := []byte{0x20, 0, 0x25, 0, 0xfb, op, heap}
			if op >= 22 {
				body = append(body, 0xd1) // ref.is_null also checks the cast result
			}
			body = append(body, 0x0b)
			funcs = append(funcs, []byte{2})
			exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("op%d_%x", op, heap), 0, uint32(len(funcs)-1)))
			bodies = append(bodies, wasmtest.Code(body))
			foreignBody := []byte{0x20, 0, 0xfb, 26, 0xfb, op, heap}
			if op >= 22 {
				foreignBody = append(foreignBody, 0xd1)
			}
			foreignBody = append(foreignBody, 0x0b)
			funcs = append(funcs, []byte{3})
			exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("foreign%d_%x", op, heap), 0, uint32(len(funcs)-1)))
			bodies = append(bodies, wasmtest.Code(foreignBody))
		}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6e, 0, 4})),
		wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNativeI31None(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeI31NoneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []uint64{0, 1, 0x3fffffff, 0x40000000, 0x7fffffff, 0xffffffff} {
			foreign, err := in.NewExternRef(fmt.Sprintf("foreign-%x", x))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("init", x); err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 2; round++ {
				for _, heap := range []byte{0x6c, 0x71, 0x6b} {
					for _, op := range []byte{20, 21, 22, 23} {
						foreignName := fmt.Sprintf("foreign%d_%x", op, heap)
						foreignGot, foreignErr := in.Invoke(foreignName, ValueExternRef(foreign).Bits())
						if op < 22 {
							if foreignErr != nil || len(foreignGot) != 1 || foreignGot[0] != 0 {
								t.Fatalf("%s: %v, %v", foreignName, foreignGot, foreignErr)
							}
						} else {
							var trap *TrapError
							if !errors.As(foreignErr, &trap) || trap.Code != TrapCastFailure {
								t.Fatalf("%s: %v, %v; want cast failure", foreignName, foreignGot, foreignErr)
							}
						}
						for index := uint64(0); index < 3; index++ {
							match := index == 1 && heap == 0x6c || index == 2 && heap == 0x6b || index == 0 && op&1 != 0
							name := fmt.Sprintf("op%d_%x", op, heap)
							got, err := in.Invoke(name, index)
							if op >= 22 && !match {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
									t.Fatalf("%s(%d): %v, %v; want cast failure", name, index, got, err)
								}
								continue
							}
							want := uint64(0)
							if op < 22 && match || op >= 22 && index == 0 {
								want = 1
							}
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("%s(%d): %v, %v; want %d", name, index, got, err, want)
							}
						}
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
		}
		in.Close()
	}
}

func TestGCNativeI31NoneHighBitsAndHandles(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeI31NoneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	// Inject words after host admission to check the native boundary directly.
	// No allocation or collection occurs while the table contains a forged word.
	table := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	for _, word := range []uint64{1, 0xffffffff, 0x100000000, 0x100000001, 0xffffffff00000000, 0xffffffffffffffff, 0xfffffffe} {
		binary.LittleEndian.PutUint64(table[8:], word)
		for _, heap := range []byte{0x6c, 0x71} {
			kind := gc.RefTestI31
			if heap == 0x71 {
				kind = gc.RefTestNone
			}
			for _, op := range []byte{20, 21, 22, 23} {
				target := gc.RefTestTarget{Kind: kind, Nullable: op&1 != 0}
				match, helperErr := in.gc.RefTest(gc.Ref(uint32(word)), target)
				if state := in.existingGCRefTestTableState(); state != nil {
					match, helperErr = state.refTest(in.gc, word, target)
				}
				got, err := in.Invoke(fmt.Sprintf("op%d_%x", op, heap), 0)
				if helperErr != nil {
					if err == nil {
						t.Fatalf("word%x op%d heap%x: helper error was hidden: %v", word, op, heap, got)
					}
				} else if op >= 22 && !match {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("word%x op%d heap%x: %v; want cast failure", word, op, heap, err)
					}
				} else {
					want := uint64(0)
					if op < 22 && match || op >= 22 && uint32(word) == 0 {
						want = 1
					}
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("word%x op%d heap%x: %v,%v; want %d", word, op, heap, got, err, want)
					}
				}
			}
		}
		binary.LittleEndian.PutUint64(table[8:], 0)
	}
}

func TestGCNativeI31NoneTrapOrderAndLivePrefix(t *testing.T) {
	for _, op := range []byte{20, 21, 22, 23} {
		params := make([]wasm.ValType, 17)
		results := make([]wasm.ValType, 17)
		body := []byte{0x41, 1, 0x20, 0, 0x6e} // division remains the first trap
		for i := range params {
			params[i], results[i] = wasm.I32, wasm.I32
		}
		for i := 1; i < len(params); i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0x23, 0, 0xfb, op, 0x6c, 0x1a, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType(params, results))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		args := make([]uint64, len(params))
		for i := 1; i < len(args); i++ {
			args[i] = uint64(100 + i)
		}
		_, err = in.Invoke("run", args...)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapDivZero {
			t.Fatalf("op%d: %v; want division trap", op, err)
		}
		args[0] = 1
		got, err := in.Invoke("run", args...)
		if op == 22 {
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("op%d: %v; want cast failure", op, err)
			}
		} else {
			if err != nil || len(got) != len(args) {
				t.Fatalf("op%d prefix: %v,%v", op, got, err)
			}
			for i := range args {
				if got[i] != args[i] {
					t.Fatalf("op%d prefix%d: %d; want %d", op, i, got[i], args[i])
				}
			}
		}
		in.Close()
		c.Close()
	}
}

func gcNativeI31NoneLoop(op, heap byte, mixed bool) []byte {
	return gcNativeI31NoneLoopValue(op, heap, mixed, false)
}

func gcNativeI31NoneLoopValue(op, heap byte, mixed, object bool) []byte {
	global := []byte{0x6e, 1, 0x41, 7, 0xfb, 28, 0x0b}
	if object {
		global = []byte{0x6e, 1, 0xfb, 1, 0, 0x0b}
	}
	if heap == 0x71 {
		global = []byte{0x6e, 1, 0xd0, 0x6e, 0x0b}
	}
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1}
	if mixed {
		body = append(body, 0x20, 1, 0x41, 1, 0x71, 0x04, 0x6e, 0x23, 0, 0x05, 0xd0, 0x6e, 0x0b)
	} else {
		body = append(body, 0x23, 0)
	}
	body = append(body, 0xfb, op, heap, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	return gcInstructionBenchmarkModule(body, global)
}

func BenchmarkGCNativeI31None(b *testing.B) {
	for _, tc := range []struct {
		name     string
		op, heap byte
		mixed    bool
	}{
		{"test-i31", 20, 0x6c, false}, {"test-none", 21, 0x71, false},
		{"cast-i31", 22, 0x6c, false}, {"cast-none", 23, 0x71, false},
		{"mixed-test", 21, 0x6c, true}, {"mixed-cast", 23, 0x6c, true},
		{"helper-struct", 20, 0x6b, false},
	} {
		b.Run(tc.name, func(b *testing.B) { benchmarkGCInstructionLoop(b, gcNativeI31NoneLoop(tc.op, tc.heap, tc.mixed), 1) })
	}
}

func BenchmarkGCNativeI31NoneCompile(b *testing.B) {
	data := gcNativeI31NoneModule()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(c.CodeSize()), "native-B")
		c.Close()
	}
}

func BenchmarkGCNativeI31NoneObject(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		b.Run(fmt.Sprintf("mixed-%v", mixed), func(b *testing.B) { benchmarkGCInstructionLoop(b, gcNativeI31NoneLoopValue(21, 0x6c, mixed, true), 1) })
	}
}
