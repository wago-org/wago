package profilecmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
)

// An experiment consumes alternating, independently saved baseline/candidate
// captures. It reports paired distributions, not a significance claim.
type experimentResult struct {
	Version             int          `json:"version"`
	Pairs               []comparison `json:"pairs"`
	BeforeRevision      string       `json:"before_revision"`
	AfterRevision       string       `json:"after_revision"`
	MedianBeforeNS      float64      `json:"median_before_ns_per_work"`
	MedianAfterNS       float64      `json:"median_after_ns_per_work"`
	MedianPairedDeltaNS float64      `json:"median_paired_delta_ns_per_work"`
	MedianPairedPercent float64      `json:"median_paired_percent"`
	MinPairedPercent    float64      `json:"min_paired_percent"`
	MaxPairedPercent    float64      `json:"max_paired_percent"`
	ImprovedPairs       int          `json:"improved_pairs"`
	Diagnostics         []string     `json:"diagnostics"`
}

func median(values []float64) float64 {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	n := len(ordered)
	if n%2 != 0 {
		return ordered[n/2]
	}
	return (ordered[n/2-1] + ordered[n/2]) / 2
}

func executionStart(c capture) int64 {
	for _, p := range c.manifest.Phases {
		if p.Name == "execute" {
			return p.Start
		}
	}
	return 0
}

func compareExperiment(captures []capture) (experimentResult, error) {
	var result experimentResult
	if len(captures) < 4 || len(captures)%2 != 0 {
		return result, fmt.Errorf("experiment needs at least two alternating baseline/candidate pairs")
	}
	result.Version = 1
	result.BeforeRevision = captures[0].manifest.Revision
	result.AfterRevision = captures[1].manifest.Revision
	result.Pairs = make([]comparison, 0, len(captures)/2)
	var before, after, delta, percent []float64
	var lastStart int64
	orderKnown := true
	for i := 0; i < len(captures); i += 2 {
		a, b := captures[i], captures[i+1]
		if a.manifest.Revision != result.BeforeRevision || b.manifest.Revision != result.AfterRevision {
			return experimentResult{}, fmt.Errorf("pair %d has inconsistent baseline or candidate revision", i/2+1)
		}
		if i != 0 {
			if a.manifest.ModuleHash != captures[0].manifest.ModuleHash || b.manifest.ModuleHash != captures[1].manifest.ModuleHash || !reflect.DeepEqual(a.manifest.Config, captures[0].manifest.Config) || !reflect.DeepEqual(b.manifest.Config, captures[1].manifest.Config) {
				return experimentResult{}, fmt.Errorf("pair %d changes module or effective configuration within a build", i/2+1)
			}
			if _, err := compareCaptures(captures[0], a); err != nil {
				return experimentResult{}, fmt.Errorf("pair %d baseline differs from the first pair: %w", i/2+1, err)
			}
			if _, err := compareCaptures(captures[1], b); err != nil {
				return experimentResult{}, fmt.Errorf("pair %d candidate differs from the first pair: %w", i/2+1, err)
			}
		}
		pair, err := compareCaptures(a, b)
		if err != nil {
			return experimentResult{}, fmt.Errorf("pair %d: %w", i/2+1, err)
		}
		first, second := executionStart(a), executionStart(b)
		if first == 0 || second == 0 {
			orderKnown = false
		} else if first <= lastStart || second <= first {
			return experimentResult{}, fmt.Errorf("pair %d is not in alternating capture order", i/2+1)
		}
		if second != 0 {
			lastStart = second
		}
		result.Pairs = append(result.Pairs, pair)
		before = append(before, pair.Elapsed.Before)
		after = append(after, pair.Elapsed.After)
		delta = append(delta, pair.Elapsed.Delta)
		change := pair.Elapsed.Delta / pair.Elapsed.Before * 100
		percent = append(percent, change)
		if change < 0 {
			result.ImprovedPairs++
		}
	}
	result.MedianBeforeNS, result.MedianAfterNS = median(before), median(after)
	result.MedianPairedDeltaNS, result.MedianPairedPercent = median(delta), median(percent)
	sort.Float64s(percent)
	result.MinPairedPercent, result.MaxPairedPercent = percent[0], percent[len(percent)-1]
	result.Diagnostics = []string{"Paired medians and range describe these runs; they do not establish statistical significance or production-build speedups."}
	if !orderKnown {
		result.Diagnostics = append(result.Diagnostics, "One or more execution timestamps are missing; alternating order could not be verified.")
	}
	if result.BeforeRevision == "" || result.BeforeRevision == "unknown" || result.AfterRevision == "" || result.AfterRevision == "unknown" {
		result.Diagnostics = append(result.Diagnostics, "Build revision provenance is incomplete.")
	}
	return result, nil
}

func writeExperiment(w io.Writer, r experimentResult, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(r)
	}
	if _, err := fmt.Fprintf(w, "%d alternating pairs; baseline %s, candidate %s\n", len(r.Pairs), r.BeforeRevision, r.AfterRevision); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Median elapsed ns/work: %.1f → %.1f; paired median %+.1f ns (%+.2f%%), range %+.2f%% to %+.2f%%; faster in %d/%d pairs\n", r.MedianBeforeNS, r.MedianAfterNS, r.MedianPairedDeltaNS, r.MedianPairedPercent, r.MinPairedPercent, r.MaxPairedPercent, r.ImprovedPairs, len(r.Pairs)); err != nil {
		return err
	}
	for _, diagnostic := range r.Diagnostics {
		if _, err := fmt.Fprintln(w, diagnostic); err != nil {
			return err
		}
	}
	return nil
}

func experiment(args []string) error {
	f := flag.NewFlagSet("experiment", flag.ContinueOnError)
	asJSON := f.Bool("json", false, "machine-readable paired experiment summary")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 4 || f.NArg()%2 != 0 {
		return fmt.Errorf("usage: wagoprof experiment [--json] before1 after1 before2 after2 [before3 after3 ...]")
	}
	captures := make([]capture, 0, f.NArg())
	for _, dir := range f.Args() {
		c, err := loadCapture(dir)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		captures = append(captures, c)
	}
	result, err := compareExperiment(captures)
	if err != nil {
		return err
	}
	return writeExperiment(os.Stdout, result, *asJSON)
}
