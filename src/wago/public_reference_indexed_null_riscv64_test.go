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

// No other typed instruction or annotation can supply the null's witness.
func rv64ReferenceNullSource(vector, dead bool, heap int32) []byte {
	typ, hiddenCount := wasm.I32, 256
	if vector {
		typ, hiddenCount = wasm.V128, 128
	}
	types := make([][]byte, 65)
	for i := range types {
		types[i] = wasmtest.FuncType(nil, nil)
	}
	hidden := make([]wasm.ValType, hiddenCount)
	for i := range hidden {
		hidden[i] = typ
	}
	types[64] = wasmtest.FuncType(hidden, hidden)
	body := []byte{0, 0xd0}
	if !dead {
		types[0] = wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ, wasm.I32})
		// Trap and reuse the same instance, then return the numeric/vector
		// payload and the result of ref.is_null. The i32 ingress is dirty.
		body = []byte{0x20, 1, 4, 0x40, 0, 0x0b, 0x20, 0, 0xd0}
	}
	body = append(body, wasmtest.SLEB32(heap)...)
	if dead {
		body = append(body, 0x1a)
	} else {
		body = append(body, 0xd1)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicReferenceNullSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, dead := range []bool{false, true} {
				for _, heap := range []int32{-16, 0, 64} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-dead%t-heap%d", artifact, vector, dead, heap), func(t *testing.T) {
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-null-%t-%t-%d", vector, dead, heap), rv64ReferenceNullSource(vector, dead, heap), artifact)
						typed := heap >= 0
						if c.nativeReferenceControlABI != typed || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != typed || railshotRequiresNativeOwnerScope(c) != typed || rv64NativeTailModule(c) != nil {
							t.Fatal("standalone null feature/witness")
						}
						p, r, err := railshotImportedCallSlots(c, 0, 0)
						if err != nil || p != 0 || r != 0 {
							t.Fatal("null acquired declared-call transport", p, r, err)
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if typed && len(in.funcRefDescs) != 40 {
							t.Fatal("null-only anchor extent", len(in.funcRefDescs))
						}
						args, want := rv64ElementGlobalArguments(vector)
						args, want = append(args, 0xdeadbeef00000000), append(want, 1)
						pc := uint32(5)
						if dead {
							args, pc = nil, 1
						}
						for range 3 {
							if !dead {
								args[len(args)-1]++
							}
							_, err := in.Invoke("run", args...)
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
								t.Fatal("null fixture cold trap", err)
							}
							if !dead {
								args[len(args)-1]--
								importedGlobalInvoke(t, in, "run", want, args...)
							}
						}
						if !dead {
							before := slices.Clone(args)
							if a := testing.AllocsPerRun(10, func() {
								got, err := in.Invoke("run", args...)
								if err != nil || !slices.Equal(got, want) || !slices.Equal(args, before) {
									panic("warm indexed null")
								}
							}); a != 0 {
								t.Fatal("warm null allocation", a)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceNullAdmission(t *testing.T) {
	source := rv64ReferenceNullSource(false, false, -16)
	c, err := compatibilityDefaultConfig().Compile(source)
	if err != nil {
		t.Fatal("abstract null baseline", err)
	}
	defer c.Close()
	if c.nativeReferenceControlABI || c.nativeReferenceCallABI {
		t.Fatal("abstract null acquired typed witness")
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	importedGlobalInvoke(t, in, "run", []uint64{7, 1}, 0xcafebabe00000007, 0xdeadbeef00000000)
	source = rv64ReferenceNullSource(false, false, 64)
	if got, err := compatibilityDefaultConfig().Compile(source); got != nil || err == nil {
		if got != nil {
			got.Close()
		}
		t.Fatal("indexed null bypassed public feature gate")
	}
	c = rv64NativeTailCompile(t, "reference-null-marker", source, false)
	c.nativeReferenceControlABI = false
	c.validateMemo = &validateMemo{}
	if err := railshotValidateCompiled(c); err == nil {
		t.Fatal("null missing witness admitted")
	}
	blob, err := marshalCompiled(c)
	if err == nil {
		if got, err := LoadTrustedArtifact(blob); err == nil {
			if got != nil {
				got.Close()
			}
			t.Fatal("null artifact missing witness admitted")
		}
	}
}
