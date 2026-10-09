//go:build linux && amd64

package amd64

import (
	"crypto/sha256"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func experimentCorpusModule(t testing.TB, path string) *wasm.Module {
	full := path
	if _, err := os.Stat(full); err != nil {
		full = filepath.Join("../../../../../..", path)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

var experimentCorpusPaths = []string{"corpus/workloads/synthetic/memory.wasm", "corpus/workloads/compute/linked_list.wasm", "corpus/workloads/synthetic/memory_tree.wasm", "corpus/workloads/synthetic/many_funcs.wasm", "corpus/workloads/polybench/jacobi-1d.wasm", "corpus/workloads/polybench/gemm.wasm", "corpus/workloads/assemblyscript/blake-as.wasm", "corpus/workloads/assemblyscript/blake-as-simd.wasm", "corpus/workloads/assemblyscript/utf-as-simd.wasm"}

func TestExperimentalCoverage(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, path := range experimentCorpusPaths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			m := experimentCorpusModule(t, path)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			labels := map[string]int{}
			for i, x := range stats.Funcs {
				for _, label := range []string{"linear-sum-unroll4", "region-loop-wide-independent", "region-loop-wide-adjacent", "region-loop-fast"} {
					if x.Peephole[label] > 0 {
						labels[label] += x.Peephole[label]
						t.Logf("accepted-native function=%d label=%s function-bytes=%d", i, label, x.CodeBytes)
					}
				}
			}
			for _, mode := range []string{"count2", "simd2", "vector-f32", "vector-i32"} {
				accepted := 0
				rejections := map[string]int{}
				for i, f := range m.Code {
					why := ""
					if mode == "vector-f32" || mode == "vector-i32" {
						var p shared.MapPlan
						why = shared.InspectMap(m, i, mode == "vector-f32", &p)
					} else {
						var p shared.ReplicationPlan
						why = shared.InspectReplication(f.BodyBytes, m, 2, false, mode == "simd2", &p)
					}
					if why == "" {
						accepted++
						t.Logf("accepted-plan mode=%s function=%d", mode, i)
					} else {
						rejections[why]++
					}
				}
				t.Logf("plan mode=%s accepted=%d rejected=%v", mode, accepted, rejections)
			}
			loops, simd := 0, 0
			classify := wasm.NewModuleInstructionClassifier(m, true)
			for _, f := range m.Code {
				r := wasm.ReaderFrom(f.BodyBytes)
				for r.BytesLeft() > 0 {
					op, err := r.Byte()
					if err != nil {
						break
					}
					var imm wasm.InstructionImmediate
					if classify.ClassifyInto(&r, op, &imm) != nil {
						break
					}
					if op == 3 {
						loops++
					}
					if op == 0xfd {
						simd++
					}
				}
			}
			t.Logf("module functions=%d loops=%d simd-instructions=%d native-bytes=%d labels=%v", len(m.Code), loops, simd, len(cm.Code), labels)
		})
	}
}
func TestExperimentalNativeArtifacts(t *testing.T) {
	requireCompilerDiagnostics(t)
	out := os.Getenv("WAGO_EXPERIMENT_ARTIFACT_DIR")
	if out == "" {
		t.Skip("set WAGO_EXPERIMENT_ARTIFACT_DIR to save evidence")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"map-i32", "dependent-i32", "dependent-f64", "pointer", "simd-i32", "map-f32"} {
		modes := []string{"scalar", "count2", "count4", "guard2"}
		if name == "simd-i32" {
			modes = []string{"scalar", "simd2", "simd4"}
		}
		if name == "map-i32" {
			modes = append(modes, "vector-i32")
		}
		if name == "map-f32" {
			modes = append(modes, "vector-f32")
		}
		for _, mode := range modes {
			m := loopExperimentModule(t, name)
			selected := mode
			if mode == "scalar" {
				selected = ""
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, AMD64FeaturesSet: true, ExperimentalLoopMode: selected})
			if err != nil {
				t.Fatal(err)
			}
			x := stats.Funcs[0]
			t.Logf("fixture=%s mode=%s module=%d function=%d frame=%d spills=%d reloads=%d slots=%d native-sha256=%x source-sha256=%x", name, mode, len(cm.Code), x.CodeBytes, x.FrameBytes, x.Spills, x.Reloads, x.MaxSpillSlots, sha256.Sum256(cm.Code), sha256.Sum256(m.Code[0].BodyBytes))
			if err := os.WriteFile(filepath.Join(out, name+"-"+mode+".bin"), cm.Code, 0644); err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
		}
	}
}

func TestExperimentalPressure(t *testing.T) {
	m := loopExperimentModule(t, "map-i32")
	var p shared.MapPlan
	if why := shared.InspectMap(m, 0, false, &p); why != "" {
		t.Fatal(why)
	}
	ft := &m.Types[0].SubTypes[0].Comp
	for i := 0; i < 9; i++ {
		ft.Params = append(ft.Params, wasm.I32)
		ft.Results = append(ft.Results, wasm.I32)
	}
	body := append([]byte(nil), m.Code[0].BodyBytes...)
	body[p.Loop.BodyStart+int(p.Loop.Operations[7].Start)+1] = 14
	// The original return is local.get 5; update the declared local's new index.
	body[len(body)-2] = 14
	body = body[:len(body)-1]
	for i := byte(5); i < 14; i++ {
		body = append(body, 0x20, i)
	}
	body = append(body, 0x0b)
	m.Code[0].BodyBytes = body
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	ref := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true}, 65536)
	args := []uint64{128, 32768, 17, 3, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41}
	for _, mode := range []string{"count2", "count4", "guard2", "vector-i32"} {
		got := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 65536)
		for i := range ref.jm.CurrentBytes() {
			ref.jm.CurrentBytes()[i] = byte(i*19 + 1)
		}
		copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
		a, b := ref.call(args...), got.call(args...)
		if a != nil || b != nil {
			t.Fatal(a, b)
		}
		for i := 0; i < 13*8; i += 8 {
			if binary.LittleEndian.Uint64(ref.out[i:]) != binary.LittleEndian.Uint64(got.out[i:]) {
				t.Fatal("live pressure value changed", mode, i)
			}
		}
		if diagnosticsEnabled {
			var s ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode, Stats: &s})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s frame=%d spills=%d reloads=%d native=%d", mode, s.Funcs[0].FrameBytes, s.Funcs[0].Spills, s.Funcs[0].Reloads, len(cm.Code))
			cm.CodeImage.Close()
		}
	}
}
func BenchmarkExperimentalCorpusCompile(b *testing.B) {
	mode := os.Getenv("WAGO_LOOP_REPLICATION")
	for _, path := range experimentCorpusPaths {
		m := experimentCorpusModule(b, path)
		b.Run(filepath.Base(path), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{ExperimentalLoopMode: mode, Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
		})
	}
}
func TestExperimentalSettings(t *testing.T) {
	keys := []string{"WAGO_LOOP_SUM_EXPERIMENT", "WAGO_LOOP_REPLICATION", "WAGO_LOOP_REDUCTION_FORMS", "WAGO_LOOP_F64_MODE", "WAGO_AMD64_VECTOR_MAP_ASSERT_FAST"}
	sort.Strings(keys)
	for _, key := range keys {
		t.Logf("%s=%q", key, os.Getenv(key))
	}
}
