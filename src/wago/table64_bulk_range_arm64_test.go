//go:build (linux || darwin) && arm64

package wago

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const arm64Table64BulkRangeChild = "WAGO_ARM64_TABLE64_BULK_RANGE_CHILD"

func arm64Table64FuncrefBulkRangeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x05, 0x01, 0x01}, // bounded table64 funcref, min/max 1
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fill_func", 0, 0),
			wasmtest.ExportEntry("copy", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x70, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}),
		)),
	)
}

func arm64Table64ExternrefBulkRangeModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x6f, 0x00, 0x01}, // table32 externref, min 1
			[]byte{0x6f, 0x04, 0x01}, // table64 externref, min 1
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fill_extern", 0, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x6f, 0x20, 0x01, 0xfc, 0x11, 0x01, 0x0b}),
		)),
	)
}

func runARM64Table64BulkRangeCase(t *testing.T, testCase string) {
	var module []byte
	var function string
	var args []uint64
	switch testCase {
	case "fill_func":
		module, function, args = arm64Table64FuncrefBulkRangeModule(), "fill_func", []uint64{^uint64(0), 1}
	case "fill_extern":
		module, function, args = arm64Table64ExternrefBulkRangeModule(), "fill_extern", []uint64{^uint64(0), 1}
	case "copy_dst":
		module, function, args = arm64Table64FuncrefBulkRangeModule(), "copy", []uint64{^uint64(0), 0, 1}
	case "copy_src":
		module, function, args = arm64Table64FuncrefBulkRangeModule(), "copy", []uint64{0, ^uint64(0), 1}
	default:
		t.Fatalf("unknown child case %q", testCase)
	}

	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTable64), module)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		compiled.Close()
		t.Fatal(err)
	}
	_, err = instance.Invoke(function, args...)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
		// On a vulnerable backend the unchecked write may have corrupted adjacent
		// memory. Exit without dereferencing it again. Ordinary Go isolates every
		// case in a child below; TinyGo already runs the suite in a test process.
		_, _ = fmt.Fprintf(os.Stderr, "Invoke = %v; want %v\n", err, TrapTableOutOfBounds)
		os.Exit(1)
	}
	if err := instance.Close(); err != nil {
		compiled.Close()
		t.Fatal(err)
	}
	compiled.Close()
}

func TestARM64Table64BulkRangeAdditionTrapsOnCarry(t *testing.T) {
	testCases := []string{"fill_func", "fill_extern", "copy_dst", "copy_src"}
	if runtime.Compiler == "tinygo" {
		for _, testCase := range testCases {
			t.Run(testCase, func(t *testing.T) {
				runARM64Table64BulkRangeCase(t, testCase)
			})
		}
		return
	}
	if testCase := os.Getenv(arm64Table64BulkRangeChild); testCase != "" {
		runARM64Table64BulkRangeCase(t, testCase)
		return
	}

	for _, testCase := range testCases {
		t.Run(testCase, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestARM64Table64BulkRangeAdditionTrapsOnCarry$", "-test.count=1")
			cmd.Env = append(os.Environ(), arm64Table64BulkRangeChild+"="+testCase)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child failed: %v\n%s", err, output)
			}
		})
	}
}
