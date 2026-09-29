package profile

import (
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"strings"
)

// WritePprof exports sampled PCs with optional compiler-recorded inline ancestry.
// All inline functions share one Location; no native callers are fabricated.
// Wasm offsets are sample labels, never source-file line numbers. See
// https://github.com/google/pprof/blob/main/proto/profile.proto.
func WritePprof(w io.Writer, r Report, durationNS int64) error {
	if r.Unit != "nanoseconds" {
		return fmt.Errorf("CPU pprof requires nanosecond event weights")
	}
	if durationNS < 0 {
		return fmt.Errorf("negative profile duration")
	}
	if r.UnknownSamples > math.MaxInt64 || r.UnknownWeight > math.MaxInt64 {
		return fmt.Errorf("unknown samples exceed pprof signed value range")
	}
	if r.UnknownSamples == 0 && r.UnknownWeight != 0 {
		return fmt.Errorf("unknown weight without samples")
	}
	totalSamples, totalWeight := r.UnknownSamples, r.UnknownWeight
	for _, row := range r.Rows {
		var rowSamples, rowWeight uint64
		for _, pc := range row.PCs {
			if pc.Samples > math.MaxInt64 || pc.Weight > math.MaxInt64 {
				return fmt.Errorf("sample exceeds pprof signed value range")
			}
			if pc.Samples > math.MaxInt64-totalSamples || pc.Weight > math.MaxInt64-totalWeight {
				return fmt.Errorf("aggregate exceeds pprof signed value range")
			}
			totalSamples += pc.Samples
			totalWeight += pc.Weight
			rowSamples += pc.Samples
			rowWeight += pc.Weight
			if pc.Source == nil && len(pc.InlineCallers) != 0 {
				return fmt.Errorf("inline callers without a source location")
			}
			if site := pc.CompilerSite; site != nil && (site.Kind == "" || pc.Offset < site.Offset || pc.Offset-site.Offset >= site.Size) {
				return fmt.Errorf("sample outside its compiler site")
			}
		}
		if rowSamples != row.Samples || rowWeight != row.Weight {
			return fmt.Errorf("row totals disagree with native PC samples")
		}
	}
	if totalSamples != r.Samples || totalWeight != r.Weight {
		return fmt.Errorf("report totals disagree with samples")
	}
	stringTable := []string{"", "samples", "count", "cpu", "nanoseconds"}
	indexes := make(map[string]uint64, len(stringTable))
	for i, s := range stringTable {
		indexes[s] = uint64(i)
	}
	intern := func(s string) uint64 {
		if i, ok := indexes[s]; ok {
			return i
		}
		i := uint64(len(stringTable))
		stringTable = append(stringTable, s)
		indexes[s] = i
		return i
	}
	var p []byte
	p = message(p, 1, number(number(nil, 1, 1), 2, 2))
	p = message(p, 1, number(number(nil, 1, 3), 2, 4))
	type functionKey struct {
		module, artifact string
		index            int
		kind             string
		region           uint64
	}
	functions := map[functionKey]uint64{}
	functionID := func(key functionKey, name string) uint64 {
		if id, ok := functions[key]; ok {
			return id
		}
		id := uint64(len(functions) + 1)
		functions[key] = id
		qualified := fmt.Sprintf("%s/%s:f%d:%s", key.module, key.artifact, key.index, name)
		if key.index < 0 {
			qualified = fmt.Sprintf("%s/%s:%s+0x%x:%s", key.module, key.artifact, key.kind, key.region, name)
		}
		if key.kind == "unmapped" && key.module == "" && key.artifact == "" {
			qualified = name
		}
		text := intern(qualified)
		p = message(p, 5, number(number(number(nil, 1, id), 2, text), 3, text))
		return id
	}
	stringLabel := func(key, value string) []byte { return number(number(nil, 1, intern(key)), 2, intern(value)) }
	numericLabel := func(key string, value uint64, unit string) []byte {
		return number(number(number(nil, 1, intern(key)), 3, value), 4, intern(unit))
	}
	var location uint64
	for _, row := range r.Rows {
		key := functionKey{module: row.ModuleID, artifact: row.ArtifactID, index: row.Function}
		if row.Function < 0 {
			key.kind, key.region = row.Kind, row.RegionOffset
		}
		physical := functionID(key, row.Name)
		for _, pc := range row.PCs {
			location++
			loc := number(number(nil, 1, location), 3, pc.Offset)
			sample := number(number(number(nil, 1, location), 2, pc.Samples), 2, pc.Weight)
			if pc.CompilerSite != nil {
				sample = message(sample, 3, stringLabel("compiler_site_kind", pc.CompilerSite.Kind))
			}
			if pc.Source == nil {
				loc = message(loc, 4, number(nil, 1, physical))
			} else {
				logicalKey := functionKey{module: row.ModuleID, artifact: row.ArtifactID, index: int(pc.Source.Function)}
				name := pc.SourceName
				if name == "" {
					name = fmt.Sprintf("wasmfunc%d", pc.Source.Function)
				}
				loc = message(loc, 4, number(nil, 1, functionID(logicalKey, name)))
				sample = message(sample, 3, numericLabel("wasm_function", uint64(pc.Source.Function), "index"))
				sample = message(sample, 3, numericLabel("wasm_offset", uint64(pc.Source.WasmOffset), "bytes"))
				sample = message(sample, 3, stringLabel("native_owner", fmt.Sprintf("f%d:%s", row.Function, row.Name)))
				var sites []string
				for _, caller := range pc.InlineCallers {
					callerKey := functionKey{module: row.ModuleID, artifact: row.ArtifactID, index: int(caller.Function)}
					name := caller.Name
					if name == "" {
						name = fmt.Sprintf("wasmfunc%d", caller.Function)
					}
					loc = message(loc, 4, number(nil, 1, functionID(callerKey, name)))
					sites = append(sites, fmt.Sprintf("f%d+0x%x", caller.Function, caller.WasmOffset))
				}
				if len(sites) > 0 {
					sample = message(sample, 3, stringLabel("inline_call_sites", strings.Join(sites, " <- ")))
				}
			}
			p = message(p, 4, loc)
			p = message(p, 2, sample)
		}
	}
	if r.UnknownSamples > 0 {
		// Use a separate identity rather than a guest-like function index.
		function := functionID(functionKey{kind: "unmapped", index: -1}, "[unmapped or host]")
		location++
		p = message(p, 4, message(number(nil, 1, location), 4, number(nil, 1, function)))
		p = message(p, 2, number(number(number(nil, 1, location), 2, r.UnknownSamples), 2, r.UnknownWeight))
	}
	comment := intern("Native PCs with optional static inline ancestry; dynamic native callers unavailable. Wasm offsets are byte labels, not source lines. Addresses are image-relative offsets.")
	for _, s := range stringTable {
		p = message(p, 6, []byte(s))
	}
	if durationNS > 0 {
		p = number(p, 10, uint64(durationNS))
	}
	p = number(p, 13, comment)
	gz := gzip.NewWriter(w)
	if err := writeAll(gz, p); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}
func varint(b []byte, n uint64) []byte {
	for n >= 128 {
		b = append(b, byte(n)|128)
		n >>= 7
	}
	return append(b, byte(n))
}
func number(b []byte, field int, n uint64) []byte {
	b = varint(b, uint64(field<<3))
	return varint(b, n)
}
func message(b []byte, field int, v []byte) []byte {
	b = varint(b, uint64(field<<3|2))
	b = varint(b, uint64(len(v)))
	return append(b, v...)
}
