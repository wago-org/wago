//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Table-free ref.func requires a declarative segment. Type 2 duplicates type 0,
// proving that the tail's declared type and function index are distinct spaces.
func rv64PublicReferenceTailModule(types []wasm.ValType, imported bool, ordinary ...bool) []byte {
	opcode := byte(0x15)
	if len(ordinary) != 0 && ordinary[0] {
		opcode = 0x14
	}
	callerTypes := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, types), wasmtest.FuncType(types, types), wasmtest.FuncType(nil, types)))}
	var args []byte
	for i := range types {
		args = append(args, 0x20, byte(i))
	}
	var codes [][]byte
	callIndex, identityIndex := uint32(1), uint32(0)
	if imported {
		im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{2, 0, 1}))
		callIndex, identityIndex = 2, 1
		codes = append(codes, wasmtest.Code(append(slices.Clone(args), 0x0b)))
	} else {
		sections = append(sections, wasmtest.Section(3, []byte{2, 0, 1}))
		codes = append(codes, wasmtest.Code(append(slices.Clone(args), 0x0b)))
	}
	body := []byte{0x20, byte(len(types)), 0x04, 3}
	body = append(body, args...)
	body = append(body, 0xd0, 2, opcode, 2, 0x05)
	body = append(body, args...)
	body = append(body, 0xd2, 0, opcode, 2, 0x0b, 0x0b)
	codes = append(codes, wasmtest.Code(body))
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, callIndex), wasmtest.ExportEntry("identity", 0, identityIndex))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceTailSourceArtifact(t *testing.T) {
	rv64PublicReferenceSourceArtifact(t, false)
}
func TestRV64PublicReferenceCallSourceArtifact(t *testing.T) {
	rv64PublicReferenceSourceArtifact(t, true)
}
func rv64PublicReferenceSourceArtifact(t *testing.T, ordinary bool) {
	for _, artifact := range []bool{false, true} {
		for _, shape := range []string{"zero", "mixed", "vector"} {
			for _, imported := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-imported%t", artifact, shape, imported), func(t *testing.T) {
					var types []wasm.ValType
					var input, want []uint64
					if shape != "zero" {
						types = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
						input = []uint64{0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
						want = []uint64{0x80000001, 0x7f800123, input[2], input[3]}
					}
					if shape == "vector" {
						types = append(types, wasm.V128)
						input = append(input, 0xaabbccddeeff0011, 0xfedcba9876543210)
						want = append(want, input[4:]...)
					}
					key := fmt.Sprintf("reference-call-%s-imported%t-ordinary%t", shape, imported, ordinary)
					var imports *Imports
					var producer *Instance
					if imported {
						pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicNativeIdentityModule(types), artifact)
						var err error
						producer, err = Instantiate(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						export, err := producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						imports = NewImports().Function("env", "target", export)
					}
					c := rv64NativeTailCompile(t, key, rv64PublicReferenceTailModule(types, imported, ordinary), artifact)
					m := rv64NativeTailModule(c)
					if c.tableCount() != 0 || !c.needsFuncRefDescs() || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.stagedFeatures().IsEnabled(CoreFeatureTypedFunctionReferences) {
						t.Fatal("table-free reference tail metadata")
					}
					if ordinary {
						if m != nil || !c.nativeReferenceCallABI {
							t.Fatal("ordinary reference lost distinct execution marker")
						}
					} else {
						if m == nil || len(m.Plans) != 2 || c.nativeReferenceCallABI {
							t.Fatal("tail metadata changed")
						}
						for _, p := range m.Plans {
							if p.Kind != encoder.NativeTailReference || p.TypeIndex != 2 || p.ReferenceBase == 0 {
								t.Fatal("reference plan", p)
							}
						}
					}
					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if producer != nil {
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
						if err := producer.c.Close(); err != nil {
							t.Fatal(err)
						}
					}
					for _, selector := range []uint64{0, 1, 0} {
						got, err := in.Invoke("call", append(slices.Clone(input), selector)...)
						if selector != 0 {
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
								t.Fatal("typed null reference trap", got, err)
							}
						} else if err != nil || !slices.Equal(got, want) {
							t.Fatal("reference tail transport", got, want, err)
						}
						if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
							t.Fatal("cold reference tail retained generation")
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceTailAdmission(t *testing.T) {
	source := rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false)
	if c, err := compatibilityDefaultConfig().Compile(source); c != nil || err == nil {
		t.Fatal("default typed tail admission opened", err)
	}
	base := rv64NativeTailCompile(t, "reference-tail-gates", source, false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"missing-typed-feature", func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTypedFunctionReferences }},
		{"missing-descriptors", func(c *Compiled) { c.NeedsFuncRefDescs = false }},
		{"unqualified-ref-test", func(c *Compiled) { c.codeCache.flags |= compiledCacheDynamicFuncRefTest }},
		{"type-at-count", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].TypeIndex = uint32(len(c.Types)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: slices.Clone(rv64NativeTailModule(base).Plans)})
			tc.mutate(c)
			cache := c.loadCodeCache()
			sealed, refs := cache.sealed, cache.refs
			t.Cleanup(func() { _ = c.Close() })
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("invalid metadata accepted", err)
			}
			if in, err := Instantiate(c); in != nil || !errors.Is(err, railshot.ErrUnsupported) || cache.sealed != sealed || cache.refs != refs {
				t.Fatal("invalid metadata reached executable publication", err)
			}
		})
	}
	imported := rv64NativeTailCompile(t, "reference-tail-host-gate", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true), false)
	if in, err := Instantiate(imported, NewImports().Function("env", "target", HostCallFunc(func(HostCall) {}))); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("unqualified host reference published", err)
	}
}

func TestRV64PublicReferenceTailCodecAdmission(t *testing.T) {
	base := rv64NativeTailCompile(t, "reference-tail-codec-gates", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false), false)
	w := compiledWriter{}
	if err := railshotWriteNativeTail(&w, base); err != nil {
		t.Fatal(err)
	}
	view := rv64TailCodecView(base)
	view.requiredFeatures &^= CoreFeatureTypedFunctionReferences
	r := compiledReader{data: w.buf, budget: newArtifactDecodeBudget(0)}
	if err := railshotReadNativeTail(&r, view); err == nil {
		t.Fatal("decoder accepted Kind2 without typed feature")
	}
	if view.stagedFeatures().IsEnabled(CoreFeatureTypedFunctionReferences) {
		t.Fatal("decoder published typed staging for invalid metadata")
	}
	// Produce exact metadata with a serialized execution marker, without using
	// public MarshalBinary's validation; Load must reject before executable sealing.
	c := cloneCompiledMetadata(base)
	c.validateMemo, c.codeCache = &validateMemo{}, nil
	rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: slices.Clone(rv64NativeTailModule(base).Plans)})
	c.codeCache.flags |= compiledCacheDynamicFuncRefTest
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustedArtifact(blob); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("loader accepted unqualified ref.test execution marker", err)
	}
	// Typed feature metadata alone never authorizes arbitrary typed operations.
	plain := rv64NativeTailCompile(t, "reference-tail-no-plan", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	v := cloneCompiledMetadata(plain)
	v.validateMemo, v.codeCache = &validateMemo{}, nil
	v.requiredFeatures |= CoreFeatureTypedFunctionReferences
	if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("typed-only metadata without Kind2 accepted", err)
	}
	blob, err = marshalCompiled(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustedArtifact(blob); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("loader accepted typed feature without Kind2", err)
	}
}

func TestRV64PublicReferenceTailWholeModuleGate(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0xd0, 0, 0x15, 0, 0x0b}))))
	if c, err := rv64NativeTailConfig().Compile(source); c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("dead-only typed tail module admitted without a qualified tail plan", err)
	}
}
