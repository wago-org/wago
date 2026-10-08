//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceSelectSource(vector, nullable, nullRight bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sig := []wasm.ValType{typ}
	// Real indirect calls distinguish the selected descriptors; the two numeric
	// side effects remain observable even when the returned payloads are equal.
	body := []byte{0x41, 0, 0x24, 0, 0x41, 0, 0xd2, 0, 0xd2, 1}
	if nullRight {
		body = body[:len(body)-2]
		body = append(body, 0xd0, 0x70)
	}
	form := byte(0x64)
	if nullable {
		form = 0x63
	}
	body = append(body, 0x20, 1, 0x1c, 1, form, 0x70, 0x26, 0, 0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, sig))),
		wasmtest.Section(3, []byte{3, 0, 0, 1}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("path", 3, 0))),
		wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 11, 0x24, 0, 0x20, 0, 0x0b}), wasmtest.Code([]byte{0x41, 22, 0x24, 0, 0x20, 0, 0x0b}), wasmtest.Code(body))),
	)
}

func TestRV64PublicReferenceSelectSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, shape := range []string{"nullable", "nonnull", "null-right"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, shape), func(t *testing.T) {
					nullable, nullRight := shape != "nonnull", shape == "null-right"
					c := rv64NativeTailCompile(t, fmt.Sprintf("reference-select-%t-%s", vector, shape), rv64ReferenceSelectSource(vector, nullable, nullRight), artifact)
					if c.nativeReferenceControlABI != !nullable || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != !nullable || rv64NativeTailModule(c) != nil {
						t.Fatal("select-only feature/witness")
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					args, want := rv64ElementGlobalArguments(vector)
					args = append(args, 0)
					for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000, 1} {
						args[len(args)-1] = condition
						got, err := in.Invoke("run", args...)
						path := uint64(22)
						if uint32(condition) != 0 {
							path = 11
						}
						if nullRight && uint32(condition) == 0 {
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
								t.Fatal("selected null descriptor", got, err)
							}
							path = 0
						} else if err != nil || !slices.Equal(got, want) {
							t.Fatal("selected target transport", got, want, err)
						}
						if got, err := in.Global("path"); err != nil || got != path {
							t.Fatal("selected descriptor side effect", got, path, err)
						}
					}
					if a := testing.AllocsPerRun(10, func() {
						got, err := in.Invoke("run", args...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm reference select")
						}
					}); a != 0 {
						t.Fatal("warm select allocation", a)
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceSelectDeadWitness(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, nullable := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-nullable%t", artifact, nullable), func(t *testing.T) {
				form := byte(0x64)
				if nullable {
					form = 0x63
				}
				body := []byte{0, 0x1c, 1, form, 0x70, 0x1a, 0x0b}
				hidden := make([]wasm.ValType, 256)
				for i := range hidden {
					hidden[i] = wasm.I32
				}
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(hidden, hidden))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-select-dead-%t", nullable), source, artifact)
				if c.nativeReferenceControlABI != !nullable || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != !nullable || railshotRequiresNativeOwnerScope(c) != !nullable {
					t.Fatal("dead select witness")
				}
				p, r, err := railshotImportedCallSlots(c, 0, 0)
				if err != nil || p != 0 || r != 0 {
					t.Fatal("select acquired declared-call transport", p, r, err)
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if !nullable && len(in.funcRefDescs) != 40 {
					t.Fatal("select-only anchor extent", len(in.funcRefDescs))
				}
				for range 3 {
					_, err = in.Invoke("run")
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 1 {
						t.Fatal("dead select source trap", err)
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceSelectAdmission(t *testing.T) {
	source := rv64ReferenceSelectSource(false, true, false)
	c, err := compatibilityDefaultConfig().Compile(source)
	if err != nil {
		t.Fatal("nullable select baseline", err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	importedGlobalInvoke(t, in, "run", []uint64{7}, 0xcafebabe00000007, 0xdeadbeef00000001)
	if c.nativeReferenceControlABI || c.nativeReferenceCallABI {
		t.Fatal("nullable select acquired typed witness")
	}
	source = rv64ReferenceSelectSource(false, false, false)
	if got, err := compatibilityDefaultConfig().Compile(source); got != nil || err == nil {
		if got != nil {
			got.Close()
		}
		t.Fatal("non-null select bypassed public feature gate")
	}
	c = rv64NativeTailCompile(t, "reference-select-marker", source, false)
	c.nativeReferenceControlABI = false
	c.validateMemo = &validateMemo{}
	if err := railshotValidateCompiled(c); err == nil {
		t.Fatal("select missing witness admitted")
	}
	blob, err := marshalCompiled(c)
	if err == nil {
		if got, err := LoadTrustedArtifact(blob); err == nil {
			if got != nil {
				got.Close()
			}
			t.Fatal("select artifact missing witness admitted")
		}
	}
}
