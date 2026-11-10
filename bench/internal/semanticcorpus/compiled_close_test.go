//go:build linux && (amd64 || arm64) && !tinygo

package semanticcorpus

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func qualityCorpusCase(t testing.TB) (string, Module) {
	t.Helper()
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x60, 3, 0x7f, 0x7f, 0x7f, 0})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("answer", 0, 0), wasmtest.ExportEntry("vector", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0x20, 2, 0x41, 42, 0x3a, 0, 0, 0x0b}))),
	)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "case.wasm"), source, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	return root, Module{Artifact: "case.wasm", ArtifactSHA256: fmt.Sprintf("%x", sum), Invoke: Invoke{Export: "answer"}, Expect: Expect{Return: []string{"0x2a"}}, Limits: Limits{TimeoutMS: 1000}}
}

// Count anonymous executable bytes: Wago code is mapped outside the Go heap.
func qualityExecutableBytes(t testing.TB) uint64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Skipf("process mappings unavailable: %v", err)
	}
	var total uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || !strings.Contains(fields[1], "x") {
			continue
		}
		ends := strings.Split(fields[0], "-")
		if len(ends) != 2 {
			t.Fatal(line)
		}
		start, err := strconv.ParseUint(ends[0], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		end, err := strconv.ParseUint(ends[1], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		total += end - start
	}
	return total
}

func TestCorpusReleasesCompiledMappings(t *testing.T) {
	// Isolate the mapping count from finalizers left by other tests. Automatic
	// Go GC stays disabled only in the child, so it cannot hide delayed cleanup.
	if os.Getenv("WAGO_CORPUS_MAPPING_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCorpusReleasesCompiledMappings$", "-test.v")
		cmd.Env = append(os.Environ(), "WAGO_CORPUS_MAPPING_CHILD=1", "GOGC=off")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("mapping checks: %v\n%s", err, output)
		}
		return
	}
	for _, mode := range []string{"run", "repeated", "vectors", "run-error", "repeated-error"} {
		t.Run(mode, func(t *testing.T) {
			root, mod := qualityCorpusCase(t)
			if mode == "vectors" {
				mod.Invoke = Invoke{Export: "vector", Vectors: &Vectors{OutputLen: 1, Cases: []VectorCase{{Len: 0, Out: "2a"}}}}
			}
			wantError := strings.HasSuffix(mode, "-error")
			if wantError {
				mod.Expect.Return = []string{"0x0"}
			}
			before := qualityExecutableBytes(t)
			for i := 0; i < 3; i++ {
				var err error
				if strings.HasPrefix(mode, "repeated") {
					err = RunRepeated(root, mod, 2)
				} else {
					err = Run(root, mod)
				}
				if (err != nil) != wantError {
					t.Fatalf("run error = %v, want error %t", err, wantError)
				}
			}
			if after := qualityExecutableBytes(t); after != before {
				t.Fatalf("anonymous executable bytes = %d, want %d after cleanup", after, before)
			}
		})
	}
}

func BenchmarkSemanticCorpusLifecycle(b *testing.B) {
	for _, repeated := range []bool{false, true} {
		b.Run(fmt.Sprintf("repeated%t", repeated), func(b *testing.B) {
			root, mod := qualityCorpusCase(b)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				if repeated {
					err = RunRepeated(root, mod, 8)
				} else {
					err = Run(root, mod)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
