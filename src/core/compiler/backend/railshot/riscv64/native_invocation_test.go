//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestProductionInvocationViewsRequireCooperativeABI(t *testing.T) {
	for _, source := range [][]byte{
		wasmtest.Module(),
		binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}),
		binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		c, err := CompileModuleWith(m, CompileOptions{InvocationContext: true, MemoryPressure: func() { pressure++ }})
		if c != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("noncooperative view reached staging", err, pressure)
		}
		if c, err := compileOptions(source, scalarRegisters[:], CompileOptions{InvocationContext: true}); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private ABI accepted public view", err)
		}
		if len(m.Code) == 0 {
			continue
		}
		plain, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		viewed, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true})
		if err != nil || bytes.Equal(plain.Code, viewed.Code) {
			t.Fatal("production lost view prologue", err)
		}
	}
}
