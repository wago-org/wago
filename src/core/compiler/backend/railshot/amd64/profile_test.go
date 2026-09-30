//go:build amd64 && wago_profile

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProfileFinalizedLayout(t *testing.T) {
	var types, body, exports [][]byte
	for i := 0; i < 32; i++ {
		types = append(types, wasmtest.ULEB(0))
		body = append(body, wasmtest.Code([]byte{0x20, 0, 0x28, 2, 0, 0xb}))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("load%d", i), 0, uint32(i)))
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(types...)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(body...)),
	)
	for _, exportAll := range []bool{true, false} {
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if !exportAll {
			m.Exports = m.Exports[:1]
		}
		for _, compact := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				t.Run(fmt.Sprintf("all-exports=%v/compact=%v/workers=%d", exportAll, compact, workers), func(t *testing.T) {
					opts := CompileOptions{CompactNative: compact, Workers: workers, DeferCodeMapping: true}
					plain, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					var stats ModuleStats
					opts.Stats = &stats
					opts.Profile = true
					opts.SourceMaps = true
					observed, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(plain.Code, observed.Code) {
						t.Fatal("profiling changed generated code")
					}
					if err := jitprofile.ValidateRegions(stats.ProfileRegions, uint64(len(observed.Code))); err != nil {
						t.Fatal(err)
					}

					if err := jitprofile.ValidateSources(stats.SourceRanges, uint64(len(observed.Code))); err != nil {
						t.Fatal(err)
					}
					if len(stats.SourceRanges) == 0 {
						t.Fatal("source map empty")
					}
					if err := jitprofile.ValidateCodeSites(stats.CodeSites, uint64(len(observed.Code))); err != nil {
						t.Fatal(err)
					}
					if err := jitprofile.ValidateCodeSiteRegions(stats.CodeSites, stats.ProfileRegions); err != nil {
						t.Fatal(err)
					}
					if len(stats.CodeSites) < 32 {
						t.Fatal("missing bounds-check sites", stats.CodeSites)
					}
					for _, site := range stats.CodeSites {
						if site.Kind != "memory-bounds-branch" {
							t.Fatal("unexpected site", site)
						}
						raw := observed.Code[site.Offset : site.Offset+site.Size]
						if !(len(raw) == 6 && raw[0] == 0x0f && raw[1]&0xf0 == 0x80 || len(raw) == 2 && raw[0]&0xf0 == 0x70) {
							t.Fatalf("site is not a finalized conditional branch: %+v %x", site, raw)
						}
						origin, ok := jitprofile.LookupSource(stats.SourceRanges, site.Offset)
						if !ok || origin.WasmOffset != 3 {
							t.Fatal("bounds site lost its load origin", site, origin)
						}
					}

					var checks, loads [32]bool
					for _, source := range stats.SourceRanges {
						if source.Function >= 32 || (source.WasmOffset != 3 && source.WasmOffset != 6) {
							t.Fatalf("unexpected Wasm provenance: %+v", source)
						}
						if source.Offset < uint64(observed.Entry[source.Function]) || source.Offset+source.Size > uint64(observed.Entry[source.Function]+stats.Funcs[source.Function].NativeSize.TotalBytes) {
							t.Fatal("source escaped function", source)
						}
						// The end opcode may emit a return-value move. Both the
						// bounds check and deferred load must retain the load PC.
						if source.WasmOffset != 3 {
							continue
						}

						code := observed.Code[source.Offset : source.Offset+source.Size]
						for at, op := range code {
							if op == 0x0f && at+5 < len(code) && code[at+1]&0xf0 == 0x80 || op >= 0x70 && op <= 0x7f && at+1 < len(code) {
								checks[source.Function] = true
							}
							// MOV r32, [RBX+index]: ModRM selects SIB, SIB base is RBX.
							if op == 0x8b && at+2 < len(code) && code[at+1]&0xc7 == 4 && code[at+2]&7 == 3 {
								loads[source.Function] = true
							}
						}

					}
					for i := range checks {
						if !checks[i] || !loads[i] {
							t.Fatalf("function %d: mapped bounds check=%v, mapped deferred load=%v; sources=%+v", i, checks[i], loads[i], stats.SourceRanges)
						}
					}
					for _, region := range stats.ProfileRegions {
						if region.Function >= 0 {
							f := stats.Funcs[region.Function]
							start := observed.Entry[region.Function]
							if int(region.Offset) < start || int(region.Offset+region.Size) > start+f.NativeSize.TotalBytes {
								t.Fatal("region escaped owning final function", region)
							}
						}
					}
				})
			}
		}
	}
}

func TestProfileTracksDeferredArithmeticOrigins(t *testing.T) {
	for _, op := range []byte{0x6d, 0x6e, 0x6f, 0x70, 0x7f, 0x80, 0x81, 0x82} {
		for _, compact := range []bool{false, true} {
			typ := wasm.I32
			if op >= 0x7f {
				typ = wasm.I64
			}
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("divide", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, op, 0x0b}))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, DeferCodeMapping: true})
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			observed, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, CompactNative: compact, DeferCodeMapping: true})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(plain.Code, observed.Code) {
				t.Fatal("source metadata changed native code")
			}
			if len(stats.SourceRanges) == 0 {
				t.Fatalf("opcode %x compact=%v lost provenance", op, compact)
			}
			foundDivision := false
			for _, source := range stats.SourceRanges {
				for at := source.Offset; at+2 <= source.Offset+source.Size; at++ {
					// This fixture has register operands and no integer constants:
					// F7 /6 and /7 are its emitted DIV/IDIV instructions.
					if observed.Code[at] == 0xf7 && observed.Code[at+1]&0xf0 == 0xf0 {
						foundDivision = true
					}
				}

				if source.Function != 0 || source.WasmOffset != 5 {
					t.Fatalf("deferred emission context presented as operation origin: %+v", source)
				}
			}
			if !foundDivision {
				t.Fatalf("opcode %x compact=%v did not map the division instruction", op, compact)
			}
		}
	}
}

func TestProfileNestedDeferredOrigins(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("divide", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6d, 0x20, 2, 0x6d, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		var stats ModuleStats
		_, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, CompactNative: compact, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[uint32]bool{}
		for _, source := range stats.SourceRanges {
			if source.WasmOffset != 5 && source.WasmOffset != 8 {
				t.Fatal(source)
			}
			seen[source.WasmOffset] = true
		}
		if len(seen) != 2 {
			t.Fatal("nested origin restoration failed", seen)
		}
	}
}

func TestProfileDeferredExpressionOrigins(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("expression", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x73, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		plain, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		observed, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, CompactNative: compact, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain.Code, observed.Code) {
			t.Fatal("emission metadata changed native code")
		}
		if err := jitprofile.ValidateSources(stats.SourceRanges, uint64(len(observed.Code))); err != nil {
			t.Fatal(err)
		}
		seen := map[uint32]bool{}
		for _, r := range stats.SourceRanges {
			if r.Function != 0 || r.WasmOffset != 5 && r.WasmOffset != 8 {
				t.Fatal("wrong expression origin", r)
			}
			seen[r.WasmOffset] = true
		}
		if len(seen) != 2 {
			t.Fatal("nested expression emission not mapped", seen)
		}
	}
}

func TestProfileInlineCallerLocations(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec([]byte{1, 'm', 1, 'f', 0, 0})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("first", 0, 2), wasmtest.ExportEntry("second", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x73, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 3, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			plain, err := CompileModuleWith(m, CompileOptions{CompactNative: compact, Workers: workers, DeferCodeMapping: true})
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			observed, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, CompactNative: compact, Workers: workers, DeferCodeMapping: true})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(plain.Code, observed.Code) {
				t.Fatal("inline metadata changed native code")
			}
			if err := jitprofile.ValidateInlineSources(stats.SourceRanges, stats.SourceFrames); err != nil {
				t.Fatal(err)
			}
			seen := map[uint32]bool{}
			for _, r := range stats.SourceRanges {
				if r.InlineParent == 0 {
					continue
				}
				callers := jitprofile.InlineCallers(stats.SourceFrames, r.InlineParent)
				if len(callers) != 1 || r.WasmOffset != 5 || callers[0].WasmOffset != 3 || callers[0].Function != r.Function+1 {
					t.Fatalf("wrong inline provenance: %+v callers=%+v", r, callers)
				}
				if r.Function != 1 && r.Function != 3 {
					t.Fatal("local index used as full Wasm index", r)
				}
				callerLocal := int(callers[0].Function) - 1
				if r.Offset < uint64(observed.Entry[callerLocal]) || r.Offset+r.Size > uint64(observed.Entry[callerLocal]+stats.Funcs[callerLocal].NativeSize.TotalBytes) {
					t.Fatal("inline location escaped physical caller", r)
				}
				seen[r.Function] = true
			}
			if len(seen) != 2 {
				t.Fatalf("compact=%v workers=%d missing inline sites: %v calls=%v/%v", compact, workers, seen, stats.Funcs[1].Calls, stats.Funcs[3].Calls)
			}
		}
	}
}

func TestProfileInlineIdentityKeepsOperandOrigin(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x6a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 5, 0x73, 0x10, 0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	_, err = CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Funcs[1].Calls["inline"] != 1 {
		t.Fatal("fixture did not inline")
	}
	found := false
	for _, r := range stats.SourceRanges {
		if r.Function == 1 && r.WasmOffset == 5 {
			found = true
			if r.InlineParent != 0 {
				t.Fatal("eliminated inline add relabeled caller XOR", r)
			}
		}
	}
	if !found {
		t.Fatal("caller operand lost its source origin")
	}
}

// Exercise eager control/call lowering around a load that is materialized later.
// An imported call cannot disappear through guest inlining.
func TestProfileOpcodeLowering(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec([]byte{1, 'm', 1, 'f', 0, 0})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("choose", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x04, 0x7f, // local.get 0; if (result i32)
			0x20, 0, 0x10, 0, // local.get 0; call imported callback
			0x05, 0x20, 0, 0x28, 2, 0, // else; local.get 0; i32.load
			0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		opts := CompileOptions{CompactNative: compact, DeferCodeMapping: true}
		plain, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		opts.Stats, opts.Profile, opts.SourceMaps = &stats, true, true
		observed, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain.Code, observed.Code) {
			t.Fatal("profiling changed native code")
		}
		if err := jitprofile.ValidateSources(stats.SourceRanges, uint64(len(observed.Code))); err != nil {
			t.Fatal(err)
		}
		covered := make(map[uint32]bool)
		for _, source := range stats.SourceRanges {
			if source.Function != 1 {
				t.Fatalf("lost import offset: %+v", source)
			}
			switch source.WasmOffset {
			case 1, 3, 5, 7, 9, 10, 12, 15, 16:
			default:
				t.Fatalf("location is not an opcode boundary: %+v", source)
			}
			covered[source.WasmOffset] = true
		}
		for _, pc := range []uint32{3, 7, 12} {
			if !covered[pc] {
				t.Fatalf("compact=%v missing if/call/load pc=%d; ranges=%+v", compact, pc, stats.SourceRanges)
			}
		}
	}
}

func TestProfileFloatVectorLocalsFinalizedLayout(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.V128} {
		body := []byte{1, 63, wasm.MustEncodeValType(typ)}
		body = append(body, 0x20, 0, 0x21, 64, 0x20, 64, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("transfer", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, compact := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					opts := CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, CompactNative: compact, Workers: workers, DeferCodeMapping: true}
					plain, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					var stats ModuleStats
					opts.Stats, opts.Profile, opts.SourceMaps = &stats, true, true
					observed, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(plain.Code, observed.Code) {
						t.Fatal("profiling changed native code", typ, features, compact, workers)
					}
					if err := jitprofile.ValidateCodeSites(stats.CodeSites, uint64(len(observed.Code))); err != nil {
						t.Fatal(err)
					}
					if err := jitprofile.ValidateCodeSiteRegions(stats.CodeSites, stats.ProfileRegions); err != nil {
						t.Fatal(err)
					}
					prefix := "fp-local-"
					if typ == wasm.V128 {
						prefix = "vector-local-"
					}
					seen := make(map[string]bool)
					for _, site := range stats.CodeSites {
						seen[site.Kind] = true
					}
					if !seen[prefix+"load"] || !seen[prefix+"store"] {
						t.Fatal("missing local traffic", typ, features, compact, workers, stats.CodeSites)
					}
				}
			}
		}
	}
}
