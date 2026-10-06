//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	"golang.org/x/sys/cpu"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

var workerResetNames = []string{"tiny", "large", "branches", "float", "simd", "memory", "divide", "call", "locals", "control", "large-scalar"}

type workerResetContext struct {
	m        *wasm.Module
	hints    []funcHints
	sidecar  funcHintSidecar
	policy   CodegenPolicy
	features shared.AMD64Features
	types    moduleTypeCache
}

type workerResetOutput struct {
	Code                    []byte
	Relocs                  []callReloc
	Internal                int
	Features                shared.AMD64Features
	BMI2                    bool
	BitCount                uint8
	Direct, Bounded, Shared bool
	Literals                []uint64
	Metadata                []byte
}

func workerResetLoad(t testing.TB, width int, compact bool, features shared.AMD64Features) *workerResetContext {
	t.Helper()
	data := workerResetModule(width)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	featuresForModule := frontend.AllFeatures()
	featuresForModule.Memory64 = width == 64
	if err := frontend.RejectUnsupportedWithFeatures(m, featuresForModule); err != nil {
		t.Fatal(err)
	}
	policy := currentCodegenPolicy()
	if compact {
		policy = shared.CompactCodegenPolicy(policy.Selection)
	}
	hints, sidecar, _, err := computeModuleHintsWithPolicy(m, 0, 0, nil, false, policy, false)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, f := range m.Code {
		total += len(f.BodyBytes)
	}
	return &workerResetContext{m, hints, sidecar, policy, features, buildModuleTypeCache(m, total)}
}

func (c *workerResetContext) worker() *scratch {
	sc := newCompileScratch(defaultStackArenaCap)
	sc.policy, sc.amd64Features = c.policy, c.features
	sc.classifier = wasm.NewModuleInstructionClassifier(c.m, true)
	sc.moduleTypes = c.types
	// Fixed module-wide reservations for both fresh and reused workers.
	sc.reserveLocalScratch(193)
	sc.reserveControlFrames(moduleControlFrameCap(c.m, c.hints))
	return sc
}

func (c *workerResetContext) compile(sc *scratch, index int, capture bool) (workerResetOutput, error) {
	// Reuse the worker output buffer. Refresh the per-function relocation
	// recorders as the parallel driver does; compileFunc resets their lengths.
	sc.asm.Rel32Sites = nil
	sc.rel32TailBound = false
	sc.localRefs.Sites = nil
	sc.localRefTailBound = false
	h := c.sidecar.viewAt(c.hints[index], index)
	var st *CodegenStats
	if capture {
		st = &CodegenStats{FuncIdx: index, RecordSources: profileEnabled, RecordUnwind: profileEnabled, RecordAdapterUnwind: profileEnabled}
	}
	code, relocs, internal, err := compileFunc(c.m, nil, index, true, false, true, false, true,
		nil, &h, nil, nil, false, rt.MaxHostArity, false, false, false, nil, nil, st, inlineTargetTable{}, sc)
	if err != nil {
		return workerResetOutput{}, err
	}
	if !capture {
		return workerResetOutput{}, nil
	}
	if profileEnabled && len(st.SourceRanges) == 0 {
		return workerResetOutput{}, fmt.Errorf("profile source ranges absent")
	}
	// Timing is the only normalized metadata. No code bytes, relocations,
	// source ranges, unwind records, CPU flags, or literal data are normalized.
	st.CompileNanos, st.ScalarAdmissionNanos = 0, 0
	metadata, err := json.Marshal(st)
	if err != nil {
		return workerResetOutput{}, err
	}
	return workerResetOutput{
		Code: append([]byte(nil), code...), Relocs: append([]callReloc(nil), relocs...), Internal: internal,
		Features: sc.usedAMD64Features, BMI2: sc.asm.UsesBMI2, BitCount: sc.asm.BitCountState,
		Direct: sc.directPrepared, Bounded: sc.directPreparedBounded, Shared: sc.fnState.scalarSummary.Eligible,
		Literals: append([]uint64(nil), sc.fnState.literalWords...), Metadata: metadata,
	}, nil
}

func workerResetCompile(t testing.TB, c *workerResetContext, sc *scratch, i int) workerResetOutput {
	t.Helper()
	out, err := c.compile(sc, i, true)
	if err != nil {
		t.Fatalf("%s: %v", workerResetNames[i], err)
	}
	return out
}

func workerResetClose(sc *scratch) { sc.finishControlWorker(); sc.finishStackWorker() }

func workerResetEqual(a, b workerResetOutput) string {
	if !bytes.Equal(a.Code, b.Code) {
		return "native code"
	}
	if !reflect.DeepEqual(a.Relocs, b.Relocs) {
		return "relocations"
	}
	if a.Features != b.Features || a.BMI2 != b.BMI2 || a.BitCount != b.BitCount {
		return "CPU feature metadata"
	}
	if !reflect.DeepEqual(a, b) {
		return "function/source/unwind/literal metadata"
	}
	return ""
}

func workerResetSequences() [][]int {
	sequences := [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, {10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}, {1, 0, 2, 0, 4, 0, 5, 0, 7, 0, 9, 0, 10, 0}}
	for i := range workerResetNames {
		sequences = append(sequences, []int{i})
	}
	return sequences
}

func TestWorkerScratchMatchesFresh(t *testing.T) {
	for _, width := range []int{32, 64} {
		for _, compact := range []bool{false, true} {
			for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
				t.Run(fmt.Sprintf("memory%d/compact=%t/features=%x", width, compact, features), func(t *testing.T) {
					c := workerResetLoad(t, width, compact, features)
					fresh := make([]workerResetOutput, len(workerResetNames))
					sharedCount := 0
					for i, name := range workerResetNames {
						sc := c.worker()
						fresh[i] = workerResetCompile(t, c, sc, i)
						workerResetClose(sc)
						if fresh[i].Shared {
							sharedCount++
						}
						t.Logf("path %s shared=%t code=%d relocs=%d features=%x", name, fresh[i].Shared, len(fresh[i].Code), len(fresh[i].Relocs), fresh[i].Features)
					}
					if sharedScalarEnabled && (!fresh[0].Shared || !fresh[10].Shared) {
						t.Fatal("tiny and large scalar fixtures must use the shared path")
					}
					if sharedCount == len(fresh) {
						t.Fatal("no fallback path")
					}
					if len(fresh[7].Relocs) == 0 {
						t.Fatal("call fixture has no relocation")
					}
					if features != 0 && fresh[4].Features == 0 {
						t.Fatal("SIMD fixture did not use optional CPU features")
					}
					for seqIndex, seq := range workerResetSequences() {
						for target := range fresh {
							sc := c.worker()
							for _, prev := range seq {
								workerResetCompile(t, c, sc, prev)
							}
							got := workerResetCompile(t, c, sc, target)
							if difference := workerResetEqual(got, fresh[target]); difference != "" {
								workerResetClose(sc)
								t.Fatalf("sequence=%d target=%s mismatch: %s; features got=%x want=%x", seqIndex, workerResetNames[target], difference, got.Features, fresh[target].Features)
							}
							// Run only after the exact comparison has passed. Check each history,
							// including the call relocation, against an independent Go result.
							if features == 0 || (cpu.X86.HasSSSE3 && cpu.X86.HasSSE41 && cpu.X86.HasSSE42 && cpu.X86.HasAVX) {
								workerResetExecute(t, got, fresh[0], target)
							}
							if target == 0 && seqIndex == 0 {
								t.Logf("retained after sequence: %+v", workerScratchStats(sc))
							}
							workerResetClose(sc)
							if stats := workerScratchStats(sc); stats.ScalarRetained+stats.NodeRetained+stats.ControlRetained != 0 {
								t.Fatal("worker scratch not released")
							}
							if target == 0 && seqIndex == 0 {
								t.Logf("after worker release: %+v", workerScratchStats(sc))
							}
						}
					}
				})
			}
		}
	}
}

func workerResetExecute(t *testing.T, out, helper workerResetOutput, target int) {
	t.Helper()
	code := append([]byte(nil), out.Code...)
	helperStart := len(code)
	code = append(code, helper.Code...)
	for _, reloc := range out.Relocs {
		if reloc.target != 0 {
			t.Fatal("unexpected relocation target")
		}
		destination := helperStart
		if reloc.internal {
			destination += helper.Internal
		}
		binary.LittleEndian.PutUint32(code[int(reloc.at):], uint32(int32(destination-int(reloc.at)-4)))
	}
	words := append(append([]uint64(nil), out.Literals...), helper.Literals...)
	offsets := []uint32{0, uint32(len(out.Literals)), uint32(len(words))}
	literalCode, literals := buildModuleLiteralIsland(words, offsets)
	literalBase := len(code)
	code = append(code, literalCode...)
	if err := patchModuleLiteralRelocs(code, []int{0, helperStart}, words, offsets, literalBase, literals); err != nil {
		t.Fatal(err)
	}
	engine, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	image, entry, err := rt.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(image)
	args, result, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(rt.TrapBufferBytes)
	for _, x := range []uint64{0, 1, 7, 63, 0xffff, 1 << 63, ^uint64(0)} {
		binary.LittleEndian.PutUint64(args, x)
		clear(trap)
		callErr := engine.Call(entry, args, jm.LinearMemory(), trap, result)
		if target == 6 && x == 0 {
			var trapErr *rt.TrapError
			if !errors.As(callErr, &trapErr) || trapErr.Code != rt.TrapDivZero {
				t.Fatalf("zero divisor: %v", callErr)
			}
			continue
		}
		if err := callErr; err != nil {
			t.Fatalf("%s x=%x: %v", workerResetNames[target], x, err)
		}
		var want uint64
		switch target {
		case 0:
			want = x + 7
		case 1, 10:
			want = 160*x + 12880
		case 2:
			want = 23
		case 3:
			want = math.Float64bits(float64(x) + 1.25)
		case 4:
			want = 2 * x
		case 5:
			want = x
		case 6:
			want = 100 / x
		case 7:
			want = 3 * (x + 7)
		case 8:
			want = x ^ 13
		case 9:
			want = x
		}
		if got := binary.LittleEndian.Uint64(result); got != want {
			t.Fatalf("%s x=%x got=%x want=%x", workerResetNames[target], x, got, want)
		}
		if target == 5 && binary.LittleEndian.Uint64(jm.LinearMemory()) != x {
			t.Fatal("store result mismatch")
		}
	}
}

// Parallel module workers continue after a per-function error and discard all
// results if any function failed. Inject one bad opcode after valid lowering
// work; this is an internal failure control, not a validated Wasm product.
func TestWorkerScratchMatchesFreshAfterError(t *testing.T) {
	c := workerResetLoad(t, 32, false, shared.AMD64ModernBaseline)
	freshWorker := c.worker()
	want := workerResetCompile(t, c, freshWorker, 0)
	workerResetClose(freshWorker)
	sc := c.worker()
	defer workerResetClose(sc)
	workerResetCompile(t, c, sc, 4)
	original := c.m.Code[1].BodyBytes
	bad := append([]byte(nil), original...)
	bad[len(bad)-1] = 0xff
	c.m.Code[1].BodyBytes = bad
	_, err := c.compile(sc, 1, true)
	c.m.Code[1].BodyBytes = original
	if err == nil {
		t.Fatal("failure control was accepted")
	}
	t.Logf("controlled backend failure: %v", err)
	got := workerResetCompile(t, c, sc, 0)
	if difference := workerResetEqual(got, want); difference != "" {
		t.Fatal(difference)
	}
	workerResetExecute(t, got, want, 0)
}
