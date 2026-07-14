//go:build (linux && amd64) || (darwin && arm64)

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCorpusExecutionCoverage exercises representative compiled workloads via
// the public compile/instantiate/invoke API. The backend corpus test covers
// lowering breadth; these cases also cover the runtime handoff and native call
// paths with pinned, independently useful results.
func TestCorpusExecutionCoverage(t *testing.T) {
	cases := []struct {
		file, init, export string
		args               []uint64
		want               uint64
	}{
		{"blake-as.wasm", "_initialize", "hashN", []uint64{100}, 2973751372},
		{"utf-as.wasm", "_initialize", "convertN", []uint64{200}, 819200},
		{"json-as.wasm", "_initialize", "serializeN", []uint64{64}, 6912},
		{"json-as.wasm", "_initialize", "deserializeN", []uint64{64}, 542208},
		{"memory_tree.wasm", "", "run", []uint64{6, 4}, 4634522508792},
		{"sieve.wasm", "", "count", []uint64{50000}, 5133},
		{"nbody.wasm", "", "step", []uint64{2000}, 4125895690},
		{"spectralnorm.wasm", "", "run", []uint64{128}, 1274222120},
		{"fannkuch.wasm", "", "run", []uint64{8}, 22},
		{"matmul.wasm", "", "run", []uint64{64}, 7081204},
		{"quicksort.wasm", "", "sortN", []uint64{4096}, 2381552730},
		{"crc32.wasm", "", "hashN", []uint64{8}, 1443045851},
		{"sha256.wasm", "", "hashN", []uint64{8}, 3825852647},
		{"raytrace.wasm", "", "render", []uint64{48}, 1021273579},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			wasm, err := os.ReadFile(filepath.Join("..", "..", "bench", "corpus", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), wasm)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.abort": HostFunc(func(HostModule, []uint64, []uint64) {})}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if tc.init != "" {
				if _, err := in.Invoke(tc.init); err != nil {
					t.Fatalf("%s: %v", tc.init, err)
				}
			}
			got, err := in.Invoke(tc.export, tc.args...)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("%s%v = %v, %v; want [%d], nil", tc.export, tc.args, got, err, tc.want)
			}
		})
	}
}
