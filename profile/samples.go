package profile

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago/internal/jitprofile"
)

// Sample is a native PC observation, timestamped in the journal's clock. Period
// is the collector event's weight; CPU-clock uses nanoseconds, cycles do not.
type Sample struct {
	Timestamp uint64 `json:"timestamp_ns"`
	PC        uint64 `json:"pc"`
	Period    uint64 `json:"period"`
}

// InlineCaller is compiler-recorded ancestry, not a sampled native frame.
type InlineCaller struct {
	Function   uint32 `json:"function"`
	WasmOffset uint32 `json:"wasm_offset"`
	Name       string `json:"name,omitempty"`
}

type HotPC struct {
	CompilerSite  *jitprofile.CodeSite    `json:"compiler_site,omitempty"`
	SourceName    string                  `json:"wasm_function_name,omitempty"`
	InlineCallers []InlineCaller          `json:"inline_callers,omitempty"`
	Source        *jitprofile.SourceRange `json:"wasm_source,omitempty"`
	Offset        uint64                  `json:"offset"`
	Samples       uint64                  `json:"samples"`
	Weight        uint64                  `json:"weight"`
}
type Row struct {
	CPUPerWork   *float64             `json:"cpu_ns_per_work,omitempty"`
	ModuleID     string               `json:"module_id"`
	ArtifactID   string               `json:"artifact_id"`
	Function     int                  `json:"function"`
	Kind         string               `json:"kind"`
	Name         string               `json:"name"`
	RegionOffset uint64               `json:"region_offset,omitempty"`
	Samples      uint64               `json:"samples"`
	Weight       uint64               `json:"weight"`
	Static       *jitprofile.Function `json:"compiler,omitempty"`
	PCs          []HotPC              `json:"pcs,omitempty"`
}
type Report struct {
	Unit           string `json:"weight_unit"`
	Samples        uint64 `json:"samples"`
	Weight         uint64 `json:"weight"`
	UnknownSamples uint64 `json:"unknown_samples"`
	UnknownWeight  uint64 `json:"unknown_weight"`
	Rows           []Row  `json:"rows"`
}

func decimalNS(s string) (uint64, error) {
	s = strings.TrimSuffix(s, ":")
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid sample timestamp %q", s)
	}
	sec, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 9 {
		return 0, fmt.Errorf("timestamp has sub-nanosecond precision")
	}
	frac += strings.Repeat("0", 9-len(frac))
	ns, err := strconv.ParseUint(frac, 10, 64)
	if err != nil {
		return 0, err
	}
	if sec > (^uint64(0)-ns)/1e9 {
		return 0, fmt.Errorf("timestamp overflow")
	}
	return sec*1e9 + ns, nil
}

// ParsePerfScript consumes `perf script --ns -F time,ip,period`, whose fixed
// output order is time, period, IP (independent of the -F option order). No symbol names
// are trusted: resolution uses timestamp, address, and the captured image journal.
func ParsePerfScript(r io.Reader, maxSamples int) ([]Sample, error) {
	if maxSamples <= 0 {
		maxSamples = DefaultLimits().Samples
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var out []Sample
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected perf sample line %q", line)
		}
		ts, err := decimalNS(fields[0])
		if err != nil {
			return nil, err
		}
		pc, err := strconv.ParseUint(strings.TrimPrefix(fields[2], "0x"), 16, 64)
		if err != nil {
			return nil, err
		}
		period, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		if len(out) >= maxSamples {
			return nil, fmt.Errorf("sample limit %d exceeded; no partial report emitted", maxSamples)
		}
		out = append(out, Sample{Timestamp: ts, PC: pc, Period: period})
	}
	return out, scanner.Err()
}

// Resolve performs a time-ordered join. Retired mappings cannot label later
// samples, even when the virtual address is reused. No call chains are inferred.
func Resolve(events []jitprofile.Event, samples []Sample, unit string) (Report, error) {
	return ResolveWithLimits(events, samples, unit, DefaultLimits())
}
func ResolveWithLimits(events []jitprofile.Event, samples []Sample, unit string, limits Limits) (Report, error) {
	report := Report{Unit: unit}
	if err := ValidateEvents(events, limits); err != nil {
		return report, err
	}
	if len(samples) > limits.Samples {
		return report, fmt.Errorf("profile sample limit exceeded")
	}
	if len(samples) == 0 {
		return report, nil
	}
	events = append([]jitprofile.Event(nil), events...)
	samples = append([]Sample(nil), samples...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp == events[j].Timestamp {
			return events[i].Sequence < events[j].Sequence
		}
		return events[i].Timestamp < events[j].Timestamp
	})
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Timestamp < samples[j].Timestamp })
	lastSampleTime := samples[len(samples)-1].Timestamp
	events = events[:sort.Search(len(events), func(i int) bool { return events[i].Timestamp > lastSampleTime })]
	active := newLiveImages(events)
	symbols := make(map[uint64]map[int]*jitprofile.Function)
	var changedLoads []int
	rows := make(map[string]int)
	hot := make(map[string]map[uint64]*HotPC)
	event := 0
	hotCount := 0
	inlineCount := 0
	for _, sample := range samples {
		changedLoads = changedLoads[:0]
		for event < len(events) && events[event].Timestamp <= sample.Timestamp {
			e := events[event]
			event++
			switch e.Kind {
			case "load":
				if e.Image == nil {
					return report, fmt.Errorf("load without image")
				}
				if e.ImageID == 0 || e.Image.ID != e.ImageID {
					return report, fmt.Errorf("load has inconsistent mapping identity")
				}
				if err := jitprofile.ValidateRegions(e.Image.Regions, e.Image.Size); err != nil {
					return report, err
				}
				if err := jitprofile.ValidateUnwind(e.Image.Unwind, e.Image.Size); err != nil {
					return report, err
				}
				if err := jitprofile.ValidateSources(e.Image.Sources, e.Image.Size); err != nil {
					return report, err
				}
				if err := jitprofile.ValidateCodeSites(e.Image.CodeSites, e.Image.Size); err != nil {
					return report, err
				}
				if err := jitprofile.ValidateCodeSiteRegions(e.Image.CodeSites, e.Image.Regions); err != nil {
					return report, err
				}
				if err := jitprofile.ValidateInlineSources(e.Image.Sources, e.Image.InlineFrames); err != nil {
					return report, err
				}
				changedLoads = append(changedLoads, active.load(event-1, e.Image))
				table := make(map[int]*jitprofile.Function, len(e.Image.Functions))
				for i := range e.Image.Functions {
					f := &e.Image.Functions[i]
					table[f.Index] = f
				}
				symbols[e.ImageID] = table
			case "retire":
				active.retire(e.ImageID)
				delete(symbols, e.ImageID)
			default:
				return report, fmt.Errorf("unknown lifecycle event %q", e.Kind)
			}
		}
		for _, position := range changedLoads {
			if err := active.checkLoad(position); err != nil {
				return report, err
			}
		}
		if sample.Period > math.MaxUint64-report.Weight {
			return report, fmt.Errorf("sample weights overflow uint64")
		}
		report.Samples++
		report.Weight += sample.Period
		im := active.lookup(sample.PC)
		if im == nil || sample.PC-im.Base >= im.Size {
			report.UnknownSamples++
			report.UnknownWeight += sample.Period
			continue
		}
		off := sample.PC - im.Base
		ri := sort.Search(len(im.Regions), func(i int) bool { return im.Regions[i].Offset+im.Regions[i].Size > off })
		region := im.Regions[ri]
		if !executable(region) || region.Kind == "unknown" {
			report.UnknownSamples++
			report.UnknownWeight += sample.Period
			continue
		}
		kind := region.Kind
		regionKey := uint64(0)
		if region.Function >= 0 {
			kind = "function"
		} else {
			regionKey = region.Offset
		}
		key := fmt.Sprintf("%s/%s/%d/%s/%d", im.ModuleID, im.ArtifactID, region.Function, kind, regionKey)
		index, ok := rows[key]
		if !ok {
			if len(rows) >= limits.Rows {
				return Report{}, fmt.Errorf("profile aggregation row limit exceeded")
			}
			row := Row{ModuleID: im.ModuleID, ArtifactID: im.ArtifactID, Function: region.Function, Kind: kind, Name: region.Name, RegionOffset: regionKey}
			if symbol := symbols[im.ID][region.Function]; symbol != nil {
				copy := *symbol
				row.Static = &copy
			}
			index = len(report.Rows)
			rows[key] = index
			report.Rows = append(report.Rows, row)
			hot[key] = make(map[uint64]*HotPC)
		}
		row := &report.Rows[index]
		row.Samples++
		row.Weight += sample.Period
		pc := hot[key][off]
		if pc == nil {
			hotCount++
			if hotCount > limits.HotPCs {
				return Report{}, fmt.Errorf("profile hot-PC limit exceeded")
			}
			pc = &HotPC{Offset: off}
			if site, ok := jitprofile.LookupCodeSite(im.CodeSites, off); ok {
				pc.CompilerSite = &site
			}
			if source, ok := jitprofile.LookupSource(im.Sources, off); ok {
				pc.Source = &source
				if symbol := symbols[im.ID][int(source.Function)]; symbol != nil {
					pc.SourceName = symbol.Name
				}
				for _, frame := range jitprofile.InlineCallers(im.InlineFrames, source.InlineParent) {
					inlineCount++
					if inlineCount > limits.Metadata {
						return Report{}, fmt.Errorf("profile expanded inline metadata limit exceeded")
					}
					caller := InlineCaller{Function: frame.Function, WasmOffset: frame.WasmOffset}
					if symbol := symbols[im.ID][int(frame.Function)]; symbol != nil {
						caller.Name = symbol.Name
					}
					pc.InlineCallers = append(pc.InlineCallers, caller)
				}
			}
			hot[key][off] = pc
		}
		pc.Samples++
		pc.Weight += sample.Period
	}
	for key, index := range rows {
		for _, pc := range hot[key] {
			report.Rows[index].PCs = append(report.Rows[index].PCs, *pc)
		}
		sort.Slice(report.Rows[index].PCs, func(i, j int) bool { return report.Rows[index].PCs[i].Offset < report.Rows[index].PCs[j].Offset })
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Weight != report.Rows[j].Weight {
			return report.Rows[i].Weight > report.Rows[j].Weight
		}
		return report.Rows[i].Function < report.Rows[j].Function
	})
	return report, nil
}
