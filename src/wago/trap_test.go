//go:build linux && amd64

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestInvokeTrapError(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boom", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))), // unreachable; end
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	_, err = in.Invoke("boom")
	var te *TrapError
	if !errors.As(err, &te) || te.Code != TrapUnreachable {
		t.Fatalf("Invoke trap = %v; want *TrapError with TrapUnreachable", err)
	}
}

func boundedLargeFrameRecursionModule() []byte {
	body := []byte{0x01}
	body = append(body, wasmtest.ULEB(4095)...)
	body = append(body, wasm.MustEncodeValType(wasm.I64))
	body = append(body,
		0x20, 0x00, // local.get depth
		0x45,       // i32.eqz
		0x04, 0x40, // if
		0x0f,       // return
		0x0b,       // end if
		0x20, 0x00, // local.get depth
		0x41, 0x01, // i32.const 1
		0x6b,       // i32.sub
		0x10, 0x00, // call self
		0x20, 0x00, // keep the call non-tail
		0x1a, // drop
		0x0b, // end function
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestConfiguredNativeStackAdmitsBoundedLargeFrameRecursion(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig(), boundedLargeFrameRecursionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	invoke := func(t *testing.T, stackBytes uint64) error {
		t.Helper()
		rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithNativeStackBytes(stackBytes)))
		defer rt.Close()
		module, err := rt.Module(compiled)
		if err != nil {
			return err
		}
		defer module.Close()
		instance, err := rt.Instantiate(context.Background(), module)
		if err != nil {
			return err
		}
		defer instance.Close()
		_, err = instance.Invoke("recurse", 120)
		return err
	}

	var trap *TrapError
	if err := invoke(t, DefaultNativeStackBytes); !errors.As(err, &trap) || trap.Code != TrapStackFenceBreached {
		t.Fatalf("default native stack recursion = %v; want stack-fence trap", err)
	}
	if err := invoke(t, 8<<20); err != nil {
		t.Fatalf("8 MiB native stack recursion: %v", err)
	}
}

func TestDirectInstantiateUsesCompiledNativeStackCapacity(t *testing.T) {
	const stackBytes = 8 << 20
	compiled, err := Compile(NewRuntimeConfig().WithNativeStackBytes(stackBytes), boundedLargeFrameRecursionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got := instance.eng.StackBytes(); got != stackBytes {
		t.Fatalf("direct instance native stack = %d, want %d", got, stackBytes)
	}
	if _, err := instance.Invoke("recurse", 120); err != nil {
		t.Fatalf("direct 8 MiB native stack recursion: %v", err)
	}
}

func TestRecursiveStackExhaustionTrapsCleanly(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))), // call self; end
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	_, err = in.Invoke("recurse")
	var te *TrapError
	if !errors.As(err, &te) || te.Code != TrapStackFenceBreached {
		t.Fatalf("recursive exhaustion trap = %v; want *TrapError with TrapStackFenceBreached", err)
	}
}

func TestExportedNamesAndMustCompile(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),                   // memory min=1, no max
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x00, 0x41, 0x07, 0x0b})), // global g: i32 const 7
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("zed", 0, 0),
			wasmtest.ExportEntry("abe", 0, 0),
			wasmtest.ExportEntry("mem", 2, 0),
			wasmtest.ExportEntry("g", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x0b}))),
	)
	c := MustCompile(mod)
	if got := c.ExportedFunctions(); len(got) != 2 || got[0] != "abe" || got[1] != "zed" {
		t.Fatalf("ExportedFunctions = %v; want sorted [abe zed]", got)
	}
	if got := c.ExportedGlobals(); len(got) != 1 || got[0] != "g" {
		t.Fatalf("ExportedGlobals = %v; want [g]", got)
	}
}
