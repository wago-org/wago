//go:build linux && !tinygo && wago_profile

package riscv64

import (
	"reflect"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func profileMetadataFixture() []profileFunctionLayout {
	return []profileFunctionLayout{
		{
			Function: 2, Entry: 0, CodeBytes: 44, PrefixBytes: 8, BodyBytes: 24, EpilogueBytes: 8,
			BodySourceStart: 100, LocalDeclBytes: 3, WasmBodyBytes: 20,
			BodyRegions: []profileBodyRegion{{0, 8, profileGuestBody}, {8, 8, profileRuntimeHelper}, {16, 8, profileColdTrap}},
			Sources: []shared.NativeSourceRange{
				{Offset: 100, Size: 8, Function: 2, WasmOffset: 5},
				{Offset: 116, Size: 8, Function: 2, WasmOffset: 10},
			},
		},
		{
			Function: 3, Entry: 48, CodeBytes: 24, PrefixBytes: 4, BodyBytes: 8, EpilogueBytes: 4,
			LocalDeclBytes: 1, WasmBodyBytes: 10,
			BodyRegions: []profileBodyRegion{{0, 8, profileGuestBody}},
			Sources:     []shared.NativeSourceRange{{Offset: 0, Size: 8, Function: 3, WasmOffset: 1}},
		},
	}
}

func TestProfileMetadataFinalCoordinatesAndGaps(t *testing.T) {
	functions := profileMetadataFixture()
	regions, sources, err := finalizeProfileMetadata(functions, 72)
	if err != nil {
		t.Fatal(err)
	}
	wantRegions := []jitprofile.Region{
		{Offset: 0, Size: 8, Kind: "runtime-helper", Function: 2},
		{Offset: 8, Size: 8, Kind: "guest-body", Function: 2},
		{Offset: 16, Size: 8, Kind: "runtime-helper", Function: 2},
		{Offset: 24, Size: 8, Kind: "cold-trap", Function: 2},
		{Offset: 32, Size: 8, Kind: "runtime-helper", Function: 2},
		{Offset: 40, Size: 4, Kind: "runtime-helper", Function: 2},
		{Offset: 44, Size: 4, Kind: "padding", Function: -1},
		{Offset: 48, Size: 4, Kind: "runtime-helper", Function: 3},
		{Offset: 52, Size: 8, Kind: "guest-body", Function: 3},
		{Offset: 60, Size: 4, Kind: "runtime-helper", Function: 3},
		{Offset: 64, Size: 8, Kind: "runtime-helper", Function: 3},
	}
	wantSources := []shared.NativeSourceRange{
		{Offset: 8, Size: 8, Function: 2, WasmOffset: 5},
		{Offset: 24, Size: 8, Function: 2, WasmOffset: 10},
		{Offset: 52, Size: 8, Function: 3, WasmOffset: 1},
	}
	if !reflect.DeepEqual(regions, wantRegions) || !reflect.DeepEqual(sources, wantSources) {
		t.Fatal("incorrect finalized layout/source coordinates", regions, sources)
	}
	if err := jitprofile.ValidateRegions(regions, 72); err != nil {
		t.Fatal(err)
	}
	if err := jitprofile.ValidateSources(sources, 72); err != nil {
		t.Fatal(err)
	}
	for offset := uint64(0); offset <= 72; offset++ {
		want := offset >= 8 && offset < 16 || offset >= 24 && offset < 32 || offset >= 52 && offset < 60
		if _, found := jitprofile.LookupSource(sources, offset); found != want {
			t.Fatal("wrapper/helper/padding gap filled or boundary lost", offset)
		}
	}
	functions[0].Sources[0].WasmOffset = 99
	functions[0].BodyRegions[0].Kind = profileRuntimeHelper
	if !reflect.DeepEqual(regions, wantRegions) || !reflect.DeepEqual(sources, wantSources) {
		t.Fatal("final metadata aliases compiler backing")
	}
}

func TestProfileMetadataRejectsMalformedLayoutsTransactionally(t *testing.T) {
	for name, mutate := range map[string]func([]profileFunctionLayout){
		"function-index":             func(f []profileFunctionLayout) { f[1].Function = maxProfileFunctionIndex },
		"function-order":             func(f []profileFunctionLayout) { f[1].Function = 2 },
		"entry-overlap":              func(f []profileFunctionLayout) { f[1].Entry = 32 },
		"entry-gap":                  func(f []profileFunctionLayout) { f[1].Entry = 64 },
		"entry-alignment":            func(f []profileFunctionLayout) { f[1].Entry = 44 },
		"negative-prefix":            func(f []profileFunctionLayout) { f[1].PrefixBytes = -4 },
		"missing-prefix":             func(f []profileFunctionLayout) { f[1].PrefixBytes = 0 },
		"unaligned-body":             func(f []profileFunctionLayout) { f[1].BodyBytes = 7 },
		"oversized-epilogue":         func(f []profileFunctionLayout) { f[1].EpilogueBytes = 24 },
		"missing-epilogue":           func(f []profileFunctionLayout) { f[1].EpilogueBytes = 0 },
		"oversized-code":             func(f []profileFunctionLayout) { f[1].CodeBytes = MaxCodeBytes + 4 },
		"body-gap":                   func(f []profileFunctionLayout) { f[1].BodyRegions[0].Offset = 4 },
		"body-overrun":               func(f []profileFunctionLayout) { f[1].BodyRegions[0].Size = 12 },
		"body-omission":              func(f []profileFunctionLayout) { f[1].BodyRegions = nil },
		"unknown-kind":               func(f []profileFunctionLayout) { f[1].BodyRegions[0].Kind = 0 },
		"source-before-body":         func(f []profileFunctionLayout) { f[0].Sources[0].Offset = 96 },
		"source-unaligned":           func(f []profileFunctionLayout) { f[1].Sources[0].Size = 7 },
		"source-empty":               func(f []profileFunctionLayout) { f[1].Sources[0].Size = 0 },
		"source-overflow":            func(f []profileFunctionLayout) { f[1].Sources[0].Size = ^uint64(0) },
		"source-helper":              func(f []profileFunctionLayout) { f[0].Sources[0].Size = 16 },
		"source-outside":             func(f []profileFunctionLayout) { f[1].Sources[0].Offset = 8 },
		"source-function":            func(f []profileFunctionLayout) { f[1].Sources[0].Function = 2 },
		"source-inline":              func(f []profileFunctionLayout) { f[1].Sources[0].InlineParent = 1 },
		"source-overlap":             func(f []profileFunctionLayout) { f[0].Sources[1].Offset = 100 },
		"source-before-decls":        func(f []profileFunctionLayout) { f[1].Sources[0].WasmOffset = 0 },
		"source-after-body":          func(f []profileFunctionLayout) { f[1].Sources[0].WasmOffset = 11 },
		"wasm-coordinate-overflow":   func(f []profileFunctionLayout) { f[1].LocalDeclBytes = ^uint32(0) },
		"native-coordinate-overflow": func(f []profileFunctionLayout) { f[1].BodySourceStart = MaxCodeBytes },
	} {
		t.Run(name, func(t *testing.T) {
			functions := profileMetadataFixture()
			mutate(functions)
			regions, sources, err := finalizeProfileMetadata(functions, 72)
			if err == nil || regions != nil || sources != nil {
				t.Fatal("malformed profile retained partial finalized output", err, regions, sources)
			}
		})
	}
	for _, size := range []int{-4, 71, 76, MaxCodeBytes + 4} {
		if regions, sources, err := finalizeProfileMetadata(profileMetadataFixture(), size); err == nil || regions != nil || sources != nil {
			t.Fatal("invalid final image accepted", size, err)
		}
	}
}

func TestProfileMetadataEmptyImagesAndBudget(t *testing.T) {
	regions, sources, err := finalizeProfileMetadata(nil, 0)
	if err != nil || regions != nil || sources != nil {
		t.Fatal("empty image invented metadata", regions, sources, err)
	}
	if _, _, err := finalizeProfileMetadata(nil, 4); err == nil {
		t.Fatal("unowned image admitted")
	}
	var budget profileMetadataBudget
	if err := budget.charge(maxProfileModuleRanges, maxProfileModuleRanges); err != nil {
		t.Fatal("exact metadata bound rejected", err)
	}
	before := budget
	for _, counts := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		if err := budget.charge(counts[0], counts[1]); err == nil || budget != before {
			t.Fatal("failed budget charge mutated owner", counts, budget)
		}
	}
	f := profileMetadataFixture()[0]
	f.BodyBytes, f.BodyRegions, f.Sources = 0, nil, nil
	f.CodeBytes = f.PrefixBytes + f.EpilogueBytes
	regions, sources, err = finalizeProfileMetadata([]profileFunctionLayout{f}, f.CodeBytes)
	if err != nil || len(regions) != 2 || sources != nil {
		t.Fatal("empty body wrapper layout", regions, sources, err)
	}
}

func TestProfileSourceRecorderOriginsBoundsAndReuse(t *testing.T) {
	var recorder profileSourceRecorder
	one := shared.EmissionOrigin{Function: 4, PC: 17, Valid: true}
	two := shared.EmissionOrigin{Function: 4, PC: 29, Valid: true}
	for _, step := range []struct {
		at     int
		origin shared.EmissionOrigin
	}{{8, one}, {12, two}, {16, one}, {20, shared.EmissionOrigin{}}, {28, one}} {
		if err := recorder.switchOrigin(step.at, step.origin); err != nil {
			t.Fatal(err)
		}
	}
	ranges, err := recorder.finish(32)
	want := []shared.NativeSourceRange{{Offset: 8, Size: 4, Function: 4, WasmOffset: 17}, {Offset: 12, Size: 4, Function: 4, WasmOffset: 29}, {Offset: 16, Size: 4, Function: 4, WasmOffset: 17}, {Offset: 28, Size: 4, Function: 4, WasmOffset: 17}}
	if err != nil || !slices.Equal(ranges, want) {
		t.Fatal("nested source scopes/gaps", ranges, err)
	}
	for _, offset := range []uint64{0, 7, 20, 24, 27, 32} {
		if _, found := jitprofile.LookupSource(ranges, offset); found {
			t.Fatal("unknown source gap filled", offset)
		}
	}
	capacity := cap(recorder.ranges)
	if err := recorder.switchOrigin(31, one); err == nil || len(recorder.ranges) != 0 {
		t.Fatal("nonmonotonic/unaligned emission did not invalidate map")
	}
	if ranges, err := recorder.finish(36); err == nil || ranges != nil {
		t.Fatal("failed map exposed output")
	}
	recorder.reset()
	if cap(recorder.ranges) != capacity || recorder.err != nil {
		t.Fatal("recorder reset lost bounded backing")
	}
	for i := 0; i <= maxProfileFunctionRanges; i++ {
		if err := recorder.switchOrigin(4*i, shared.EmissionOrigin{Function: 0, PC: uint32(i), Valid: true}); err != nil {
			t.Fatal("exact source bound", i, err)
		}
	}
	if len(recorder.ranges) != maxProfileFunctionRanges || cap(recorder.ranges) != maxProfileFunctionRanges {
		t.Fatal("source cap changed")
	}
	if ranges, err := recorder.finish(4 * (maxProfileFunctionRanges + 1)); err == nil || ranges != nil || len(recorder.ranges) != 0 {
		t.Fatal("source cap failure retained partial map")
	}
	var absent *profileSourceRecorder
	if err := absent.switchOrigin(0, one); err != nil {
		t.Fatal(err)
	}
	if ranges, err := absent.finish(0); err != nil || ranges != nil {
		t.Fatal("nil recorder created metadata")
	}
}

func TestProfilePublicCoordinatesExceedPrivatePilot(t *testing.T) {
	functions := profileMetadataFixture()
	for i := range functions {
		functions[i].Function += 65536
		for j := range functions[i].Sources {
			functions[i].Sources[j].Function += 65536
		}
	}
	if _, _, err := finalizeProfileMetadata(functions, 72); err != nil {
		t.Fatal("large function identity", err)
	}
	size := MaxCodeBytes + 4
	large := profileFunctionLayout{Function: 65536, Entry: 0, CodeBytes: size, PrefixBytes: 4, BodyBytes: size - 8, EpilogueBytes: 4, BodySourceStart: 4, WasmBodyBytes: 1, BodyRegions: []profileBodyRegion{{Offset: 0, Size: size - 8, Kind: profileGuestBody}}}
	if _, _, err := finalizeProfileMetadata([]profileFunctionLayout{large}, size); err != nil {
		t.Fatal("large native coordinate", err)
	}
}
