//go:build amd64 && !tinygo && (linux || darwin || windows)

package wago

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPreparedDirectTrapAfterOrdinaryEntryAMD64(t *testing.T) {
	const child = "WAGO_TEST_PREBOUND_TRAP_CHILD"
	if os.Getenv(child) == "1" {
		testPreparedDirectTrapAfterOrdinaryEntryAMD64(t)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreparedDirectTrapAfterOrdinaryEntryAMD64$")
	cmd.Env = append(os.Environ(), child+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("prepared trap subprocess failed: %v\n%s", err, out)
	}
}

func testPreparedDirectTrapAfterOrdinaryEntryAMD64(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("div", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("ordinary", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	prepared, err := in.PrepareFunction("div")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.directIntMode != preparedIntCallPrebound {
		t.Fatalf("prepared mode = %v, want prebound context", prepared.directIntMode)
	}
	if _, err := in.Invoke("ordinary", 0x3ff8000000000000); err != nil {
		t.Fatalf("ordinary entry: %v", err)
	}
	if _, err := prepared.Invoke2(I32(7), I32(0)); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("prepared trap after ordinary entry = %v, want division-by-zero trap", err)
	}
}
