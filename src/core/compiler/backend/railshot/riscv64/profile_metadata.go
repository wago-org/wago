//go:build linux && !tinygo && wago_profile

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Compiler-owned metadata uses bounded directories and measured final spans.
// Finalization validates the complete layout before any executable staging.
const (
	maxProfileFunctionRanges = shared.MaxScalarSourceRanges
	maxProfileModuleRanges   = 262144
	maxProfileCodeBytes      = 1<<31 - 1
	maxProfileFunctionIndex  = uint32(1<<32 - 1)
)

// profileSourceRecorder records target.Position coordinates. Unknown origins
// leave gaps; Finish closes the last scope. A failed recorder exposes no partial
// map. The owner may reuse its bounded backing between functions with reset.
type profileSourceRecorder struct {
	ranges []shared.NativeSourceRange
	origin shared.EmissionOrigin
	cursor int
	err    error
}

func (r *profileSourceRecorder) reset() {
	*r = profileSourceRecorder{ranges: r.ranges[:0]}
}

func (r *profileSourceRecorder) fail(reason string) error {
	r.err = fmt.Errorf("rv64: profile %s", reason)
	r.ranges = r.ranges[:0]
	return r.err
}

func (r *profileSourceRecorder) switchOrigin(at int, origin shared.EmissionOrigin) error {
	if r == nil {
		return nil
	}
	if r.err != nil {
		return r.err
	}
	if at < r.cursor || at > maxProfileCodeBytes || at&3 != 0 {
		return r.fail("source position is not monotonic bounded RV64 code")
	}
	if origin.Valid && (origin.Function >= maxProfileFunctionIndex || origin.InlineParent != 0) {
		return r.fail("source function/inline identity")
	}
	if at > r.cursor && r.origin.Valid {
		next := shared.NativeSourceRange{Offset: uint64(r.cursor), Size: uint64(at - r.cursor), Function: r.origin.Function, WasmOffset: r.origin.PC}
		coalesces := false
		if len(r.ranges) != 0 {
			last := &r.ranges[len(r.ranges)-1]
			coalesces = last.Offset+last.Size == next.Offset && last.Function == next.Function && last.WasmOffset == next.WasmOffset
			if coalesces {
				last.Size += next.Size
			}
		}
		if !coalesces {
			if len(r.ranges) == maxProfileFunctionRanges {
				return r.fail("function source range bound")
			}
			if len(r.ranges) == cap(r.ranges) {
				grown := make([]shared.NativeSourceRange, len(r.ranges), min(maxProfileFunctionRanges, max(16, 2*cap(r.ranges))))
				copy(grown, r.ranges)
				r.ranges = grown
			}
			r.ranges = append(r.ranges, next)
		}
	}
	r.cursor, r.origin = at, origin
	return nil
}

func (r *profileSourceRecorder) finish(at int) ([]shared.NativeSourceRange, error) {
	if r == nil {
		return nil, nil
	}
	if err := r.switchOrigin(at, shared.EmissionOrigin{}); err != nil {
		return nil, err
	}
	return r.ranges, nil
}

type profileBodyKind uint8

const (
	profileGuestBody profileBodyKind = iota + 1
	profileColdTrap
	profileRuntimeHelper
)

// profileBodyRegion is relative to the first byte of the body copied into a
// frame. Its directory must partition that body exactly. Cold traps have their
// own original opcode, while helper bytes deliberately have no source map.
type profileBodyRegion struct {
	Offset, Size int
	Kind         profileBodyKind
}

// profileFunctionLayout records measured final spans, never estimates. Prefix
// includes all entry checks/setup; Epilogue follows the complete body (including
// its cold islands). Remaining bytes are entry failures and optional direct-entry adapters. Sources use
// their original target coordinates, with BodySourceStart naming the first body
// byte in those coordinates. This also accepts shared scalar recorder output.
type profileFunctionLayout struct {
	Function                              uint32
	Entry, CodeBytes                      int
	PrefixBytes, BodyBytes, EpilogueBytes int
	BodySourceStart                       int
	LocalDeclBytes, WasmBodyBytes         uint32
	BodyRegions                           []profileBodyRegion
	Sources                               []shared.NativeSourceRange
}

// profileMetadataBudget must also be charged as functions are produced, before
// retaining/copying their directories. Finalization repeats the count check,
// but cannot retroactively bound caller allocations made before it was called.
type profileMetadataBudget struct{ regions, sources int }

func (b *profileMetadataBudget) charge(regionCount, sourceCount int) error {
	if regionCount < 0 || sourceCount < 0 || regionCount > maxProfileModuleRanges-b.regions || sourceCount > maxProfileModuleRanges-b.sources {
		return fmt.Errorf("rv64: module profile metadata bound")
	}
	b.regions += regionCount
	b.sources += sourceCount
	return nil
}

func profileAlignedSize(n int) bool { return n >= 0 && n <= maxProfileCodeBytes && n&3 == 0 }

func (f *profileFunctionLayout) validate() error {
	if f.Function >= maxProfileFunctionIndex || !profileAlignedSize(f.Entry) || f.Entry&15 != 0 || !profileAlignedSize(f.CodeBytes) || f.CodeBytes == 0 ||
		!profileAlignedSize(f.PrefixBytes) || f.PrefixBytes == 0 || !profileAlignedSize(f.BodyBytes) || !profileAlignedSize(f.EpilogueBytes) || f.EpilogueBytes == 0 ||
		!profileAlignedSize(f.BodySourceStart) || f.BodyBytes > maxProfileCodeBytes-f.BodySourceStart ||
		f.PrefixBytes > f.CodeBytes || f.BodyBytes > f.CodeBytes-f.PrefixBytes || f.EpilogueBytes > f.CodeBytes-f.PrefixBytes-f.BodyBytes ||
		len(f.BodyRegions) > maxProfileFunctionRanges || len(f.Sources) > maxProfileFunctionRanges || uint64(f.LocalDeclBytes)+uint64(f.WasmBodyBytes) > uint64(^uint32(0)) {
		return fmt.Errorf("rv64: invalid finalized profile function layout")
	}
	end := 0
	for _, region := range f.BodyRegions {
		if region.Offset != end || !profileAlignedSize(region.Size) || region.Size == 0 || region.Size > f.BodyBytes-end || region.Kind < profileGuestBody || region.Kind > profileRuntimeHelper {
			return fmt.Errorf("rv64: incomplete/invalid profile body partition")
		}
		end += region.Size
	}
	if end != f.BodyBytes {
		return fmt.Errorf("rv64: profile body partition leaves unknown layout")
	}
	var sourceEnd uint64
	regionIndex := 0
	for _, source := range f.Sources {
		base, limit := uint64(f.BodySourceStart), uint64(f.BodySourceStart+f.BodyBytes)
		if source.Offset < sourceEnd || source.Offset < base || source.Offset > limit || source.Size == 0 || source.Size > limit-source.Offset || source.Offset&3 != 0 || source.Size&3 != 0 || source.Function != f.Function || source.InlineParent != 0 || source.WasmOffset < f.LocalDeclBytes || uint64(source.WasmOffset) >= uint64(f.LocalDeclBytes)+uint64(f.WasmBodyBytes) {
			return fmt.Errorf("rv64: invalid finalized source range")
		}
		sourceEnd = source.Offset + source.Size
		start, stop := int(source.Offset-base), int(sourceEnd-base)
		for regionIndex < len(f.BodyRegions) && f.BodyRegions[regionIndex].Offset+f.BodyRegions[regionIndex].Size <= start {
			regionIndex++
		}
		for at := regionIndex; at < len(f.BodyRegions) && f.BodyRegions[at].Offset < stop; at++ {
			if f.BodyRegions[at].Kind == profileRuntimeHelper {
				return fmt.Errorf("rv64: source map attributes synthetic helper bytes")
			}
		}
	}
	return nil
}

// finalizeProfileMetadata validates before allocating either result. Results
// own their backing and never alias compiler scratch; every error returns nil
// directories. It covers exactly finalized bytes, including alignment, while
// source gaps in prefixes, epilogues, helpers and entry failures remain gaps.
func finalizeProfileMetadata(functions []profileFunctionLayout, codeBytes int) ([]jitprofile.Region, []shared.NativeSourceRange, error) {
	if len(functions) > maxProfileModuleRanges/2 || !profileAlignedSize(codeBytes) {
		return nil, nil, fmt.Errorf("rv64: finalized profile image bound")
	}
	var budget profileMetadataBudget
	end := 0
	for i := range functions {
		f := &functions[i]
		if err := f.validate(); err != nil {
			return nil, nil, err
		}
		if f.Entry != (end+15)&^15 || f.Entry > codeBytes || f.CodeBytes > codeBytes-f.Entry || i > 0 && f.Function != functions[i-1].Function+1 {
			return nil, nil, fmt.Errorf("rv64: finalized profile function order/extent")
		}
		regions := len(f.BodyRegions) + 2 // nonempty prefix and epilogue
		if f.Entry != end {
			regions++
		}
		if f.PrefixBytes+f.BodyBytes+f.EpilogueBytes != f.CodeBytes {
			regions++
		}
		if err := budget.charge(regions, len(f.Sources)); err != nil {
			return nil, nil, err
		}
		end = f.Entry + f.CodeBytes
	}
	if end != codeBytes {
		return nil, nil, fmt.Errorf("rv64: finalized profile image has unowned tail")
	}
	var regions []jitprofile.Region
	var sources []shared.NativeSourceRange
	if budget.regions != 0 {
		regions = make([]jitprofile.Region, 0, budget.regions)
	}
	if budget.sources != 0 {
		sources = make([]shared.NativeSourceRange, 0, budget.sources)
	}
	add := func(offset, size int, kind string, function int) {
		if size != 0 {
			regions = append(regions, jitprofile.Region{Offset: uint64(offset), Size: uint64(size), Kind: kind, Function: function})
		}
	}
	end = 0
	for _, f := range functions {
		add(end, f.Entry-end, "padding", -1)
		add(f.Entry, f.PrefixBytes, "runtime-helper", int(f.Function))
		bodyStart := f.Entry + f.PrefixBytes
		for _, region := range f.BodyRegions {
			kind := "guest-body"
			if region.Kind == profileColdTrap {
				kind = "cold-trap"
			}
			if region.Kind == profileRuntimeHelper {
				kind = "runtime-helper"
			}
			add(bodyStart+region.Offset, region.Size, kind, int(f.Function))
		}
		add(bodyStart+f.BodyBytes, f.EpilogueBytes, "runtime-helper", int(f.Function))
		entryFailure := bodyStart + f.BodyBytes + f.EpilogueBytes
		add(entryFailure, f.Entry+f.CodeBytes-entryFailure, "runtime-helper", int(f.Function))
		for _, source := range f.Sources {
			source.Offset = uint64(bodyStart) + source.Offset - uint64(f.BodySourceStart)
			sources = append(sources, source)
		}
		end = f.Entry + f.CodeBytes
	}
	return regions, sources, nil
}
