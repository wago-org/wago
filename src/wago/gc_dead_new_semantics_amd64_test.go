//go:build linux && amd64 && !tinygo && !wago_guardpage

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

type deadGCAllocationOutcome struct {
	Results []uint64 `json:"results,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func deadGCAllocationModule(array bool) ([]byte, GCConfig) {
	typeDef := []byte{0x5f, 0x00} // empty final struct
	first := []byte{0xfb, 0x01, 0x00}
	cfg := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}
	if array {
		typeDef = []byte{0x5e, 0x7f, 0x01} // final (array (mut i32))
		first = []byte{0x41, 0x01, 0xfb, 0x08, 0x00, 0x01}
		cfg = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8}
	}
	body := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null 0) local
	body = append(body, first...)
	body = append(body, 0x21, 0x00)
	body = append(body, first...)
	body = append(body,
		0x1a,       // drop the second allocation
		0x20, 0x00, // retain the first object across it
		0xd1, 0x45, // ref.is_null; i32.eqz
		0x0b,
	)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typeDef, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	return module, cfg
}

func deadGCNestedAllocationModule() ([]byte, GCConfig) {
	inner := []byte{0x5f, 0x00}
	outer := []byte{0x5f}
	outer = append(outer, wasmtest.Vec([]byte{0x63, 0x00, 0x00})...)
	body := []byte{
		0xfb, 0x01, 0x00, // struct.new_default inner
		0xfb, 0x00, 0x01, // struct.new outer
		0x1a,
		0x41, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(inner, outer, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	), GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8}
}

func runDeadGCAllocationChild(t *testing.T, disabled bool, kind string) deadGCAllocationOutcome {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeadGCConstructorsPreserveBoundedAllocation$", "-test.count=1")
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "WAGO_AMD64_NO_DEAD_GC_NEW=") || strings.HasPrefix(entry, "WAGO_DEAD_GC_ALLOCATION_CHILD=") || strings.HasPrefix(entry, "WAGO_DEAD_GC_ALLOCATION_KIND=") {
			continue
		}
		env = append(env, entry)
	}
	value := "0"
	if disabled {
		value = "1"
	}
	cmd.Env = append(env, "WAGO_AMD64_NO_DEAD_GC_NEW="+value, "WAGO_DEAD_GC_ALLOCATION_CHILD=1", "WAGO_DEAD_GC_ALLOCATION_KIND="+kind)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s disabled=%v child: %v\n%s", kind, disabled, err, output)
	}
	const prefix = "DEAD_GC_ALLOCATION="
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var out deadGCAllocationOutcome
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("%s disabled=%v produced no oracle:\n%s", kind, disabled, output)
	return deadGCAllocationOutcome{}
}

func TestDeadGCConstructorsPreserveBoundedAllocation(t *testing.T) {
	if os.Getenv("WAGO_DEAD_GC_ALLOCATION_CHILD") == "1" {
		kind := os.Getenv("WAGO_DEAD_GC_ALLOCATION_KIND")
		data, cfg := deadGCAllocationModule(kind == "array")
		if kind == "nested" {
			data, cfg = deadGCNestedAllocationModule()
		}
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), data)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		results, invokeErr := in.Invoke("run")
		out := deadGCAllocationOutcome{Results: results}
		if invokeErr != nil {
			out.Error = invokeErr.Error()
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("DEAD_GC_ALLOCATION=" + string(encoded))
		return
	}
	for _, kind := range []string{"struct", "array", "nested"} {
		t.Run(kind, func(t *testing.T) {
			enabled := runDeadGCAllocationChild(t, false, kind)
			disabled := runDeadGCAllocationChild(t, true, kind)
			if enabled.Error == "" || !strings.Contains(enabled.Error, "tiny heap exhausted") {
				t.Fatalf("enabled = %+v, want bounded exhaustion", enabled)
			}
			if enabled.Error != disabled.Error || fmt.Sprint(enabled.Results) != fmt.Sprint(disabled.Results) {
				t.Fatalf("dead-constructor parity: enabled=%+v disabled=%+v", enabled, disabled)
			}
		})
	}
}
