//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64I31CoreExactAdmissionAndLowering(t *testing.T) {
	source := []byte(rv64I31CoreWire)
	wantSource := [sha256.Size]byte{0x91, 0xa5, 0x75, 0x41, 0xb3, 0x27, 0x61, 0xe0, 0xa6, 0x76, 0xb9, 0xf6, 0xf4, 0x79, 0x18, 0xf4, 0xcb, 0xfb, 0x40, 0x2e, 0xa1, 0x4f, 0x65, 0x84, 0xa4, 0x70, 0xa7, 0x39, 0x2b, 0x21, 0x21, 0x21}
	if len(source) != 65 || sha256.Sum256(source) != wantSource || !admitRV64I31CoreWire(source) || AdmitProductionSource(source) != nil {
		t.Fatal("canonical i31 source was not admitted", len(source), sha256.Sum256(source))
	}
	m, err := wasm.DecodeModule(source)
	if err != nil || !admitRV64I31Core(m) {
		t.Fatal("canonical decoded i31 profile was not admitted", err)
	}
	if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true}); image != nil || err == nil {
		if image != nil && image.CodeImage != nil {
			image.CodeImage.Close()
		}
		t.Fatal("decoded i31 profile compiled without exact source witness", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, I31CoreSource: true})
	if err != nil {
		t.Fatal(err)
	}
	if image.CodeImage != nil {
		defer image.CodeImage.Close()
	}
	wantCode := [sha256.Size]byte{0xa1, 0xd1, 0x89, 0xc2, 0x55, 0x71, 0x3e, 0x0d, 0xcb, 0x51, 0xbe, 0x35, 0xe1, 0x9c, 0xbc, 0x4d, 0x98, 0xbc, 0x68, 0x6e, 0xb2, 0x7c, 0xea, 0x08, 0x5b, 0xd4, 0xe2, 0x0a, 0xd0, 0x31, 0xd0, 0x18}
	if len(image.Code) != 672 || sha256.Sum256(image.Code) != wantCode || !slices.Equal(image.Entry, []int{0, 336}) || !slices.Equal(image.InternalEntry, image.Entry) {
		t.Logf("I31_CODE bytes=%d sha256=%x entry=%v internal=%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
		t.Fatal("unexpected exact i31 entry directory")
	}
	t.Logf("I31_CODE bytes=%d sha256=%x entry=%v", len(image.Code), sha256.Sum256(image.Code), image.Entry)
}

func TestRV64I31CoreRejectsSourceAndSemanticNearMisses(t *testing.T) {
	source := []byte(rv64I31CoreWire)
	for name, at := range map[string]int{
		"export":      27,
		"signed-op":   54,
		"unsigned-op": 63,
	} {
		candidate := slices.Clone(source)
		candidate[at] ^= 1
		if admitRV64I31CoreWire(candidate) {
			t.Fatal("source mutation admitted", name)
		}
		decoded, err := wasm.DecodeModule(candidate)
		if err == nil {
			image, compileErr := CompileModuleWith(decoded, CompileOptions{DeferCodeMapping: true, Interruptible: true, I31CoreSource: admitRV64I31CoreWire(candidate)})
			if image != nil && image.CodeImage != nil {
				image.CodeImage.Close()
			}
			if compileErr == nil {
				t.Fatal("source mutation compiled", name, compileErr)
			}
		}
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[0].BodyBytes = slices.Clone(v.Code[0].BodyBytes)
			v.Code[0].BodyBytes[5] = 0x1e
		},
		"export":       func(v *wasm.Module) { v.Exports[0].Name = "other" },
		"extra-global": func(v *wasm.Module) { v.Globals = append(v.Globals, wasm.Global{}) },
	} {
		copy := *m
		copy.Code = slices.Clone(m.Code)
		copy.Exports = slices.Clone(m.Exports)
		mutate(&copy)
		if admitRV64I31Core(&copy) {
			t.Fatal("semantic mutation admitted", name)
		}
		image, err := CompileModuleWith(&copy, CompileOptions{DeferCodeMapping: true, Interruptible: true, I31CoreSource: true})
		if image != nil {
			image.CodeImage.Close()
		}
		if err == nil {
			t.Fatal("semantic mutation compiled", name, err)
		}
	}
}
