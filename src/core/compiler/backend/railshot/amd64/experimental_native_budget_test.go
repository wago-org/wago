//go:build linux && amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestExperimentalNativeBudgetFallback(t *testing.T) {
	m := loopExperimentModule(t, "map-i32")
	const functions = 1500
	f := m.Code[0]
	ft := m.FuncTypes[0]
	m.Code = make([]wasm.Func, functions)
	m.FuncTypes = make([]wasm.TypeIdx, functions)
	for i := range m.Code {
		m.Code[i], m.FuncTypes[i] = f, ft
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	base, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if base.CodeImage != nil {
		defer base.CodeImage.Close()
	}
	if len(base.Code) <= shared.ExperimentMaxNativeModuleBytes {
		t.Fatal("fixture does not reach native module budget", len(base.Code))
	}
	for _, workers := range []int{1, 2} {
		got, err := CompileModuleWith(m, CompileOptions{Workers: workers, ExperimentalLoopMode: "count2"})
		if err != nil {
			t.Fatal(err)
		}
		if got.CodeImage != nil {
			defer got.CodeImage.Close()
		}
		if !bytes.Equal(base.Code, got.Code) {
			t.Fatal("native budget must restore source compilation", workers)
		}
	}
}

func TestExperimentalNativeFunctionBudgetFallback(t *testing.T) {
	m := loopExperimentModule(t, "map-i32")
	body := m.Code[0].BodyBytes
	suffix := append([]byte(nil), body[:len(body)-1]...)
	m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "sink", Type: wasm.NewFuncExternType(m.FuncTypes[0])})
	m.Exports = nil
	for i := 0; i < 100; i++ {
		for j := 0; j < 5; j++ {
			suffix = append(suffix, 0x41, 0)
		}
		suffix = append(suffix, 0x10, 0, 0x1a, 0x1a, 0x1a, 0x1a)
	}
	m.Code[0].BodyBytes = append(suffix, 0x0b)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if _, why, err := shared.RewriteReplication(m, "count2"); err != nil || why != "accepted" {
		t.Fatal("fixture must reach native budget", why, err)
	}
	base, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if base.CodeImage != nil {
		defer base.CodeImage.Close()
	}
	if len(base.Code) <= shared.ExperimentMaxNativeFunctionBytes {
		t.Fatal("fixture below native function budget", len(base.Code))
	}
	for _, workers := range []int{1, 2} {
		got, err := CompileModuleWith(m, CompileOptions{Workers: workers, ExperimentalLoopMode: "count2"})
		if err != nil {
			t.Fatal(err)
		}
		if got.CodeImage != nil {
			defer got.CodeImage.Close()
		}
		if !bytes.Equal(base.Code, got.Code) {
			t.Fatal("function budget must restore source compilation", workers)
		}
	}
}
