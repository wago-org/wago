//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"path/filepath"
	"testing"
)

func TestWideReuseDepthAdmission(t *testing.T) {
	saved := wideReusePinsEnabled
	wideReusePinsEnabled = true
	defer func() { wideReusePinsEnabled = saved }()
	f := fn{nLocals: 80, intervalControl: true, policy: currentCodegenPolicy(), classifier: wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)}
	f.localType = make([]machineType, 80)
	f.intervalLast = make([]uint32, 80)
	f.intervalScore = make([]uint32, 80)
	for i := range f.localType {
		f.localType[i] = mtI32
		f.intervalLast[i] = 1
		f.intervalScore[i] = 100
	}
	// Outer-loop definition/read inside the nested loop qualifies. An inner-loop
	// mutation and a later definition at the read's nesting depth both decline.
	body := []byte{0x03, 0x40, 0x41, 7, 0x21, 70, 0x03, 0x40, 0x20, 70, 0x1a, 0x20, 71, 0x1a, 0x41, 1, 0x21, 71, 0x20, 72, 0x1a, 0x0b, 0x03, 0x40, 0x41, 2, 0x21, 72, 0x0b, 0x0b, 0x0b}
	f.prepareIntervalEvents(body, 40, false)
	if len(f.wideReuseScore) != 80 || f.wideReuseScore[70] != 90 || f.wideReuseScore[71] != 0 || f.wideReuseScore[72] != 0 {
		t.Fatalf("scores=%v", f.wideReuseScore)
	}

}

func TestWideReusePinsNative(t *testing.T) {
	requireCompilerDiagnostics(t)
	// Eight outer-defined locals compete for four pins. The inner counter and
	// loop-carried accumulator must retain their ordinary regional treatment.
	body := []byte{1, 80, 0x7f, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for x := byte(70); x <= 77; x++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(x-67))...)
		body = append(body, 0x21, x)
	}
	body = append(body, 0x20, 1, 0x04, 0x40, 0x41, 11, 0x21, 70, 0x05, 0x41, 3, 0x21, 70, 0x0b, 0x41, 3, 0x21, 3, 0x03, 0x40)
	body = append(body, 0x20, 78, 0x41, 1, 0x6a, 0x22, 78, 0x20, 2, 0x6a)
	for x := byte(70); x <= 77; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x21, 2, 0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0d, 0, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x0b, 0x20, 2, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"wide-reuse-pins": on}, Stats: &stats}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			count := stats.Funcs[0].Peephole["wide-reuse-local-pin"]
			if on && count != 4 || !on && count != 0 {
				t.Fatalf("on=%v guard=%v count=%d stats=%v", on, guard, count, stats.Funcs[0].Peephole)
			}
			opts.Stats = nil
			for _, n := range []uint64{0, 1, 2, 7, 31} {
				for _, condition := range []uint64{0, 1, 0x80000000} {
					k := 3 * n
					sum := uint64(52)
					if condition != 0 {
						sum += 8
					}
					want := k*sum + k*(k+1)/2
					got, err := runArm64WrapperWithOptions(t, m, opts, n, condition)
					if err != nil || got != want {
						t.Fatalf("on=%v guard=%v n=%d cond=%x got=%d want=%d err=%v", on, guard, n, condition, got, want, err)
					}
				}
			}
		}
	}
}

// Optional diagnostic compilation only; source identities do not enter policy.
func TestWideReuseCorpusProbe(t *testing.T) {
	requireCompilerDiagnostics(t)
	root := os.Getenv("WAGO_WIDE_REUSE_PROBE")
	if root == "" {
		t.Skip("optional corpus diagnostic")
	}
	for _, name := range []string{"video-dct", "graphics-reed-solomon", "crypto-aes128", "vision-dilation"} {
		data, err := os.ReadFile(filepath.Join(root, "artifacts", name+".wasm"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Stats: &stats, Optimizations: map[string]bool{"wide-reuse-pins": true}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		for i, f := range stats.Funcs {
			t.Logf("%s f%d %v", name, i, f.Peephole)
		}
	}
}

func TestWideReuseInnerDefinitionsNative(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 80, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0, 0x03, 0x40}
	for x := byte(70); x <= 77; x++ {
		body = append(body, 0x20, 1, 0x41, x-70, 0x6a, 0x21, x)
	}
	body = append(body, 0x20, 2)
	for repeat := 0; repeat < 4; repeat++ {
		for x := byte(70); x <= 77; x++ {
			body = append(body, 0x20, x, 0x6a)
		}
	}
	body = append(body, 0x21, 2, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x0b, 0x20, 2, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"wide-reuse-pins": on}, Stats: &stats}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			count := stats.Funcs[0].Peephole["wide-reuse-local-pin"]
			if on && count != 4 || !on && count != 0 {
				t.Fatalf("on=%v guard=%v pins=%d stats=%v", on, guard, count, stats.Funcs[0].Peephole)
			}
			opts.Stats = nil
			for _, n := range []uint64{0, 1, 2, 7, 31} {
				for _, seed := range []uint64{0, 1, 0xffffffff, 0x80000000, 0xdeadbeef80000001} {
					want := uint64(uint32(n * (32*uint64(uint32(seed)) + 112)))
					got, err := runArm64WrapperWithOptions(t, m, opts, n, seed)
					if err != nil || got != want {
						t.Fatalf("on=%v guard=%v n=%d seed=%x got=%d want=%d err=%v", on, guard, n, seed, got, want, err)
					}
				}
			}
		}
	}
}

func TestWideReuseTrafficSaturation(t *testing.T) {
	if saturatingWideTraffic(^uint32(0)-3, 4) != ^uint32(0) || saturatingWideTraffic(10, 20) != 30 {
		t.Fatal("traffic overflow")
	}
}
