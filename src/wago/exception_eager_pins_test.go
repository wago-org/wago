//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The callee pins and overwrites its f64 parameter before throwing. The caller
// catches and reads its original parameter. With selector zero, the caller
// instead updates its own local before throw/throw_ref; eager catch reloads must
// use that latest value, not the entry or pre-call slot.
func exceptionEagerPinsModule(throwRef bool) []byte {
	void := wasmtest.FuncType(nil, nil)
	calleeSig := wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil)
	callerSig := wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64})
	exnSig := []byte{0x60, 0, 1, 0x63, 0x69}                // () -> (ref null exn)
	callee := []byte{0x20, 0, 0x9a, 0x21, 0, 0x08, 0, 0x0b} // negate local; throw tag 0
	body := []byte{
		0x02, 0x40, // block $caught
		0x1f, 0x40, 1, byte(wasm.CatchAll), 0, // try_table (catch_all $caught)
		0x20, 1, 0x04, 0x40, // if selector
		0x44, // f64.const 99
	}
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(99))
	body = append(body, 0x10, 0, 0x05) // call callee; else
	if throwRef {
		body = append(body,
			0x02, 3, // block (result exnref)
			0x1f, 0x40, 1, byte(wasm.CatchAllRef), 0,
			0x08, 0, 0x0b, 0x00, 0x0b, // throw; end try; unreachable; end block
		)
	}
	body = append(body, 0x20, 0, 0x9a, 0x21, 0) // local = -local
	if throwRef {
		body = append(body, 0x0a) // throw_ref
	} else {
		body = append(body, 0x08, 0) // throw tag 0
	}
	body = append(body, 0x0b, 0x0b, 0x0b, 0x20, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(void, calleeSig, callerSig, exnSig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(body))),
	)
}

func TestExceptionEagerPinnedLocals(t *testing.T) {
	for _, throwRef := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			// ARM64 exercises both spill policies; the default AMD64 policy is
			// an additional execution reference for the same validated module.
			if runtime.GOARCH == "amd64" && !stackReg {
				continue
			}
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("throw-ref=%v/stack-reg=%v/reg-abi=%v", throwRef, stackReg, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
						WithOptimization("stack-reg", stackReg).
						WithOptimization("reg-abi", regABI).
						WithOptimization("inline", false)
					compiled, err := Compile(cfg, exceptionEagerPinsModule(throwRef))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					in, err := Instantiate(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for _, input := range []float64{1.25, -2.5, 0} {
						for _, selector := range []uint64{0, 1} {
							want := math.Float64bits(input)
							if selector == 0 {
								want ^= 1 << 63
							}
							got, err := in.Invoke("run", math.Float64bits(input), selector)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("input=%v selector=%d: got %x, %v; want %x", input, selector, got, err, want)
							}
						}
					}
				})
			}
		}
	}
}

func BenchmarkExceptionEagerPinsCompile(b *testing.B) {
	data := exceptionEagerPinsModule(false)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	for range b.N {
		compiled, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		compiled.Close()
	}
}

// A pin-preserving integer-only leaf need not clobber any register to expose the
// bug: its exceptional return still needs the caller's latest canonical homes.
func exceptionPinPreservingLeafModule() []byte {
	body := []byte{
		0x02, 0x40,
		0x1f, 0x40, 1, byte(wasm.CatchAll), 0,
		0x20, 0, 0x9a, 0x21, 0, // change local after try entry
		0x10, 0, // call void throwing leaf
		0x0b, 0x0b, 0x20, 0, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0, 0x0b}), wasmtest.Code(body))),
	)
}

func TestExceptionPinPreservingLeafLocals(t *testing.T) {
	for _, stackReg := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			// AMD64's wrapper ABI is an additional execution reference; the
			// pin-preserving register-call regression is ARM64-specific here.
			if runtime.GOARCH == "amd64" && (!stackReg || regABI) {
				continue
			}
			t.Run(fmt.Sprintf("stack-reg=%v/reg-abi=%v", stackReg, regABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
					WithOptimization("stack-reg", stackReg).
					WithOptimization("reg-abi", regABI).
					WithOptimization("inline", false)
				compiled, err := Compile(cfg, exceptionPinPreservingLeafModule())
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, input := range []float64{1.25, -2.5, 0} {
					want := math.Float64bits(input) ^ (1 << 63)
					got, err := in.Invoke("run", math.Float64bits(input))
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("input=%v: got %x, %v; want %x", input, got, err, want)
					}
				}
			})
		}
	}
}
