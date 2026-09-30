package profilecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/wago-org/wago"
)

type costChange struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Delta  float64 `json:"delta"`
}

func changedCost(before, after float64) costChange {
	return costChange{Before: before, After: after, Delta: after - before}
}

type functionDelta struct {
	BeforeCompiler *wago.CodeProfileFunction `json:"before_compiler,omitempty"`
	AfterCompiler  *wago.CodeProfileFunction `json:"after_compiler,omitempty"`
	Module         string                    `json:"module"`
	Function       int                       `json:"function"`
	Name           string                    `json:"name"`
	Before         float64                   `json:"before_cpu_ns_per_iteration"`
	After          float64                   `json:"after_cpu_ns_per_iteration"`
	Delta          float64                   `json:"delta_cpu_ns_per_iteration"`
}

type cpuComparison struct {
	Total         costChange `json:"total_ns_per_iteration"`
	Functions     costChange `json:"function_ns_per_iteration"`
	Helpers       costChange `json:"helper_ns_per_iteration"`
	Unknown       costChange `json:"unknown_ns_per_iteration"`
	BeforeSamples uint64     `json:"before_samples"`
	AfterSamples  uint64     `json:"after_samples"`
	BeforeUnknown uint64     `json:"before_unknown_samples"`
	AfterUnknown  uint64     `json:"after_unknown_samples"`
}

type comparison struct {
	Version          int             `json:"version"`
	BeforeIterations uint64          `json:"before_completed_iterations"`
	AfterIterations  uint64          `json:"after_completed_iterations"`
	Elapsed          costChange      `json:"elapsed_ns_per_iteration"`
	CPU              *cpuComparison  `json:"sampled_cpu,omitempty"`
	Functions        []functionDelta `json:"functions"`
	Diagnostics      []string        `json:"diagnostics"`
}

func compareCaptures(a, b capture) (comparison, error) {
	var result comparison
	x, y := a.manifest, b.manifest
	if x.RawStackBytes != y.RawStackBytes || x.StackCollection != y.StackCollection || x.RequestedRate != y.RequestedRate {
		return result, fmt.Errorf("captures have incompatible stack collection or sampling rates")
	}
	if x.Backend == "samply" || y.Backend == "samply" {
		return result, fmt.Errorf("samply observations are not qualified CPU cost; compare raw profiles in Samply")
	}
	if x.PhaseIsolation != y.PhaseIsolation || x.SourceMapsRequested != y.SourceMapsRequested || x.UnwindMapsRequested != y.UnwindMapsRequested || x.CodeIncluded != y.CodeIncluded || x.CollectorVersion != y.CollectorVersion {
		return result, fmt.Errorf("captures have incompatible phase isolation, metadata collection, or collector versions")
	}
	if x.ReloadArtifact != y.ReloadArtifact || x.CPUModel != y.CPUModel || x.OSVersion != y.OSVersion || x.CPUs != y.CPUs || x.WorkloadHash != y.WorkloadHash || x.Target != y.Target || x.Mode != y.Mode || x.TimelineCoverage != y.TimelineCoverage || x.Phase != y.Phase || x.Event != y.Event || x.Backend != y.Backend || x.Warmup != y.Warmup {
		return result, fmt.Errorf("captures have incompatible workload, target, mode, artifact reload, phase, event, backend, or warmup")
	}
	if x.Phase != "execute" {
		return result, fmt.Errorf("diff currently compares the execute phase; inspect other phases in the raw viewer")
	}
	if x.Iterations == 0 || y.Iterations == 0 || x.ActualNS <= 0 || y.ActualNS <= 0 {
		return result, fmt.Errorf("comparison needs completed work and positive execution durations")
	}
	result = comparison{
		Version: 1, BeforeIterations: x.Iterations, AfterIterations: y.Iterations,
		Elapsed:     changedCost(float64(x.ActualNS)/float64(x.Iterations), float64(y.ActualNS)/float64(y.Iterations)),
		Functions:   []functionDelta{},
		Diagnostics: []string{"A single pair does not establish statistical significance."},
	}
	if x.Backend != "perf" {
		result.Diagnostics = append(result.Diagnostics, "No native CPU comparison is available for this backend.")
		return result, nil
	}
	if x.Event != "cpu-clock:u" || a.report.Unit != "nanoseconds" || b.report.Unit != "nanoseconds" || x.PhaseIsolation != "collector-start-stop" {
		return comparison{}, fmt.Errorf("CPU comparison needs phase-isolated cpu-clock:u samples with nanosecond weights")
	}
	if a.report.Samples == 0 || b.report.Samples == 0 {
		return comparison{}, fmt.Errorf("CPU comparison needs observed samples in both captures")
	}
	result.Diagnostics = append(result.Diagnostics, "CPU values are sampled self-time estimates. Zero function weight means no samples observed, not proven zero execution cost.", "Unknown CPU includes host/runtime code and unresolved guest PCs; it is not exclusively host CPU.")
	result.CPU = &cpuComparison{
		Total:         changedCost(float64(a.report.Weight)/float64(x.Iterations), float64(b.report.Weight)/float64(y.Iterations)),
		Unknown:       changedCost(float64(a.report.UnknownWeight)/float64(x.Iterations), float64(b.report.UnknownWeight)/float64(y.Iterations)),
		BeforeSamples: a.report.Samples, AfterSamples: b.report.Samples,
		BeforeUnknown: a.report.UnknownSamples, AfterUnknown: b.report.UnknownSamples,
	}
	type identity struct {
		module   string
		function int
	}
	rows := map[identity]*functionDelta{}
	var functionCosts, helperCosts [2]float64
	for side, c := range []capture{a, b} {
		// Multiple compilation variants can share a logical function. Aggregate
		// their sampled cost, but never attach an arbitrary variant's counters.
		variants := map[identity]map[string]struct{}{}
		for _, r := range c.report.Rows {
			cost := float64(r.Weight) / float64(c.manifest.Iterations)
			if r.Function < 0 {
				helperCosts[side] += cost
				continue
			}
			functionCosts[side] += cost
			key := identity{r.ModuleID, r.Function}
			d := rows[key]
			if d == nil {
				d = &functionDelta{Module: r.ModuleID, Function: r.Function, Name: r.Name}
				rows[key] = d
			}
			if side == 0 {
				d.Before += cost
			} else {
				d.After += cost
			}
			if variants[key] == nil {
				variants[key] = map[string]struct{}{}
			}
			variants[key][r.ArtifactID] = struct{}{}
		}
		for _, e := range c.events {
			if e.Image == nil {
				continue
			}
			for _, fn := range e.Image.Functions {
				key := identity{e.Image.ModuleID, fn.Index}
				v := variants[key]
				if _, ok := v[e.Image.ArtifactID]; !ok || len(v) != 1 {
					continue
				}
				copy := fn
				if side == 0 {
					rows[key].BeforeCompiler = &copy
				} else {
					rows[key].AfterCompiler = &copy
				}
			}
		}
		for key, v := range variants {
			if len(v) > 1 {
				result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("Capture %d: %s/f%d has multiple sampled artifacts; compiler counters omitted.", side+1, key.module, key.function))
			}
		}
	}
	result.CPU.Functions = changedCost(functionCosts[0], functionCosts[1])
	result.CPU.Helpers = changedCost(helperCosts[0], helperCosts[1])
	for _, d := range rows {
		d.Delta = d.After - d.Before
		result.Functions = append(result.Functions, *d)
	}
	sort.Slice(result.Functions, func(i, j int) bool {
		a, b := result.Functions[i], result.Functions[j]
		if a.Delta != b.Delta {
			return a.Delta > b.Delta
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Function < b.Function
	})
	// Map iteration must not make otherwise identical machine reports differ.
	sort.Strings(result.Diagnostics)
	return result, nil
}

func diff(a, b capture, asJSON bool) error {
	result, err := compareCaptures(a, b)
	if err != nil {
		return err
	}
	return writeComparison(os.Stdout, result, asJSON)
}

func writeComparison(w io.Writer, result comparison, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(result)
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "Elapsed ns/iteration: %.1f → %.1f (%+.1f; wall time, not CPU time)\n", result.Elapsed.Before, result.Elapsed.After, result.Elapsed.Delta)
	if cpu := result.CPU; cpu != nil {
		fmt.Fprintln(&out, "Sampled CPU ns/iteration: baseline → candidate (delta)")
		for _, row := range []struct {
			name string
			cost costChange
		}{
			{"Total", cpu.Total}, {"Guest functions and owned adapters", cpu.Functions},
			{"Helpers", cpu.Helpers}, {"Unknown/non-guest", cpu.Unknown},
		} {
			fmt.Fprintf(&out, "%-35s %12.1f → %12.1f (%+.1f)\n", row.name, row.cost.Before, row.cost.After, row.cost.Delta)
		}
		for _, d := range result.Functions {
			fmt.Fprintf(&out, "f%-6d %-30s %12.1f → %12.1f (%+.1f)\n", d.Function, d.Name, d.Before, d.After, d.Delta)
			if p, q := d.BeforeCompiler, d.AfterCompiler; p != nil && q != nil {
				fmt.Fprintf(&out, "  static: bytes %d→%d, frame %d→%d, spills %d→%d, reloads %d→%d, bounds checks %d→%d (no measured saving implied)\n", p.NativeBytes, q.NativeBytes, p.FrameBytes, q.FrameBytes, p.Spills, q.Spills, p.Reloads, q.Reloads, p.BoundsChecks, q.BoundsChecks)
			}
		}
		fmt.Fprintf(&out, "Samples: %d → %d; unknown/non-guest: %d → %d.\n", cpu.BeforeSamples, cpu.AfterSamples, cpu.BeforeUnknown, cpu.AfterUnknown)
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintln(&out, diagnostic)
	}
	_, err := out.WriteTo(w)
	return err
}
