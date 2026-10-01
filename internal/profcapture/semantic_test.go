//go:build wago_profile

package profcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func semanticFixture() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 2, 0x20, 0, 0x2d, 0, 0, 0x20, 1, 0x6a, 0x3a, 0, 0, 0x0b}))),
	)
}

func TestSemanticCatalogAndCaptureOracles(t *testing.T) {
	for _, mode := range []string{"public", "prepared"} {
		for _, valid := range []bool{true, false} {
			t.Run(mode+map[bool]string{true: "/valid", false: "/bad-memory"}[valid], func(t *testing.T) {
				dir := t.TempDir()
				data := semanticFixture()
				sum := sha256.Sum256(data)
				hash := hex.EncodeToString(sum[:])
				if err := os.WriteFile(filepath.Join(dir, "test.wasm"), data, 0600); err != nil {
					t.Fatal(err)
				}
				expected := "02"
				if !valid {
					expected = "03"
				}
				check := map[string]any{"id": "test/copy", "artifact": "test.wasm", "artifact_sha256": hash, "abi": "core", "invoke": map[string]any{"export": "copy", "vectors": map[string]any{"input_offset": 16, "output_offset": 32, "output_len": 1, "mod": 251, "cases": []map[string]any{{"len": 1, "out": "01"}, {"len": 2, "out": expected}}}}}
				catalog := map[string]any{"schema": 1, "benchmarks": []map[string]any{{"id": "test", "artifact": "test.wasm", "artifact_sha256": hash, "semantic_exec": []string{"test/copy"}}}, "checks": []any{check}}
				b, _ := json.Marshal(catalog)
				path := filepath.Join(dir, "catalog.json")
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
				w, b, err := LoadWorkload(path, "test", "", "", "", "", "")
				if err != nil {
					t.Fatal(err)
				}
				out := filepath.Join(dir, "capture")
				err = Run(Options{Out: out, Backend: "none", Phase: "execute", Mode: mode, Iterations: 3, Warmup: 1, Bounds: "explicit", Rate: 99}, w, b)
				if (err == nil) != valid {
					t.Fatalf("valid=%v error=%v", valid, err)
				}
				b, err = os.ReadFile(filepath.Join(out, "manifest.json"))
				if err != nil {
					t.Fatal(err)
				}
				var m Manifest
				if err = json.Unmarshal(b, &m); err != nil {
					t.Fatal(err)
				}
				if m.Complete != valid || !m.MemoryValidation || !m.SemanticInputWrites || len(m.SemanticChecks) != 1 {
					t.Fatal(m)
				}
				if valid && (m.Iterations != 3 || m.Invocations != 6) {
					t.Fatal(m)
				}
			})
		}
	}
}

func TestSemanticContractNearMisses(t *testing.T) {
	var good semanticCase
	if err := json.Unmarshal([]byte(`{"id":"test","abi":"core","invoke":{"export":"copy","args":[0,0,32],"input":"ab"},"expect":{"memory":[{"offset":32,"hex":"ab"}]}}`), &good); err != nil {
		t.Fatal(err)
	}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*semanticCase){
		func(s *semanticCase) { s.KnownIssue = "blocked" },
		func(s *semanticCase) { s.ABI = "wasi" },
		func(s *semanticCase) { s.Invoke.Input = "xz" },
		func(s *semanticCase) { s.Expect.Memory = nil },
	} {
		s := good
		mutate(&s)
		if s.validate() == nil {
			t.Fatal("invalid contract admitted")
		}
	}
	other := good
	other.Invoke.Args = []int32{0, 1, 32}
	if workloadHash(Workload{semantic: []semanticCase{good}}) == workloadHash(Workload{semantic: []semanticCase{other}}) {
		t.Fatal("semantic input absent from workload identity")
	}
}

func TestSemanticInputsResetBetweenContracts(t *testing.T) {
	var first, second semanticCase
	for i, entry := range []*semanticCase{&first, &second} {
		value := []string{"ab", "cd"}[i]
		contract := fmt.Sprintf(`{"id":"copy/%d","abi":"core","invoke":{"export":"copy","args":[0,0,32],"input":"%s"},"expect":{"memory":[{"offset":32,"hex":"%s"}]}}`, i, value, value)
		if err := json.Unmarshal([]byte(contract), entry); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"public", "prepared"} {
		out := filepath.Join(t.TempDir(), "capture")
		err := Run(Options{Out: out, Backend: "none", Phase: "execute", Mode: mode, Iterations: 5, Warmup: 1, Bounds: "explicit", Rate: 99}, Workload{ID: "copy", semantic: []semanticCase{first, second}}, semanticFixture())
		if err != nil {
			t.Fatal(mode, err)
		}
	}
}
