package parity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
)

type workload struct {
	ID       string   `json:"id"`
	Artifact string   `json:"artifact"`
	Export   string   `json:"export"`
	Args     []uint64 `json:"args"`
	Oracle   struct {
		Expected []string `json:"expected"`
	} `json:"oracle"`
}

// Uses the unchanged wasm.fyi application artifacts and exact-result oracles.
// Execution excludes compilation and instantiation; compilation includes release.
func BenchmarkParity(b *testing.B) {
	root := os.Getenv("WAGO_PARITY_CORPUS")
	if root == "" {
		root = "../../../../Web/wasm.fyi/corpora/applications"
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		b.Fatal(err)
	}
	var workloads []workload
	if err := json.Unmarshal(data, &workloads); err != nil {
		b.Fatal(err)
	}
	for _, w := range workloads {
		b.Run(strings.TrimPrefix(w.ID, "applications/"), func(b *testing.B) {
			code, err := os.ReadFile(filepath.Join(root, w.Artifact))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Compile", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					c, err := wago.Compile(nil, code)
					if err != nil {
						b.Fatal(err)
					}
					if err := c.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Exec", func(b *testing.B) {
				c, err := wago.Compile(nil, code)
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				if dir := os.Getenv("WAGO_PARITY_CODE_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						b.Fatal(err)
					}
					file, err := os.Create(filepath.Join(dir, filepath.Base(w.ID)+".bin"))
					if err != nil {
						b.Fatal(err)
					}
					_, err = c.WriteCodeTo(file)
					file.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
				in, err := wago.Instantiate(c, wago.InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc(w.Export)
				if err != nil {
					b.Fatal(err)
				}
				want := make([]uint64, len(w.Oracle.Expected))
				for i, s := range w.Oracle.Expected {
					want[i], err = strconv.ParseUint(s, 10, 64)
					if err != nil {
						b.Fatal(err)
					}
				}
				check := func() {
					out, err := fn.Invoke(w.Args...)
					if err != nil {
						b.Fatal(err)
					}
					if len(out) != len(want) {
						b.Fatalf("result count %d, want %d", len(out), len(want))
					}
					for i, v := range out {
						if v != want[i] {
							b.Fatalf("oracle: got %d, want %d", v, want[i])
						}
					}
				}
				for i := 0; i < 5; i++ {
					check()
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					check()
				}
			})
		})
	}
}
