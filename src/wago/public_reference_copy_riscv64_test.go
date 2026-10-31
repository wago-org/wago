//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceCopyModule(t *testing.T, vector, borrowed, imported, null bool, count int, exported bool) []byte {
	t.Helper()
	source := rv64ReferenceGlobalModule(vector, borrowed, imported, null, count)
	globals := make([][]byte, count)
	for i := range globals {
		expr := []byte{0xd2, 0, 0x0b}
		if i == 0 && null {
			expr = []byte{0xd0, 0x70, 0x0b}
		}
		if i > 0 {
			expr = append(append([]byte{0x23}, wasmtest.ULEB(uint32(i-1))...), 0x0b)
		}
		globals[i] = wasmtest.GlobalEntry(wasm.FuncRef, false, expr)
	}
	source = rv64ReplaceTableStageSection(t, source, 6, wasmtest.Section(6, wasmtest.Vec(globals...)))
	if exported {
		source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("install", 0, 1), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("isnull", 0, 3), wasmtest.ExportEntry("first", 3, 0), wasmtest.ExportEntry("last", 3, uint32(count-1)))))
	}
	return source
}

func TestRV64PublicReferenceCopies(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-null%t-artifact%t", vector, null, artifact), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-copy-vector%t-null%t", vector, null), rv64ReferenceCopyModule(t, vector, false, false, null, 256, true), artifact)
					if !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) || !c.Globals[255].HasInitGlobal || c.Globals[255].InitGlobal != 254 {
						t.Fatal("compact chain metadata")
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					c.Close()
					first, err := in.GlobalValue("first")
					if err != nil {
						t.Fatal(err)
					}
					last, err := in.GlobalValue("last")
					if err != nil || first != last || last.FuncRef().IsNull() != null {
						t.Fatal("copied token identity", first, last, err)
					}
					a, err := in.ExportedGlobalObject("first")
					if err != nil {
						t.Fatal(err)
					}
					b, err := in.ExportedGlobalObject("last")
					if err != nil {
						t.Fatal(err)
					}
					if a == b || &a.cell[0] == &b.cell[0] {
						t.Fatal("copied globals share a cell")
					}
					bits := readGlobalObject(a, ValFuncRef)
					if !null {
						writeGlobalObject(a, ValFuncRef, 0)
						if readGlobalObject(b, ValFuncRef) != bits || bits <= 0xffffffff {
							t.Fatal("copy lost full-width descriptor or aliases source")
						}
						writeGlobalObject(a, ValFuncRef, bits)
						if !in.FuncRefMatchesFunction(last.FuncRef(), 0) {
							t.Fatal("copied function identity")
						}
					}
					runtime.GC()
					if _, err := in.Invoke("install", 0); err != nil {
						t.Fatal(err)
					}
					args, want := []uint64{0xfeedface00000007, 0}, []uint64{7}
					if vector {
						args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
						want = args[:2]
					}
					if null {
						_, err := in.Invoke("call", args...)
						tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
					} else {
						importedGlobalInvoke(t, in, "call", want, args...)
					}
					in.Close()
					if in.hasPhysicalResources() || c.codeCache.mem != nil {
						t.Fatal("copied reference mapping retained")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceCopyRetiredWriter(t *testing.T) {
	testRV64PublicReferenceGlobalRetiredWriter(t, true)
}
