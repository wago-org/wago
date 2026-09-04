// Package profile defines the compiler-backend-neutral profile format.
package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

const Version = 1

// Source identifies where a profile observation came from.
type Source string

const (
	SourceStatic       Source = "static"
	SourceRailshot     Source = "railshot"
	SourceDragline     Source = "dragline"
	SourceInstrumented Source = "instrumented"
	SourceHardware     Source = "hardware"
)

// Phase distinguishes startup, steady-state, and rare or shutdown behavior.
type Phase string

const (
	PhaseStartup Phase = "startup"
	PhaseSteady  Phase = "steady"
	PhaseRare    Phase = "rare"
)

// Site identifies an observation in original Wasm, not compiler IR.
type Site struct {
	Function uint32 `json:"function"`
	Offset   uint32 `json:"offset"`
}

type EdgeCount struct {
	Site   Site   `json:"site"`
	Target uint32 `json:"target"`
	Count  uint64 `json:"count"`
}

type TargetCount struct {
	Function uint32 `json:"function"`
	Count    uint64 `json:"count"`
}

type TargetHistogram struct {
	Site    Site          `json:"site"`
	Targets []TargetCount `json:"targets"`
}

type ValueBucket struct {
	Low   int64  `json:"low"`
	High  int64  `json:"high"`
	Count uint64 `json:"count"`
}

type ValueHistogram struct {
	Site    Site          `json:"site"`
	Buckets []ValueBucket `json:"buckets"`
}

type SiteCount struct {
	Site  Site   `json:"site"`
	Count uint64 `json:"count"`
}

// Module is the versioned profile exchanged by Railshot, Dragline, offline
// tooling, and future tiering. All locations refer to original Wasm offsets.
type Module struct {
	Version uint32 `json:"version"`

	ModuleHash [32]byte `json:"module_hash"`
	Generation uint64   `json:"generation"`
	Source     Source   `json:"source"`
	Phase      Phase    `json:"phase"`

	FunctionCounts []uint64          `json:"function_counts,omitempty"`
	EdgeCounts     []EdgeCount       `json:"edge_counts,omitempty"`
	BackedgeCounts []EdgeCount       `json:"backedge_counts,omitempty"`
	CallTargets    []TargetHistogram `json:"call_targets,omitempty"`
	ValueRanges    []ValueHistogram  `json:"value_ranges,omitempty"`
	MemOpSizes     []ValueHistogram  `json:"mem_op_sizes,omitempty"`
	Allocations    []SiteCount       `json:"allocations,omitempty"`
}

// Clone returns a deep copy suitable for an immutable compiler configuration.
func Clone(p Module) Module {
	p.FunctionCounts = append([]uint64(nil), p.FunctionCounts...)
	p.EdgeCounts = append([]EdgeCount(nil), p.EdgeCounts...)
	p.BackedgeCounts = append([]EdgeCount(nil), p.BackedgeCounts...)
	p.CallTargets = append([]TargetHistogram(nil), p.CallTargets...)
	for i := range p.CallTargets {
		p.CallTargets[i].Targets = append([]TargetCount(nil), p.CallTargets[i].Targets...)
	}
	p.ValueRanges = append([]ValueHistogram(nil), p.ValueRanges...)
	for i := range p.ValueRanges {
		p.ValueRanges[i].Buckets = append([]ValueBucket(nil), p.ValueRanges[i].Buckets...)
	}
	p.MemOpSizes = append([]ValueHistogram(nil), p.MemOpSizes...)
	for i := range p.MemOpSizes {
		p.MemOpSizes[i].Buckets = append([]ValueBucket(nil), p.MemOpSizes[i].Buckets...)
	}
	p.Allocations = append([]SiteCount(nil), p.Allocations...)
	return p
}

// Marshal encodes p canonically. Slice order is normalized so equivalent
// profiles produce identical bytes and hashes.
func Marshal(p Module) ([]byte, error) {
	if p.Version == 0 {
		p.Version = Version
	}
	p.normalize()
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// Unmarshal decodes one strict profile document. Unknown fields and trailing
// JSON are rejected to keep cache and replay identities auditable.
func Unmarshal(data []byte) (Module, error) {
	var p Module
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Module{}, fmt.Errorf("profile: decode: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		if err == nil {
			return Module{}, fmt.Errorf("profile: trailing JSON value")
		}
		return Module{}, fmt.Errorf("profile: trailing data: %w", err)
	}
	p.normalize()
	if err := p.Validate(); err != nil {
		return Module{}, err
	}
	return p, nil
}

// Hash returns the digest used by compiler cache identities.
func Hash(p Module) ([32]byte, error) {
	encoded, err := Marshal(p)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func (p Module) Validate() error {
	if p.Version != Version {
		return fmt.Errorf("profile: version %d unsupported (want %d)", p.Version, Version)
	}
	switch p.Source {
	case SourceStatic, SourceRailshot, SourceDragline, SourceInstrumented, SourceHardware:
	default:
		return fmt.Errorf("profile: unknown source %q", p.Source)
	}
	switch p.Phase {
	case PhaseStartup, PhaseSteady, PhaseRare:
	default:
		return fmt.Errorf("profile: unknown phase %q", p.Phase)
	}
	if err := validateEdges("edge", p.EdgeCounts); err != nil {
		return err
	}
	if err := validateEdges("backedge", p.BackedgeCounts); err != nil {
		return err
	}
	if err := validateSites("call target", len(p.CallTargets), func(i int) Site { return p.CallTargets[i].Site }); err != nil {
		return err
	}
	for _, histogram := range p.CallTargets {
		for i := 1; i < len(histogram.Targets); i++ {
			if histogram.Targets[i-1].Function >= histogram.Targets[i].Function {
				return fmt.Errorf("profile: duplicate or unordered call target %d at function %d offset %d", histogram.Targets[i].Function, histogram.Site.Function, histogram.Site.Offset)
			}
		}
	}
	if err := validateSites("value range", len(p.ValueRanges), func(i int) Site { return p.ValueRanges[i].Site }); err != nil {
		return err
	}
	for _, histogram := range p.ValueRanges {
		if err := validateBuckets("value range", histogram.Site, histogram.Buckets); err != nil {
			return err
		}
	}
	if err := validateSites("memory size", len(p.MemOpSizes), func(i int) Site { return p.MemOpSizes[i].Site }); err != nil {
		return err
	}
	for _, histogram := range p.MemOpSizes {
		if err := validateBuckets("memory size", histogram.Site, histogram.Buckets); err != nil {
			return err
		}
	}
	return validateSites("allocation", len(p.Allocations), func(i int) Site { return p.Allocations[i].Site })
}

func validateBuckets(name string, site Site, buckets []ValueBucket) error {
	for i, bucket := range buckets {
		if bucket.Low > bucket.High {
			return fmt.Errorf("profile: invalid %s bucket [%d,%d] at function %d offset %d", name, bucket.Low, bucket.High, site.Function, site.Offset)
		}
		if i > 0 && buckets[i-1].High >= bucket.Low {
			return fmt.Errorf("profile: overlapping %s buckets at function %d offset %d", name, site.Function, site.Offset)
		}
	}
	return nil
}

func validateEdges(name string, values []EdgeCount) error {
	for i := 1; i < len(values); i++ {
		before, after := values[i-1], values[i]
		if !siteLess(before.Site, after.Site) && (before.Site != after.Site || before.Target >= after.Target) {
			return fmt.Errorf("profile: duplicate or unordered %s at function %d offset %d", name, after.Site.Function, after.Site.Offset)
		}
	}
	return nil
}

func validateSites(name string, count int, at func(int) Site) error {
	for i := 1; i < count; i++ {
		if !siteLess(at(i-1), at(i)) {
			site := at(i)
			return fmt.Errorf("profile: duplicate or unordered %s at function %d offset %d", name, site.Function, site.Offset)
		}
	}
	return nil
}

func (p *Module) normalize() {
	sort.Slice(p.EdgeCounts, func(i, j int) bool { return edgeLess(p.EdgeCounts[i], p.EdgeCounts[j]) })
	sort.Slice(p.BackedgeCounts, func(i, j int) bool { return edgeLess(p.BackedgeCounts[i], p.BackedgeCounts[j]) })
	sort.Slice(p.CallTargets, func(i, j int) bool { return siteLess(p.CallTargets[i].Site, p.CallTargets[j].Site) })
	for i := range p.CallTargets {
		sort.Slice(p.CallTargets[i].Targets, func(a, b int) bool {
			return p.CallTargets[i].Targets[a].Function < p.CallTargets[i].Targets[b].Function
		})
	}
	sort.Slice(p.ValueRanges, func(i, j int) bool { return siteLess(p.ValueRanges[i].Site, p.ValueRanges[j].Site) })
	for i := range p.ValueRanges {
		sort.Slice(p.ValueRanges[i].Buckets, func(a, b int) bool {
			left, right := p.ValueRanges[i].Buckets[a], p.ValueRanges[i].Buckets[b]
			return left.Low < right.Low || left.Low == right.Low && left.High < right.High
		})
	}
	sort.Slice(p.MemOpSizes, func(i, j int) bool { return siteLess(p.MemOpSizes[i].Site, p.MemOpSizes[j].Site) })
	for i := range p.MemOpSizes {
		sort.Slice(p.MemOpSizes[i].Buckets, func(a, b int) bool {
			left, right := p.MemOpSizes[i].Buckets[a], p.MemOpSizes[i].Buckets[b]
			return left.Low < right.Low || left.Low == right.Low && left.High < right.High
		})
	}
	sort.Slice(p.Allocations, func(i, j int) bool { return siteLess(p.Allocations[i].Site, p.Allocations[j].Site) })
}

func edgeLess(a, b EdgeCount) bool {
	return siteLess(a.Site, b.Site) || a.Site == b.Site && a.Target < b.Target
}

func siteLess(a, b Site) bool {
	return a.Function < b.Function || a.Function == b.Function && a.Offset < b.Offset
}
