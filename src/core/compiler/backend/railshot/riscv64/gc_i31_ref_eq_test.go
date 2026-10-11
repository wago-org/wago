//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64I31RefEqPilotHex = "0061736d01000000018a808080000260000060027f7f017f038380808000020001048480808000016d0005078d808080000204696e6974000002657100010abc8080800002a680808000004100d06d26004101d06c260041024107fb1c260041034107fb1c260041044108fb1c26000b8b80808000002000250020012500d30b"
const rv64OfficialObjectRefEqHex = "0061736d0100000001af808080000950005f0050005f017f005e78005001005f005001005f005001015f017f005001015f017f0060000060027f7f017f038380808000020708048480808000016d0014078d808080000204696e6974000002657100010adc8080800002c680808000004100d06d26004101d06c260041024107fb1c260041034107fb1c260041044108fb1c26004105fb010026004106fb0100260041074100fb0702260041084100fb070226000b8b80808000002000250020012500d30b"
const rv64GCStructGetHex = "0061736d01000000018980808000025f017f016000017f0382808080000101078780808000010367657400000a8f8080800001898080800000fb0100fb0200000b"
const rv64GCArrayLocalHex = "0061736d01000000019680808000045e7f0160027f7f017f60037f7f7f017f60017f017f0384808080000301020307978080800003036765740000077365745f6765740001036c656e00020ac180808000038c80808000002000fb07002001fb0b000b9c80808000010163002000fb07002103200320012002fb0e0020032001fb0b000b8980808000002000fb0700fb0f0b"

func rv64I31RefEqPilotBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64I31RefEqPilotHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRV64I31RefEqPilotAdmissionAndImage(t *testing.T) {
	source := rv64I31RefEqPilotBytes(t)
	wantSource := [sha256.Size]byte{0xd4, 0x1b, 0x88, 0xea, 0xab, 0x33, 0x8e, 0x5b, 0x01, 0xe6, 0x7e, 0xbf, 0x43, 0x52, 0xce, 0x12, 0x6d, 0xfd, 0xa5, 0x0d, 0x26, 0xaa, 0xb2, 0x3c, 0x76, 0x1f, 0xd5, 0x6c, 0x3a, 0x29, 0xd5, 0xaf}
	if len(source) != 128 || sha256.Sum256(source) != wantSource {
		t.Fatalf("i31 ref.eq source identity changed: len=%d sha=%x", len(source), sha256.Sum256(source))
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64I31RefEqPilot(m) || !admitRV64I31Core(m) || !admitRV64I31TableProduct(m) || !admitRV64I31CoreWire(source) || !AdmitI31CoreSource(source) {
		t.Fatal("i31 ref.eq classifiers rejected")
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true}
	if _, err = CompileModuleWith(m, opts); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded ref.eq shape escaped exact-wire provenance", err)
	}
	opts.I31CoreSource = true
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal("production compile", err)
	}
	wantDigest := [sha256.Size]byte{0x35, 0xa6, 0xd7, 0xcf, 0x47, 0xfe, 0xec, 0xb8, 0x28, 0x37, 0xbd, 0xfd, 0x50, 0x1c, 0xbd, 0x67, 0xa8, 0x73, 0xe2, 0xab, 0xb3, 0xc6, 0xd0, 0x10, 0xa9, 0xa1, 0xb0, 0xda, 0x3e, 0x33, 0xe9, 0x23}
	wantEntry := []int{0, 1488}
	if len(compiled.Code) != 2436 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("ref.eq image identity = %d/%x entries %v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}

	// Insert an empty memory section after the function section. Decoding
	// discards it, but the exact source-provenance witness must not.
	nearWire := slices.Concat(source[:43], []byte{5, 1, 0}, source[43:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64I31RefEqPilot(nearModule) || admitRV64I31CoreWire(nearWire) || AdmitI31CoreSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	opts.I31CoreSource = false
	if _, err = CompileModuleWith(nearModule, opts); !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}

	for name, mutate := range map[string]func(*wasm.Module){
		"body":       func(v *wasm.Module) { v.Code[1].BodyBytes[8] = 0xd1 },
		"export":     func(v *wasm.Module) { v.Exports[1].Name = "ne" },
		"table-type": func(v *wasm.Module) { v.Tables[0].Type.Ref = wasm.AnyRef.Ref() },
	} {
		candidate, err := wasm.DecodeModule(slices.Clone(source))
		if err != nil {
			t.Fatal(err)
		}
		mutate(candidate)
		if admitRV64I31RefEqPilot(candidate) || admitRV64I31Core(candidate) {
			t.Fatal("decoded near miss admitted", name)
		}
		opts.I31CoreSource = true
		if _, err = CompileModuleWith(candidate, opts); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded near miss escaped provenance", name, err)
		}
	}

	full, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
	if err != nil {
		t.Fatal(err)
	}
	fullModule, err := wasm.DecodeModule(full)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 197 || admitRV64I31RefEqPilot(fullModule) || admitRV64I31Core(fullModule) || admitRV64I31CoreWire(full) || AdmitI31CoreSource(full) {
		t.Fatal("object-allocating official ref_eq module entered the compact pilot")
	}
	opts.I31CoreSource = true
	if _, err = CompileModuleWith(fullModule, opts); !errors.Is(err, ErrUnsupported) {
		t.Fatal("object-allocating official ref_eq module compiled without collector lowering", err)
	}
}

func TestRV64I31RefEqUsesFullWidthIdentity(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	fn := &wasm.Func{BodyBytes: []byte{0x20, 0, 0x25, 0, 0x20, 1, 0x25, 0, 0xd3, 0x0b}}
	state := moduleState{
		production:      true,
		i31Core:         true,
		ownedTable:      true,
		tableAddr64:     []bool{false},
		tableCompactRef: []bool{true},
		types:           []functionSpec{{signature: ft}},
		functions:       []functionSpec{{signature: ft}},
	}
	if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("ref.eq escaped its exact product witness", err)
	}
	state.i31RefEq = true
	summary, err := walkInteger(fn, ft, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := compileIntegerMode(fn, ft, &state, summary, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, instruction := range map[string]uint32{
		"left-full-width-load":  rv64IType(0x03, 3, 5, 2, 32),
		"right-full-width-load": rv64IType(0x03, 3, 6, 2, 40),
	} {
		if !rv64HasInstruction(code, instruction) {
			t.Fatalf("%s missing: %#08x", name, instruction)
		}
	}
	if !rv64HasInstruction(code, 0x0062c2b3) || !rv64HasInstruction(code, rv64IType(0x13, 3, 5, 5, 1)) {
		t.Fatal("ref.eq did not compare full carrier words and normalize its i32 result")
	}
}

func TestRV64OfficialGCRefEqAllocationHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRefEqWire(source) || !AdmitGCRefEqSource(source) || !admitRV64GCRefEq(m) {
		t.Fatal("official allocating ref.eq identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 2, GCRefEqSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	}
	withoutWire := opts
	withoutWire.GCRefEqSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("allocating ref.eq escaped exact wire provenance", err)
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"missing-descriptors": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeDescs = nil
		},
		"wrong-descriptor": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeDescs[0].Final = !candidate.Codegen.Module.GCTypeDescs[0].Final
		},
		"missing-layouts": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts = nil
		},
		"wrong-layout": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts[0].PointerFree = !candidate.Codegen.Module.GCTypeLayouts[0].PointerFree
		},
		"frame-roots": func(candidate *CompileOptions) {
			candidate.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code))
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("allocating ref.eq escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if image == nil || len(image.Code) == 0 || len(image.Entry) != 2 || image.Entry[0] != 0 || image.Entry[1] <= image.Entry[0] {
		t.Fatal("allocating ref.eq helper image metadata", image)
	}
}

func TestRV64GCStructGetHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructGetHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCStructGetWire(source) || !AdmitGCStructGetSource(source) || !admitRV64GCStructGet(m) {
		t.Fatal("gc struct.get identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 3, GCStructGetSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	}
	withoutWire := opts
	withoutWire.GCStructGetSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("gc struct.get escaped exact wire provenance", err)
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"missing-descriptors": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs = nil },
		"wrong-descriptor": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeDescs[0].Size++
		},
		"missing-layouts": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeLayouts = nil },
		"wrong-layout": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts[0].PointerFree = !candidate.Codegen.Module.GCTypeLayouts[0].PointerFree
		},
		"frame-roots": func(candidate *CompileOptions) {
			candidate.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code))
		},
		"array-helper":  func(candidate *CompileOptions) { candidate.GCArrayHelpers = true },
		"slot-capacity": func(candidate *CompileOptions) { candidate.SyncHostSlots = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("gc struct.get escaped exact helper/collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantCode := [sha256.Size]byte{0x54, 0xa4, 0xd5, 0x21, 0x52, 0xa3, 0x42, 0x28, 0xf5, 0x08, 0x8d, 0xc4, 0x62, 0xea, 0x05, 0x47, 0x60, 0xfc, 0x3b, 0xd5, 0x92, 0x4c, 0x0a, 0xda, 0x6c, 0x43, 0xb7, 0x62, 0xc0, 0xd5, 0x11, 0x6d}
	if image == nil || len(image.Code) != 1244 || sha256.Sum256(image.Code) != wantCode || len(image.Entry) != 1 || image.Entry[0] != 0 || !slices.Equal(image.Entry, image.InternalEntry) {
		t.Fatal("gc struct.get helper image metadata", image)
	}
}

func TestRV64GCArrayLocalHelperImage(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayLocalHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayLocalWire(source) || !AdmitGCArrayLocalSource(source) || !admitRV64GCArrayLocal(m) {
		t.Fatal("gc array local identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 4, GCArrayLocalSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	}
	withoutWire := opts
	withoutWire.GCArrayLocalSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("gc array local escaped exact wire provenance", err)
	}
	// Insert an empty memory section after the function section. Decoding drops
	// it, but the exact source-provenance witness must not.
	nearWire := slices.Concat(source[:46], []byte{5, 1, 0}, source[46:])
	nearModule, err := wasm.DecodeModule(nearWire)
	if err != nil {
		t.Fatal("decode inert-section near miss", err)
	}
	if !admitRV64GCArrayLocal(nearModule) || admitRV64GCArrayLocalWire(nearWire) || AdmitGCArrayLocalSource(nearWire) {
		t.Fatal("inert-section near miss lost decoded/wire distinction")
	}
	withoutWire.GCArrayLocalSource = false
	if image, err := CompileModuleWith(nearModule, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("inert-section near miss escaped provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body":   func(v *wasm.Module) { v.Code[0].BodyBytes[8] = 0x0f },
		"export": func(v *wasm.Module) { v.Exports[2].Name = "size" },
		"array-type": func(v *wasm.Module) {
			v.Types[0].SubTypes[0].Comp.Array = wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const)
		},
		"local-type": func(v *wasm.Module) { v.Code[1].Locals.Runs[0].Type = wasm.I64 },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayLocal(candidate) {
				t.Fatal("decoded near miss admitted")
			}
			if image, err := CompileModuleWith(candidate, opts); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded near miss escaped provenance", err)
			}
		})
	}
	for name, mutate := range map[string]func(*CompileOptions){
		"struct-helper":       func(candidate *CompileOptions) { candidate.GCStructHelpers = true },
		"missing-descriptors": func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs = nil },
		"wrong-descriptor":    func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeDescs[0].ElemSize++ },
		"missing-layouts":     func(candidate *CompileOptions) { candidate.Codegen.Module.GCTypeLayouts = nil },
		"wrong-layout": func(candidate *CompileOptions) {
			candidate.Codegen.Module.GCTypeLayouts[0].PointerFree = !candidate.Codegen.Module.GCTypeLayouts[0].PointerFree
		},
		"frame-roots":   func(candidate *CompileOptions) { candidate.GCFrameRoots = shared.NewGCModuleFrameRootPlan(len(m.Code)) },
		"slot-capacity": func(candidate *CompileOptions) { candidate.SyncHostSlots = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := opts
			candidate.Codegen.Module.GCTypeDescs = slices.Clone(opts.Codegen.Module.GCTypeDescs)
			candidate.Codegen.Module.GCTypeLayouts = slices.Clone(opts.Codegen.Module.GCTypeLayouts)
			mutate(&candidate)
			if image, err := CompileModuleWith(m, candidate); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("gc array local escaped exact collector metadata", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x88, 0xe5, 0x7a, 0x7f, 0x97, 0x1e, 0x9d, 0x4a, 0xf1, 0x5e, 0x5f, 0x79, 0xd1, 0xa4, 0x6b, 0xc6, 0x44, 0x8e, 0xaa, 0xa1, 0xae, 0x2d, 0x02, 0x61, 0xd3, 0x18, 0xa6, 0xa8, 0xba, 0xe6, 0xac, 0x79}
	wantEntry := []int{0, 784, 1680}
	if image == nil || len(image.Code) != 3468 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("gc array local helper image metadata", image)
	}
}
