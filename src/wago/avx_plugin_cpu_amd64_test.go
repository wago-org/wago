//go:build amd64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	amd64codegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPluginAVXRequirementsSurviveArtifactRoundTrip(t *testing.T) {
	originalAVX2, originalAVX512 := avx2HostFeaturesSupported, avx512HostFeaturesSupported
	defer func() { avx2HostFeaturesSupported, avx512HostFeaturesSupported = originalAVX2, originalAVX512 }()
	avx2HostFeaturesSupported = func() bool { return true }
	avx512HostFeaturesSupported = func() bool { return true }
	for _, tc := range []struct {
		name string
		set  func(*Compiled)
		get  func(*Compiled) bool
		deny func()
	}{
		{"AVX2", func(c *Compiled) { c.requiresAVX2 = true }, (*Compiled).RequiresAVX2, func() { avx2HostFeaturesSupported = func() bool { return false } }},
		{"AVX-512", func(c *Compiled) { c.requiresAVX512 = true }, (*Compiled).RequiresAVX512, func() { avx512HostFeaturesSupported = func() bool { return false } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &Compiled{}
			tc.set(input)
			blob, err := input.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if err := loaded.UnmarshalBinary(blob); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if !tc.get(&loaded) {
				t.Fatalf("%s requirement was lost", tc.name)
			}
			tc.deny()
			var rejected Compiled
			if err := rejected.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "requires "+tc.name) {
				t.Fatalf("unsupported %s artifact error = %v", tc.name, err)
			}
			avx2HostFeaturesSupported = func() bool { return true }
			avx512HostFeaturesSupported = func() bool { return true }
		})
	}
}

func TestPluginAVXLegacyArtifactRejected(t *testing.T) {
	blob, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var marker [8]byte
	binary.LittleEndian.PutUint64(marker[:], compiledCPURequirementsV1)
	if count := bytes.Count(blob, marker[:]); count != 1 {
		t.Fatalf("CPU requirements marker count = %d, want 1", count)
	}
	legacy := append([]byte(nil), blob...)
	legacy[bytes.Index(legacy, marker[:])+6] &^= 0x10
	var loaded Compiled
	if err := loaded.UnmarshalBinary(legacy); err == nil || !strings.Contains(err.Error(), "lacks CPU requirements metadata") {
		t.Fatalf("legacy artifact error = %v", err)
	}
}

func TestPluginAVXCompileHostGate(t *testing.T) {
	originalAVX2, originalAVX512 := avx2HostFeaturesSupported, avx512HostFeaturesSupported
	defer func() { avx2HostFeaturesSupported, avx512HostFeaturesSupported = originalAVX2, originalAVX512 }()
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/machine", "avx.marker", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
	for _, tc := range []struct {
		name string
		flag amd64codegen.Features
		deny func()
	}{
		{"AVX2", amd64codegen.FeatureAVX2, func() { avx2HostFeaturesSupported = func() bool { return false } }},
		{"AVX-512", amd64codegen.FeatureAVX512, func() { avx512HostFeaturesSupported = func() bool { return false } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			avx2HostFeaturesSupported = func() bool { return true }
			avx512HostFeaturesSupported = func() bool { return true }
			rt := NewRuntime()
			defer rt.Close()
			ext := instructionMachineExt{name: "avx.marker", output: []int32{32}, lowering: &amd64codegen.Lowering{
				Compatibility: amd64codegen.CompatibilityFullAccess,
				Features:      tc.flag,
				Emit: func(ctx amd64codegen.Context) error {
					r := ctx.AllocGP()
					ctx.Encoder().MovImm32(r, 1)
					return ctx.OutputI32(r)
				},
			}}
			if err := rt.Use(ext); err != nil {
				t.Fatal(err)
			}
			tc.deny()
			mod, err := rt.Compile(module)
			if mod != nil {
				defer mod.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "requires "+tc.name) {
				t.Fatalf("unsupported %s compile error = %v", tc.name, err)
			}
		})
	}
}

func TestAMD64AVXFeatureMasks(t *testing.T) {
	const ecx = uint32(1)<<27 | uint32(1)<<28
	const ebx = uint32(1)<<5 | uint32(1)<<16
	if !amd64AVX2FeaturesSupported(ecx, 0x6, ebx) || !amd64AVX512FeaturesSupported(ecx, 0xe6, ebx) {
		t.Fatal("complete AVX host features were rejected")
	}
	for _, bit := range []uint32{1 << 27, 1 << 28} {
		if amd64AVX2FeaturesSupported(ecx&^bit, 0xe6, ebx) || amd64AVX512FeaturesSupported(ecx&^bit, 0xe6, ebx) {
			t.Fatalf("missing ECX bit %#x was accepted", bit)
		}
	}
	for _, bit := range []uint32{1 << 1, 1 << 2} {
		if amd64AVX2FeaturesSupported(ecx, 0xe6&^bit, ebx) || amd64AVX512FeaturesSupported(ecx, 0xe6&^bit, ebx) {
			t.Fatalf("missing XCR0 bit %#x was accepted", bit)
		}
	}
	for _, bit := range []uint32{1 << 5, 1 << 6, 1 << 7} {
		if amd64AVX512FeaturesSupported(ecx, 0xe6&^bit, ebx) {
			t.Fatalf("missing AVX-512 state bit %#x was accepted", bit)
		}
	}
	if amd64AVX2FeaturesSupported(ecx, 0x6, ebx&^(1<<5)) || amd64AVX512FeaturesSupported(ecx, 0xe6, ebx&^(1<<16)) {
		t.Fatal("missing CPUID leaf-7 feature was accepted")
	}
}

func TestAVXCPUFlagsSupported(t *testing.T) {
	for _, tc := range []struct {
		flags        string
		avx2, avx512 bool
	}{
		{"flags : avx avx2 avx512f", true, true},
		{"flags : avx avx2", true, false},
		{"flags : avx avx512f", false, true},
		{"flags : avx2 avx512f", false, false},
		{"flags : xavx avx20 avx512foo", false, false},
	} {
		avx2, avx512 := avxCPUFlagsSupported([]byte(tc.flags))
		if avx2 != tc.avx2 || avx512 != tc.avx512 {
			t.Fatalf("flags %q = %v/%v, want %v/%v", tc.flags, avx2, avx512, tc.avx2, tc.avx512)
		}
	}
}
