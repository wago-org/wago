// Command draglinemetrics compiles one validated module through Dragline and
// writes canonical machine-readable compiler measurements. On a function
// failure, -replay writes the corresponding strict replay artifact.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func main() {
	outPath := flag.String("out", "", "write JSON metrics to this path instead of stdout")
	markdownPath := flag.String("markdown", "", "write a Markdown status projection to this path")
	codePath := flag.String("code", "", "write the generated native code image to this path")
	layoutPath := flag.String("layout", "", "write generated entry offsets to this path")
	replayPath := flag.String("replay", "", "write a replay artifact here if a function fails")
	targetMode := flag.String("target", "compat", "target mode: compat or native")
	boundsMode := flag.String("bounds", "explicit", "bounds mode: explicit or signals")
	scheduleMode := flag.String("schedule", "auto", "diagnostic schedule: auto, source, latency, or pressure")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: draglinemetrics [-target compat|native] [-bounds explicit|signals] [-schedule auto|source|latency|pressure] [-out metrics.json] [-markdown status.md] [-replay failure.json] module.wasm")
		os.Exit(2)
	}
	if *targetMode != "compat" && *targetMode != "native" {
		fmt.Fprintln(os.Stderr, "draglinemetrics: -target must be compat or native")
		os.Exit(2)
	}
	if *boundsMode != "explicit" && *boundsMode != "signals" {
		fmt.Fprintln(os.Stderr, "draglinemetrics: -bounds must be explicit or signals")
		os.Exit(2)
	}
	schedule, ok := parseScheduleDiagnostic(*scheduleMode)
	if !ok {
		fmt.Fprintln(os.Stderr, "draglinemetrics: -schedule must be auto, source, latency, or pressure")
		os.Exit(2)
	}

	source, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fail("read module", err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		fail("decode module", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		fail("validate module", err)
	}

	metrics := dragline.Metrics{ScheduleOverride: schedule}
	compiler := dragline.Compiler{Metrics: &metrics}
	if *replayPath != "" {
		compiler.Replay = func(replay corecompiler.ReplayArtifact) error {
			encoded, err := corecompiler.MarshalReplay(replay)
			if err != nil {
				return err
			}
			return os.WriteFile(*replayPath, append(encoded, '\n'), 0o644)
		}
	}
	mode := corecompiler.TargetCompatibility
	if *targetMode == "native" {
		mode = corecompiler.TargetNative
	}
	bounds := corecompiler.BoundsExplicit
	if *boundsMode == "signals" {
		bounds = corecompiler.BoundsSignals
	}
	target, err := corecompiler.HostTarget(mode)
	if err != nil {
		fail("resolve target", err)
	}
	compiled, err := compiler.Compile(corecompiler.Input{
		Module: m, Source: source,
		Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target:  target,
		Bounds:  bounds,
	})
	if err != nil {
		fail("compile", err)
	}
	if *codePath != "" {
		if err := os.WriteFile(*codePath, compiled.Code, 0o644); err != nil {
			fail("write code", err)
		}
	}
	if *layoutPath != "" {
		layout, err := json.MarshalIndent(struct {
			Entry         []int `json:"entry"`
			InternalEntry []int `json:"internal_entry"`
		}{compiled.Entry, compiled.InternalEntry}, "", "  ")
		if err != nil {
			fail("encode layout", err)
		}
		if err := os.WriteFile(*layoutPath, append(layout, '\n'), 0o644); err != nil {
			fail("write layout", err)
		}
	}

	var output = os.Stdout
	if *outPath != "" {
		output, err = os.Create(*outPath)
		if err != nil {
			fail("create metrics", err)
		}
		defer output.Close()
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metrics); err != nil {
		fail("write metrics", err)
	}
	if *markdownPath != "" {
		markdown, err := os.Create(*markdownPath)
		if err != nil {
			fail("create Markdown status", err)
		}
		if err := writeMarkdownStatus(markdown, flag.Arg(0), &metrics); err != nil {
			markdown.Close()
			fail("write Markdown status", err)
		}
		if err := markdown.Close(); err != nil {
			fail("close Markdown status", err)
		}
	}
}

func parseScheduleDiagnostic(value string) (dragline.ScheduleDiagnosticKind, bool) {
	switch value {
	case "auto":
		return dragline.ScheduleDiagnosticAuto, true
	case "source":
		return dragline.ScheduleDiagnosticSourceStable, true
	case "latency":
		return dragline.ScheduleDiagnosticLatencyFusion, true
	case "pressure":
		return dragline.ScheduleDiagnosticPressure, true
	default:
		return dragline.ScheduleDiagnosticAuto, false
	}
}

func writeMarkdownStatus(w io.Writer, modulePath string, metrics *dragline.Metrics) error {
	if metrics == nil {
		return fmt.Errorf("nil metrics")
	}
	if _, err := fmt.Fprintf(w, "# Dragline compiler status\n\n- Module: `%s`\n- Metrics schema: `%d`\n- Target fingerprint: `%x`\n- Schedule diagnostic: %s\n- Total native image: %d bytes\n- Peak compiler-owned live storage: %d bytes\n- Target-selected machine instructions: %d\n- Generic machine instructions: %d\n\n", filepath.Base(modulePath), metrics.Version, metrics.TargetFingerprint, scheduleDiagnosticName(metrics.ScheduleOverride), metrics.NativeBytes, metrics.PeakLiveBytes, metrics.TargetSelectedInstructions, metrics.GenericMachineInstructions); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| Emitter | Functions | Wasm body bytes | Native bytes | Lower ms | Emit ms | Cache hits |\n|---|---:|---:|---:|---:|---:|---:|"); err != nil {
		return err
	}
	for _, row := range []struct {
		name string
		data dragline.EmitterMetrics
	}{{"RailMach", metrics.RailMach}, {"Structured", metrics.Structured}} {
		if _, err := fmt.Fprintf(w, "| %s | %d | %d | %d | %.3f | %.3f | %d |\n", row.name, row.data.Functions, row.data.BodyBytes, row.data.NativeBytes, float64(row.data.LowerNanos)/1e6, float64(row.data.EmitNanos)/1e6, row.data.CacheHits); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\n## Functions\n\n| Function | Emitter | Reason | Wasm bytes | Native bytes | Memory accesses | Proved checks | Reused checks | Spill debt | Edge moves | Loop-backedge moves | Fixed moves | Segmented trial | Trial debt | Trial copies | Intervals | Segments | Segmented ranges | Fragments | Lower ms | Emit ms |\n|---:|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|"); err != nil {
		return err
	}
	for _, row := range metrics.Functions {
		if row.NativeBytes == 0 {
			continue
		}
		emitter := "Structured"
		if row.RailMachFinalized {
			emitter = "RailMach"
		}
		trial, trialDebt, trialCopies := "-", "-", "-"
		if row.SegmentedAttempted {
			trial = "rejected"
			if row.SegmentedAdmitted {
				trial = "admitted"
			}
			trialDebt = fmt.Sprintf("%d → %d", row.SegmentedBaselineDebt, row.SegmentedCandidateDebt)
			trialCopies = fmt.Sprintf("%d → %d", row.SegmentedBaselineCopies, row.SegmentedCandidateCopies)
		}
		reason := row.StructuredReason
		if reason == "" {
			reason = "-"
		}
		if _, err := fmt.Fprintf(w, "| %d | %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %s | %s | %s | %d | %d | %d | %d | %.3f | %.3f |\n", row.Function, emitter, reason, row.BodyBytes, row.NativeBytes, row.MemoryAccesses, row.BoundsChecksElided, row.BoundsChecksReused, row.WeightedSpillDebt, row.EdgeMoves, row.LoopBackedgeMoves, row.FixedMoves, trial, trialDebt, trialCopies, row.LiveIntervals, row.LiveSegments, row.SegmentedRanges, row.AllocationFragments, float64(row.LowerNanos)/1e6, float64(row.EmitNanos)/1e6); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\n## Schedule candidates\n\nInitial and bounded allocator-retry candidates are scored separately after allocation and late SSA exit. Final kind identifies the schedule kind ultimately retained. Metrics-enabled compilation also plans candidate post-RA opportunities; each phase's frontier remains advisory until exact realized native bytes join the score.\n\n| Function | Phase | Candidate | Kind | Final kind | Candidate frontier | Estimated cycles | Resource cycles | Selected-rule bytes | Post-RA rewrites | Planned elisions | Wrap spills | Eliminated moves | Spill debt | Physical copies | Copy cycles | Copy motion | Fixed repairs | Broken fusions | Loop-invariant ops |\n|---:|---|---:|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|"); err != nil {
		return err
	}
	for _, row := range metrics.Functions {
		writeCandidates := func(phase string, scores [3]dragline.ScheduleCandidateMetrics, count uint8) error {
			limit := min(int(count), len(scores))
			for index, score := range scores[:limit] {
				retained := ""
				if score.Kind == row.ScheduleKind {
					retained = "yes"
				}
				frontier := ""
				if score.Nondominated {
					frontier = "yes"
				}
				if _, err := fmt.Fprintf(w, "| %d | %s | %d | %s | %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n", row.Function, phase, index+1, scheduleKindName(score.Kind), retained, frontier, score.EstimatedCycles, score.ResourceCycles, score.SelectedBytes, score.PostRARewrites, score.PostRAElisions, score.PostRAWrapSpills, score.EliminatedMoves, score.WeightedSpillDebt, score.PhysicalCopies, score.CopyCycles, score.CopyMotion, score.FixedRepairs, score.BrokenFusions, score.LoopInvariantOps); err != nil {
					return err
				}
			}
			return nil
		}
		if err := writeCandidates("initial", row.InitialScheduleScores, row.InitialScheduleScoreCount); err != nil {
			return err
		}
		if err := writeCandidates("retry", row.RetryScheduleScores, row.RetryScheduleScoreCount); err != nil {
			return err
		}
	}
	return nil
}

func scheduleDiagnosticName(kind dragline.ScheduleDiagnosticKind) string {
	switch kind {
	case dragline.ScheduleDiagnosticAuto:
		return "auto"
	case dragline.ScheduleDiagnosticSourceStable:
		return "source"
	case dragline.ScheduleDiagnosticLatencyFusion:
		return "latency"
	case dragline.ScheduleDiagnosticPressure:
		return "pressure"
	default:
		return fmt.Sprintf("unknown-%d", kind)
	}
}

func scheduleKindName(kind uint8) string {
	switch kind {
	case uint8(railmach.ScheduleKindSourceStable):
		return "source-stable"
	case uint8(railmach.ScheduleKindLatencyFusion):
		return "latency/fusion"
	case uint8(railmach.ScheduleKindPressure):
		return "pressure"
	default:
		return fmt.Sprintf("unknown-%d", kind)
	}
}

func fail(operation string, err error) {
	fmt.Fprintf(os.Stderr, "draglinemetrics: %s: %v\n", operation, err)
	os.Exit(1)
}
