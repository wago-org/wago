package compilerpair

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIndependentFeatureProbes(t *testing.T) {
	for _, probe := range independentFeatureProbes() {
		m, err := wasm.DecodeModule(probe.Wasm)
		if err != nil {
			t.Fatalf("%s: %v", probe.Name, err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("%s: %v", probe.Name, err)
		}
	}
}

func TestIndependentV8UnsupportedFeature(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node/V8 independent engine unavailable")
	}
	skipped := false
	t.Run("oracle", func(t *testing.T) {
		t.Cleanup(func() { skipped = t.Skipped() })
		independentWithProbes(t, Fixtures()[:1], []nodeFeatureProbe{{"test-unsupported", []byte{0}}})
		t.Fatal("unsupported feature did not skip the reference test")
	})
	if !skipped {
		t.Fatal("unsupported feature was not reported as a skip")
	}
}

func TestIndependentV8ErrorsRemainFailures(t *testing.T) {
	const control = "WAGO_TEST_COMPILERPAIR_REFERENCE_FAILURE"
	if mode := os.Getenv(control); mode != "" {
		fixture := Fixtures()[0]
		if mode == "compile" {
			fixture.Wasm = []byte{0}
		} else {
			fixture.Want[0][0]++
		}
		independentWithProbes(t, []Fixture{fixture}, []nodeFeatureProbe{{"MVP", wasmtest.Module()}})
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node/V8 independent engine unavailable")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, reason string }{{"compile", "independent V8:"}, {"result", "V8 small f0"}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestIndependentV8ErrorsRemainFailures$", "-test.v")
			cmd.Env = append(os.Environ(), control+"="+tc.mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), tc.reason) || strings.Contains(string(out), "--- SKIP:") {
				t.Fatalf("reference %s error must fail the test: %v\n%s", tc.mode, err, out)
			}
		})
	}
}
