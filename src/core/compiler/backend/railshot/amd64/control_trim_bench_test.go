//go:build amd64

package amd64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestControlScratchCorpusDepth(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/quickjs/qjs.wasm",
		"corpus/workloads/applications/jq/jq.wasm",
		"corpus/workloads/applications/php/php.wasm",
		"corpus/workloads/applications/lua/lua.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../../../../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			m, err := frontend.DecodeValidate(data)
			if err != nil {
				t.Fatal(err)
			}
			hints, _, _, err := computeModuleHints(m, m.GlobalCount(), m.ImportedFuncCount(), nil, false)
			if err != nil {
				t.Fatal(err)
			}
			maxDepth, beyondHint := 0, 0
			for _, h := range hints {
				if d := int(h.maxControlDepth); d > maxDepth {
					maxDepth = d
				}
				if h.maxControlDepth == 255 {
					beyondHint++
				}
			}
			t.Logf("functions=%d max_hinted_depth=%d saturated_depth_functions=%d", len(m.Code), maxDepth, beyondHint)
		})
	}
}

func TestTrimControlScratchAfterShallowRun(t *testing.T) {
	sc := newCompileScratch(defaultStackArenaCap)
	sc.ctrl = make([]ctrlFrame, 0, 640)
	sc.ctrlMerges = make([]ctrlFrameMerge, 0, 640)
	sc.ctrlRoots = make([]ctrlFrameRoots, 0, 640)
	frames := sc.ctrl[:cap(sc.ctrl)]
	merges := sc.ctrlMerges[:cap(sc.ctrlMerges)]
	roots := sc.ctrlRoots[:cap(sc.ctrlRoots)]
	frames[639].types = []machineType{mtI32}
	merges[639].eh = &ctrlFrameEH{}
	roots[639].flags = []bool{true}
	sc.trimControlScratch(8)
	if cap(sc.ctrl) != 640 || cap(sc.ctrlMerges) != 640 || cap(sc.ctrlRoots) != 640 {
		t.Fatal("first shallow function discarded outlier")
	}
	sc.trimControlScratch(8)
	if cap(sc.ctrl) != 0 || cap(sc.ctrlMerges) != 0 || cap(sc.ctrlRoots) != 0 {
		t.Fatal("second shallow function retained outlier")
	}
	if frames[639].types != nil || merges[639].eh != nil || roots[639].flags != nil {
		t.Fatal("released backing retained pointer-rich elements")
	}
	if sc.controlScratchDiscarded != 640 || sc.controlMergeDiscarded != 640 || sc.controlRootDiscarded != 640 {
		t.Fatal("discard counters omit released backing")
	}
	sc.ctrl = make([]ctrlFrame, 0, 64)
	sc.trimControlScratch(255)
	sc.trimControlScratch(0)
	if cap(sc.ctrl) != 64 || sc.smallControlRuns != 1 {
		t.Fatal("deep/small transition discarded ordinary capacity")
	}
	sc.ctrl[:cap(sc.ctrl)][5].mergeIndex = 5
	sc.ctrlMerges = make([]ctrlFrameMerge, 0, 640)
	sc.ctrlRoots = make([]ctrlFrameRoots, 0, 640)
	sc.trimControlScratch(0)
	if cap(sc.ctrl) != 64 || sc.ctrl[:cap(sc.ctrl)][5].mergeIndex != 0 {
		t.Fatal("retained frame kept stale merge index after sidecar release")
	}
}

func TestControlScratchDeepThenTinyWorker(t *testing.T) {
	previous := controlScratchTrimEnabled
	controlScratchTrimEnabled = true
	defer func() { controlScratchTrimEnabled = previous }()
	c := workerResetLoad(t, 32, false, shared.AMD64ModernBaseline)
	deep := make([]byte, 0, 768*3+3)
	for range 768 {
		deep = append(deep, 0x02, 0x7e)
	} // block (result i64)
	deep = append(deep, 0x20, 0) // local.get 0
	for range 769 {
		deep = append(deep, 0x0b)
	}
	c.m.Code[9].BodyBytes = deep
	var err error
	c.hints, c.sidecar, _, err = computeModuleHintsWithPolicy(c.m, 0, 0, nil, false, c.policy, false)
	if err != nil {
		t.Fatal(err)
	}
	c.ctrlCap = workerControlFrameCap(c.m, c.hints)
	sc := c.worker()
	defer workerResetClose(sc)
	workerResetCompile(t, c, sc, 9)
	large := workerScratchStats(sc).ControlRetained
	if large < 32<<10 {
		t.Fatalf("deep fixture retained only %d control bytes", large)
	}
	fresh := c.worker()
	defer workerResetClose(fresh)
	want := workerResetCompile(t, c, fresh, 0)
	for i := 0; i < 2; i++ {
		got := workerResetCompile(t, c, sc, 0)
		if difference := workerResetEqual(got, want); difference != "" {
			t.Fatal(difference)
		}
	}
	small := workerScratchStats(sc).ControlRetained
	if small >= large/2 {
		t.Fatalf("retained control bytes: deep=%d after tiny=%d", large, small)
	}
	t.Logf("retained control bytes: deep=%d after two tiny=%d", large, small)
}

func benchmarkControlScratchModule(b *testing.B, deepEverywhere bool) {
	m := benchParallelControlOutlierModule(b, 64, 768)
	if deepEverywhere {
		deep := m.Code[0]
		for i := range m.Code {
			m.Code[i] = deep
		}
	}
	if err := wasm.ValidateModule(m); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
		if err != nil {
			b.Fatal(err)
		}
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
	}
}

func BenchmarkControlScratchDeepThenTiny(b *testing.B) { benchmarkControlScratchModule(b, false) }
func BenchmarkControlScratchDeepThenDeep(b *testing.B) { benchmarkControlScratchModule(b, true) }
