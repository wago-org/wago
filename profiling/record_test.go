//go:build wago_profile

package profiling

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func applicationModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("custom")...)
	imp = append(imp, 0, 0) // function import, type 0
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
	)
}

func TestRecordApplicationImportsAndValidatedWork(t *testing.T) {
	imports := wago.NewImports()
	imports.HostFunc("env", "custom", func(v int32) int32 { return v + 1 })
	started, calls := 0, 0
	out := filepath.Join(t.TempDir(), "capture")
	h := Harness{
		ID: "real-app", Contract: "request-v1", WorkUnit: "request", Wasm: applicationModule(),
		Config: wago.NewRuntimeConfig().WithNativeStackBytes(2 << 20), Instantiate: wago.InstantiateOptions{Imports: imports},
		Initialize: func(*wago.Instance) error { started++; return nil },
		Execute: func(in *wago.Instance) error {
			result, err := in.Invoke("run", 41)
			if err != nil || len(result) != 1 || wago.AsI32(result[0]) != 42 {
				return fmt.Errorf("invalid application result: %v %v", result, err)
			}
			calls++
			return nil
		},
	}
	if err := Record(Options{Out: out, Backend: "none", Iterations: 3, Warmup: 1, SourceMaps: true, WagoRevision: "test-wago-revision"}, h); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m profcapture.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if !m.Complete || m.Mode != "application" || m.Iterations != 3 || m.Invocations != 3 || started != 1 || calls != 4 || m.WorkloadHash == "" || m.Revision != "test-wago-revision" {
		t.Fatalf("application capture: %+v; started=%d calls=%d", m, started, calls)
	}
	if m.Config["native_stack_bytes"] != float64(2<<20) {
		t.Fatalf("application runtime configuration was replaced: %+v", m.Config)
	}
	for _, phase := range m.Phases {
		if phase.Name == "execute" && (phase.WorkUnit != "request" || phase.Completed != 3) {
			t.Fatalf("incorrect unit: %+v", phase)
		}
	}
}

func TestRecordApplicationRequiresValidatedWork(t *testing.T) {
	if err := Record(Options{Out: filepath.Join(t.TempDir(), "capture"), Iterations: 1}, Harness{ID: "bad", Contract: "v1", WorkUnit: "request", Wasm: applicationModule()}); err == nil {
		t.Fatal("accepted missing work callback")
	}
}

func TestRecordApplicationRejectsConflictingDurationAndIterations(t *testing.T) {
	err := Record(Options{Out: filepath.Join(t.TempDir(), "capture"), Duration: time.Second, Iterations: 1}, Harness{
		ID: "app", Contract: "v1", WorkUnit: "request", Wasm: applicationModule(),
		Execute: func(*wago.Instance) error { return nil },
	})
	if err == nil {
		t.Fatal("accepted conflicting duration and iteration limits")
	}
}
