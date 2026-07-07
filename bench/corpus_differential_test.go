//go:build wago_guardpage

package wagobench

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// corpusDifferentialCase pins one corpus module's execution result as a golden
// constant and checks that BoundsChecksExplicit and BoundsChecksSignalsBased
// compile the SAME module to the SAME answer. This is the #68-class regression
// net: #68 and the railshot blake bug were both a bounds-mode-specific desync
// (a lazy pinned-local/STACK_REG reconciliation bug) that silently corrupted
// results in exactly one of the two modes while leaving the other correct.
var corpusDifferentialCases = []struct {
	file   string
	init   string
	export string
	args   []uint64
	want   uint64
}{
	{"blake-as.wasm", "_initialize", "hashN", []uint64{100}, 2973751372},
	{"utf-as.wasm", "_initialize", "convertN", []uint64{200}, 819200},
	{"json-as.wasm", "_initialize", "serializeN", []uint64{64}, 6912},
	{"json-as.wasm", "_initialize", "deserializeN", []uint64{64}, 542208},
	{"memory_tree.wasm", "", "run", []uint64{6, 4}, 4634522508792},
	{"sieve.wasm", "", "count", []uint64{50000}, 5133},
	// Rust compute kernels (corpus/rust). The array-heavy ones (matmul, quicksort,
	// crc32, sha256) are the bounds-mode-desync-sensitive cases; nbody/spectralnorm/
	// raytrace pin the f64 path. Golden values are the low-32-bit i32 result.
	{"nbody.wasm", "", "step", []uint64{2000}, 4125895690},
	{"spectralnorm.wasm", "", "run", []uint64{128}, 1274222120},
	{"fannkuch.wasm", "", "run", []uint64{8}, 22},
	{"matmul.wasm", "", "run", []uint64{64}, 7081204},
	// 2381552730 (== wasmtime's -1913414566 as u32) is the CORRECT result. The
	// prior golden 3925533191 was captured from a latent condenseBinary
	// self-update miscompile, fixed alongside the inflate bug in this change.
	{"quicksort.wasm", "", "sortN", []uint64{4096}, 2381552730},
	{"crc32.wasm", "", "hashN", []uint64{8}, 1443045851},
	{"sha256.wasm", "", "hashN", []uint64{8}, 3825852647},
	{"raytrace.wasm", "", "render", []uint64{48}, 1021273579},
}

func runCorpusDifferentialCase(t *testing.T, mode wago.BoundsCheckMode, file, init, export string, args []uint64) uint64 {
	t.Helper()
	b, err := os.ReadFile("corpus/" + file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(mode)
	comp, err := wago.Compile(cfg, b)
	if err != nil {
		t.Fatalf("%s compile: %v", file, err)
	}
	in, err := wago.Instantiate(comp, wago.InstantiateOptions{Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}})
	if err != nil {
		t.Fatalf("%s instantiate: %v", file, err)
	}
	defer in.Close()
	if init != "" {
		if _, err := in.Invoke(init); err != nil {
			t.Fatalf("%s init: %v", file, err)
		}
	}
	res, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s %s%v: %v", file, export, args, err)
	}
	if len(res) != 1 {
		t.Fatalf("%s %s%v: expected 1 result, got %d", file, export, args, len(res))
	}
	return res[0]
}

// TestCorpusDifferential compiles each corpus module under explicit and
// guard-page bounds checks and asserts both agree with each other AND with a
// golden constant (so a bug shared by both modes still fails the test).
func TestCorpusDifferential(t *testing.T) {
	for _, c := range corpusDifferentialCases {
		c := c
		t.Run(c.file+"."+c.export, func(t *testing.T) {
			explicit := runCorpusDifferentialCase(t, wago.BoundsChecksExplicit, c.file, c.init, c.export, c.args)
			guard := runCorpusDifferentialCase(t, wago.BoundsChecksSignalsBased, c.file, c.init, c.export, c.args)
			if explicit != guard {
				t.Errorf("explicit/guard mismatch: explicit=%d guard=%d", explicit, guard)
			}
			if explicit != c.want {
				t.Errorf("explicit=%d, want golden %d", explicit, c.want)
			}
			if guard != c.want {
				t.Errorf("guard=%d, want golden %d", guard, c.want)
			}
		})
	}
}
