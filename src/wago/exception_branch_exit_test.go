//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Every try catches to $caught. A taken branch exits to $out, then throws
// outside all tries. A stale handler incorrectly returns 99 instead of trapping.
// Conditional fallthrough throws inside the try and must still return 99.
func exceptionBranchExitModule(kind string, nesting int) []byte {
	outType := byte(0x40)
	if kind == "br_i32" {
		outType = 0x7f
	}
	if kind == "br_on_non_null" {
		outType = 0x70
	}
	if kind == "br_on_cast" || kind == "br_on_cast_fail" {
		outType = 0x6e
	}
	body := []byte{}
	if strings.HasPrefix(kind, "br_on_cast") {
		// Admit the generic GC helper path used by branch casts.
		body = append(body, 0xfb, 1, 2, 0x1a) // struct.new_default 2; drop
	}
	body = append(body, 0x02, 0x40, 0x02, outType) // block $caught; block $out
	for i := 0; i < nesting; i++ {
		body = append(body, 0x1f, 0x40, 1, byte(wasm.CatchAll), byte(i+1))
	}
	target := byte(nesting)
	switch kind {
	case "br_i32":
		body = append(body, 0x41, 42, 0x0c, target)
	case "br":
		body = append(body, 0x0c, target)
	case "br_if":
		body = append(body, 0x20, 0, 0x0d, target)
	case "br_if_eqz":
		body = append(body, 0x20, 0, 0x45, 0x0d, target)
	case "br_if_compare":
		body = append(body, 0x20, 0, 0x41, 0, 0x47, 0x0d, target)
	case "br_table", "br_table_large":
		n := 2
		if kind == "br_table_large" {
			n = 16
		}
		body = append(body, 0x20, 0, 0x0e, byte(n))
		for i := 0; i <= n; i++ {
			body = append(body, target)
		}
	case "br_on_null":
		body = append(body, 0x20, 0, 0x04, 0x70, 0xd0, 0x70, 0x05, 0xd2, 0, 0x0b, 0xd5, target, 0x1a)
	case "br_on_non_null":
		body = append(body, 0x20, 0, 0x04, 0x70, 0xd2, 0, 0x05, 0xd0, 0x70, 0x0b, 0xd6, target)
	case "br_on_cast":
		body = append(body, 0x20, 0, 0x04, 0x6e, 0x41, 1, 0xfb, 28, 0x05, 0xd0, 0x6e, 0x0b, 0xfb, 24, 1, target, 0x6e, 0x6c, 0x1a)
	case "br_on_cast_fail":
		body = append(body, 0x20, 0, 0x04, 0x6e, 0xd0, 0x6e, 0x05, 0x41, 1, 0xfb, 28, 0x0b, 0xfb, 25, 1, target, 0x6e, 0x6c, 0x1a)
	}
	body = append(body, 0x08, 0) // throw inside the live handler on fallthrough
	for i := 0; i < nesting; i++ {
		body = append(body, 0x0b)
	}
	body = append(body, 0x00, 0x0b) // unreachable; end $out
	if outType != 0x40 {
		body = append(body, 0x1a)
	}
	body = append(body, 0x08, 0, 0x0b, 0x41, 0xe3, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), []byte{0x5f, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), // declare ref.func 0
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
	)
}

func TestExceptionBranchExitsDiscardHandlers(t *testing.T) {
	for _, kind := range []string{"br", "br_i32", "br_if", "br_if_eqz", "br_if_compare", "br_table", "br_table_large", "br_on_null", "br_on_non_null", "br_on_cast", "br_on_cast_fail"} {
		for nesting := 1; nesting <= 3; nesting++ {
			t.Run(fmt.Sprintf("%s/depth%d", kind, nesting), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), exceptionBranchExitModule(kind, nesting))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				taken := uint64(1)
				if kind == "br_if_eqz" {
					taken = 0
				}
				if got, err := in.Invoke("run", taken); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
					t.Fatalf("branch outside try: got %v, %v; want unhandled exception", got, err)
				}
				if strings.HasPrefix(kind, "br_if") || strings.HasPrefix(kind, "br_on") {
					if got, err := in.Invoke("run", 1-taken); err != nil || len(got) != 1 || got[0] != 99 {
						t.Fatalf("fallthrough inside try: got %v, %v; want 99", got, err)
					}
				}
			})
		}
	}
}

// Isolate the dead-frame repro: the broken handler can restore a returned native
// stack frame, so a process failure is also a regression rather than a test hang.
func TestExceptionBranchExitCannotReenterReturnedFrame(t *testing.T) {
	if os.Getenv("WAGO_TEST_EH_RETURNED_FRAME") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExceptionBranchExitCannotReenterReturnedFrame$", "-test.count=1")
		cmd.Env = append(os.Environ(), "WAGO_TEST_EH_RETURNED_FRAME=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("returned-frame regression: %v\n%s", err, output)
		}
		return
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("g", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x02, 0x40, 0x1f, 0x40, 1, byte(wasm.CatchAll), 0, 0x0c, 1, 0x0b, 0x0b, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x08, 0, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, callErr := in.Invoke("run")
	g, err := in.Global("g")
	if err != nil || g != 1 {
		t.Fatalf("returned frame ran again: g=%d, %v; want 1", g, err)
	}
	if callErr == nil || !strings.Contains(callErr.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("run = %v; want unhandled exception", callErr)
	}
}

func TestExceptionBranchToTryLabelDiscardsHandler(t *testing.T) {
	t.Run("only handler", func(t *testing.T) {
		body := []byte{
			0x02, 0x40, // block $out
			0x1f, 0x40, 1, byte(wasm.CatchAll), 0, // try_table (catch_all $out)
			0x0c, 0, // br to the try_table's own end; no lexical fallthrough
			0x0b,
			0x08, 0, // throw after the exited try
			0x0b,
			0x41, 0xe3, 0x00, // stale handler catch would reach this 99
			0x0b,
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
			wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
		)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("run")
		if err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
			t.Fatalf("branch to own try label retained handler: got %v, %v", got, err)
		}
	})

	t.Run("nested retains outer handler", func(t *testing.T) {
		body := []byte{
			0x02, 0x40, // block $outer_caught
			0x1f, 0x40, 1, byte(wasm.CatchAll), 0, // outer try_table
			0x02, 0x40, // block $inner_caught
			0x1f, 0x40, 1, byte(wasm.CatchAll), 0, // inner try_table
			0x0c, 0, // br to the inner try_table's own end
			0x0b,
			0x08, 0, // must reach the outer handler, not the exited inner handler
			0x0b,
			0x41, 0xd8, 0x00, 0x0f, // stale inner handler returns 88
			0x0b,
			0x0b,
			0x41, 0xe3, 0x00, // outer handler returns 99
			0x0b,
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
			wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
		)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 99 {
			t.Fatalf("branch to inner try label: got %v, %v; want outer catch result 99", got, err)
		}
	})
}
