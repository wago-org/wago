//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func storedRefShapeModule(t *testing.T) (*wasm.Module, *wasm.ValidatedModuleAnalysis) {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x00},       // open child
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x00}, // parent(ref null child)
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 0x00, 0x00, // child()
			0xfb, 0x00, 0x01, // parent(child)
			0xd1, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	a := new(wasm.ValidatedModuleAnalysis)
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
		t.Fatal(err)
	}
	return m, a
}

func TestStoredRefProofScope(t *testing.T) {
	oldElision, oldInline, oldDirect := gcStoredRefElisionEnabled, nativeGCStructInlineEnabled, nativeGCStructDirectSlotsEnabled
	defer func() {
		gcStoredRefElisionEnabled, nativeGCStructInlineEnabled, nativeGCStructDirectSlotsEnabled = oldElision, oldInline, oldDirect
	}()
	for _, route := range []string{"direct", "inline", "shared"} {
		nativeGCStructDirectSlotsEnabled = route == "direct"
		nativeGCStructInlineEnabled = route != "shared"
		for _, target := range []string{"final", "open", "eq", "any"} {
			for _, nullable := range []bool{false, true} {
				for _, origin := range []string{"new", "default", "null", "i31", "nop", "local", "global", "load", "call"} {
					if origin == "null" && !nullable || origin == "i31" && target != "eq" && target != "any" {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/null%v/%s", route, target, nullable, origin), func(t *testing.T) {
						root := []byte{0x5f, 0x00}
						if target == "open" {
							root = append([]byte{0x50, 0}, root...)
						}
						ref := byte(0x64)
						if nullable {
							ref = 0x63
						}
						field := []byte{ref, 0, 0}
						if target == "eq" {
							field = []byte{ref, 0x6d, 0}
						}
						if target == "any" {
							field = []byte{ref, 0x6e, 0}
						}
						parent := append([]byte{0x5f, 1}, field...)
						ops := []byte{0xfb, 0, 0}
						switch origin {
						case "default":
							ops = []byte{0xfb, 1, 0}
						case "null":
							ops = []byte{0xd0, 0}
						case "i31":
							ops = []byte{0x41, 7, 0xfb, 28}
						case "global":
							ops = []byte{0x23, 0}
						case "load":
							ops = append(ops, 0x01, 0xfb, 0, 3, 0xfb, 2, 3, 0)
						case "call":
							ops = []byte{0x10, 1}
						case "nop":
							ops = append(ops, 1)
						case "local":
							ops = append(ops, 0x21, 0, 0x20, 0)
							if !nullable {
								ops = append(ops, 0xd4)
							}
						}
						ops = append(ops, 0xfb, 0, 1, 0xd1, 0x0b)
						body := append([]byte{1, 1, 0x63, 0}, ops...)
						data := wasmtest.Module(
							wasmtest.Section(1, wasmtest.Vec(root, parent, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 1, 0x64, 0, 0}, []byte{0x60, 0, 1, 0x64, 0})),
							wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{4})),
							wasmtest.Section(6, wasmtest.Vec([]byte{0x64, 0, 0, 0xfb, 1, 0, 0x0b})),
							wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0xfb, 1, 0, 0x0b}))),
						)
						m, err := wasm.DecodeModule(data)
						if err != nil {
							t.Fatal(err)
						}
						a := new(wasm.ValidatedModuleAnalysis)
						if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, a); err != nil {
							t.Fatal(err)
						}
						compile := func(enabled bool, validation string) []byte {
							gcStoredRefElisionEnabled = enabled
							opts := CompileOptions{GCStructHelpers: true, DeferCodeMapping: true, Optimizations: map[string]bool{"dead-gc-new": false}}
							var analysis *wasm.ValidatedModuleAnalysis
							switch validation {
							case "valid":
								analysis = a
							case "foreign":
								_, analysis = storedRefShapeModule(t)
							case "reset":
								copy := *a
								analysis = &copy
								copy = wasm.ValidatedModuleAnalysis{}
							}
							cm, err := CompileValidatedModuleWith(m, opts, analysis)
							if err != nil {
								t.Fatal(err)
							}
							if cm.CodeImage != nil {
								defer cm.CodeImage.Close()
							}
							return bytes.Clone(cm.Code)
						}
						checked := compile(false, "valid")
						got := compile(true, "valid")
						wantProof := origin == "new" || origin == "default" || origin == "null" || origin == "i31"
						if wantProof && len(got) >= len(checked) {
							t.Fatalf("proved %s: %d >= checked %d", origin, len(got), len(checked))
						}
						if !wantProof && !bytes.Equal(got, checked) {
							t.Fatal("proof crossed an instruction or local")
						}
						for _, validation := range []string{"absent", "foreign", "reset"} {
							if unvalidated := compile(true, validation); !bytes.Equal(unvalidated, checked) {
								t.Fatalf("%s validation elided checks", validation)
							}
						}
					})
				}
			}
		}
	}
}

func TestStoredRefCodeShape(t *testing.T) {
	if path := os.Getenv("WAGO_TEST_STORED_REF_DUMP"); path != "" {
		m, a := storedRefShapeModule(t)
		cm, err := CompileValidatedModuleWith(m, CompileOptions{GCStructHelpers: true, DeferCodeMapping: true,
			Optimizations: map[string]bool{"dead-gc-new": false},
		}, a)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if err := os.WriteFile(path, cm.Code, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	var dumps [3][]byte
	for i, off := range []string{"default", "0", "1"} {
		path := filepath.Join(dir, off+".bin")
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoredRefCodeShape$", "-test.v")
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "WAGO_AMD64_NO_GC_STORED_REF_ELISION=") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		cmd.Env = append(cmd.Env, "WAGO_TEST_STORED_REF_DUMP="+path)
		if off != "default" {
			cmd.Env = append(cmd.Env, "WAGO_AMD64_NO_GC_STORED_REF_ELISION="+off)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shape child: %v\n%s", err, out)
		}
		var err error
		dumps[i], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if keep := os.Getenv("WAGO_TEST_STORED_REF_EVIDENCE"); keep != "" {
			if err := os.WriteFile(filepath.Join(keep, "switch-"+off+".bin"), dumps[i], 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("default bytes=%d, checked bytes=%d", len(dumps[0]), len(dumps[2]))
	if !bytes.Equal(dumps[0], dumps[1]) {
		t.Fatal("unset and zero switch states differ")
	}
	if bytes.Equal(dumps[0], dumps[2]) || len(dumps[0]) >= len(dumps[2]) {
		t.Fatal("validated constructor retains redundant stored-reference checks")
	}
}

// Thirty-two functions and four requested workers enter compileModuleParallel.
// Every function has a proved constructor so losing the entry argument in either
// path changes observable code. SourceContext alone must never enable elision.
func TestStoredRefValidatedEntry(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previousProcs)
	old := gcStoredRefElisionEnabled
	defer func() { gcStoredRefElisionEnabled = old }()
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers%d", workers), func(t *testing.T) {
			m, _ := storedRefShapeModule(t)
			for len(m.Code) < 32 {
				m.Code = append(m.Code, m.Code[0])
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
			}
			a := new(wasm.ValidatedModuleAnalysis)
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, workers, wasm.ValidationLimits{}, a); err != nil {
				t.Fatal(err)
			}
			_, foreign := storedRefShapeModule(t)
			reset := *a
			reset = wasm.ValidatedModuleAnalysis{}
			compile := func(analysis *wasm.ValidatedModuleAnalysis, lowLevel bool) []byte {
				opts := CompileOptions{Workers: workers, GCStructHelpers: true, DeferCodeMapping: true,
					Optimizations: map[string]bool{"dead-gc-new": false},
					Codegen:       codegen.SourceOptions(codegen.Options{}, m, a, wasm.ValidationFeatures{}),
				}
				defer codegen.CloseSourceContext(opts.Codegen, m)
				entry := CompileValidatedModuleWith
				if lowLevel {
					entry = func(m *wasm.Module, opts CompileOptions, _ *wasm.ValidatedModuleAnalysis) (*encoder.CompiledModule, error) {
						return CompileModuleWith(m, opts)
					}
				}
				cm, err := entry(m, opts, analysis)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				return bytes.Clone(cm.Code)
			}
			gcStoredRefElisionEnabled = false
			checked := compile(a, false)
			gcStoredRefElisionEnabled = true
			proved := compile(a, false)
			if len(proved) >= len(checked) {
				t.Fatalf("validated entry ignored: %d >= %d", len(proved), len(checked))
			}
			for name, analysis := range map[string]*wasm.ValidatedModuleAnalysis{"absent": nil, "foreign": foreign, "reset": &reset} {
				if !bytes.Equal(compile(analysis, false), checked) {
					t.Fatalf("%s analysis elided checks", name)
				}
			}
			if !bytes.Equal(compile(nil, true), checked) {
				t.Fatal("low-level SourceContext elided checks")
			}
		})
	}
}
