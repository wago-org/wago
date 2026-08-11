//go:build amd64

package wago

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func dirtyHostI32LoopPrecheckModule() []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("addr")...), 0x00, 0x00)
	body := []byte{
		0x01, 0x01, 0x7f, // local $base i32
		0x10, 0x00, 0x21, 0x00, // base = host addr()
		0x03, 0x40, // loop
	}
	for _, offset := range []uint32{0, 4, 8, 12} {
		body = append(body, 0x20, 0x00, 0x28, 0x02)
		body = append(body, wasmtest.ULEB(offset)...)
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b, 0x41, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

type loopPrecheckOutcome struct {
	Results  []uint64 `json:"results,omitempty"`
	Trap     uint32   `json:"trap,omitempty"`
	CodeSize int      `json:"code_size,omitempty"`
}

func memory64LoopWithoutElisionModule() []byte {
	instructions := []byte{0x03, 0x40} // loop
	for _, offset := range []uint32{0, 8, 16, 24} {
		instructions = append(instructions, 0x20, 0x00, 0x29, 0x03)
		instructions = append(instructions, wasmtest.ULEB(offset)...)
		instructions = append(instructions, 0x1a)
	}
	instructions = append(instructions, 0x0b, 0x42, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x05, 0x01, 0x01})), // memory64 min=max=1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(instructions))),
	)
}

func runLoopPrecheckChild(t *testing.T, testName, childEnv, prefix, precheck string) loopPrecheckOutcome {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$", "-test.count=1")
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "WAGO_LOOP_PRECHECK=") || strings.HasPrefix(entry, childEnv+"=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "WAGO_LOOP_PRECHECK="+precheck, childEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("precheck=%s child: %v\n%s", precheck, err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var out loopPrecheckOutcome
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("precheck=%s produced no oracle:\n%s", precheck, output)
	return loopPrecheckOutcome{}
}

func TestMemory64LoopDoesNotVersionWithoutElision(t *testing.T) {
	const childEnv = "WAGO_MEMORY64_LOOP_VERSION_CHILD"
	const prefix = "MEMORY64_LOOP_VERSION="
	if os.Getenv(childEnv) == "1" {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), memory64LoopWithoutElisionModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		results, err := in.Invoke("run", 0)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(loopPrecheckOutcome{Results: results, CodeSize: compiled.CodeSize()})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(prefix + string(data))
		return
	}
	disabled := runLoopPrecheckChild(t, "TestMemory64LoopDoesNotVersionWithoutElision", childEnv, prefix, "0")
	enabled := runLoopPrecheckChild(t, "TestMemory64LoopDoesNotVersionWithoutElision", childEnv, prefix, "1")
	if fmt.Sprint(disabled.Results) != "[0]" || fmt.Sprint(enabled.Results) != "[0]" || disabled.CodeSize != enabled.CodeSize {
		t.Fatalf("memory64 loop versioning: disabled=%+v enabled=%+v", disabled, enabled)
	}
}

func TestMemory32LoopPrecheckCanonicalizesHostI32(t *testing.T) {
	const childEnv = "WAGO_LOOP_PRECHECK_CANONICAL_CHILD"
	const prefix = "LOOP_PRECHECK_CANONICAL="
	if os.Getenv(childEnv) == "1" {
		compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), dirtyHostI32LoopPrecheckModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.addr": HostFunc(func(_ HostModule, _, results []uint64) {
			results[0] = 0xffff_ffff_ffff_fff0
		})}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		results, callErr := in.Invoke("run")
		out := loopPrecheckOutcome{Results: results}
		if callErr != nil {
			trap, ok := callErr.(*TrapError)
			if !ok {
				t.Fatalf("non-trap error: %v", callErr)
			}
			out.Results = nil
			out.Trap = uint32(trap.Code)
		}
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(prefix + string(data))
		return
	}

	disabled := runLoopPrecheckChild(t, "TestMemory32LoopPrecheckCanonicalizesHostI32", childEnv, prefix, "0")
	enabled := runLoopPrecheckChild(t, "TestMemory32LoopPrecheckCanonicalizesHostI32", childEnv, prefix, "1")
	if disabled.Trap != uint32(TrapLinMemOutOfBounds) || enabled.Trap != uint32(TrapLinMemOutOfBounds) {
		t.Fatalf("loop precheck parity: disabled=%+v enabled=%+v, want memory OOB", disabled, enabled)
	}
	if fmt.Sprint(disabled) != fmt.Sprint(enabled) {
		t.Fatalf("loop precheck semantic mismatch: disabled=%+v enabled=%+v", disabled, enabled)
	}
}
