//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

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

func rv64PolymorphicSelectSource(vector bool, body []byte) []byte {
	typ, count := wasm.I32, 256
	if vector {
		typ, count = wasm.V128, 128
	}
	hidden := make([]wasm.ValType, count)
	for i := range hidden {
		hidden[i] = typ
	}
	// The selected arm always traps first. Its remaining bytes test admission,
	// while the other arm returns a payload after each trapped invocation.
	code := append([]byte{0x20, 1, 4, 0x40, 0}, body...)
	code = append(code, 0x0b, 0x20, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType(hidden, hidden))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(code))),
	)
}

func TestRV64PublicReferencePolymorphicSelectSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tc := range []struct {
				name    string
				body    []byte
				witness bool
			}{
				{"nonnull", []byte{0x1b, 0xd4, 0x1a}, true},
				{"chained", []byte{0x1b, 0x1b, 0x1b, 0xd4, 0x1a}, true},
				{"null-branch", []byte{0x1b, 0xd5, 0, 0x1a}, true},
				{"nonnull-branch", []byte{2, 0x70, 0, 0x1b, 0xd6, 0, 0, 0x0b, 0x1a}, true},
				{"ref-prefix", []byte{2, 0x70, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0xd4, 0x0b, 0x1a}, true},
				{"typed-nullable", []byte{0x1b, 0x41, 0, 0x1c, 1, 0x63, 0x70, 0x1a}, false},
				{"typed-nonnull", []byte{0x1b, 0x41, 0, 0x1c, 1, 0x64, 0x70, 0x1a}, true},
				{"indexed-i64-control", []byte{0x1b, 2, 1, 0x0b, 0x1a}, false},
			} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, tc.name), func(t *testing.T) {
					source := rv64PolymorphicSelectSource(vector, tc.body)
					c := rv64NativeTailCompile(t, fmt.Sprintf("polymorphic-select-%t-%s", vector, tc.name), source, artifact)
					if c.nativeReferenceControlABI != tc.witness || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != tc.witness || railshotRequiresNativeOwnerScope(c) != tc.witness || rv64NativeTailModule(c) != nil {
						t.Fatal("polymorphic control witness")
					}
					p, r, err := railshotImportedCallSlots(c, 0, 0)
					if err != nil || p != 0 || r != 0 {
						t.Fatal("polymorphic control acquired call transport", p, r, err)
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if tc.witness && len(in.funcRefDescs) != 40 {
						t.Fatal("control-only anchor extent", len(in.funcRefDescs))
					}
					args, want := rv64ElementGlobalArguments(vector)
					args = append(args, 0xcafebabe00000000)
					for range 3 {
						args[len(args)-1]++
						_, err := in.Invoke("run", args...)
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
							t.Fatal("polymorphic admission fixture trap", err)
						}
						args[len(args)-1]--
						importedGlobalInvoke(t, in, "run", want, args...)
					}
					before := slices.Clone(args)
					if a := testing.AllocsPerRun(10, func() {
						got, err := in.Invoke("run", args...)
						if err != nil || !slices.Equal(got, want) || !slices.Equal(args, before) {
							panic("polymorphic fixture reuse")
						}
					}); a != 0 {
						t.Fatal("warm allocation", a)
					}
				})
			}
		}
	}
}
