//go:build amd64

package amd64

import (
	"bytes"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcReductionModuleBytes(cast, ordinary bool) []byte {
	types := [][]byte{{0x50, 0, 0x5f, 1, 0x7f, 0}}
	source := byte(0)
	if cast {
		types = append(types, []byte{0x50, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0})
		source = 1
	}
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	body := []byte{1, 2, 0x7f}
	if ordinary {
		body = append(body, 1)
	} // same semantics, outside the complete-loop proof
	body = append(body, 0x03, 0x40, 0x20, 2, 0x20, 1)
	if cast {
		body = append(body, 0x41, 7)
	}
	body = append(body, 0xfb, 0, source)
	if cast {
		body = append(body, 0xfb, 22, 0)
	}
	body = append(body, 0xfb, 2, 0, 0, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(uint32(len(types)-1)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCStructReductionSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name                          string
		cast, ordinary, interruptible bool
		from, to                      []byte
		want                          int
	}{
		{name: "struct", want: 1}, {name: "cast", cast: true, want: 1},
		{name: "prelude", ordinary: true}, {name: "interruptible", interruptible: true},
		{name: "different-step", from: []byte{0x41, 1, 0x6a}, to: []byte{0x41, 2, 0x6a}},
		{name: "signed-bound", from: []byte{0x49, 0x0d}, to: []byte{0x48, 0x0d}},
		{name: "different-field", cast: true, from: []byte{0xfb, 22, 0, 0xfb, 2, 0, 0}, to: []byte{0xfb, 22, 1, 0xfb, 2, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := gcReductionModuleBytes(tc.cast, tc.ordinary)
			if tc.from != nil {
				if !bytes.Contains(data, tc.from) {
					t.Fatal("missing mutation")
				}
				data = bytes.Replace(data, tc.from, tc.to, 1)
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Interruptible: tc.interruptible, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := directPreparedMarked(cm.DirectPreparedBounded, 0); got != (tc.want == 1) {
				t.Fatalf("bounded GC segments=%v, want %v", got, tc.want == 1)
			}
			if tc.want == 1 && directPreparedMarked(cm.DirectPrepared, 0) {
				t.Fatal("GC reduction must not advertise direct register entry")
			}
			if got := stats.Funcs[0].Peephole["gc-struct-induction-reduction"]; got != tc.want {
				t.Fatalf("reductions=%d, want %d", got, tc.want)
			}
		})
	}
}

// The serial and parallel compilers publish the same entry proof. A large
// wrapper still receives the reduction but cannot retain the scheduler.
func TestGCStructReductionBoundedMetadata(t *testing.T) {
	for _, workers := range []int{1, 2} {
		for _, locals := range []uint32{2, 128} {
			m, err := wasm.DecodeModule(gcReductionModuleBytes(false, false))
			if err != nil {
				t.Fatal(err)
			}
			ordinary, err := wasm.DecodeModule(gcReductionModuleBytes(false, true))
			if err != nil {
				t.Fatal(err)
			}
			f := m.Code[0]
			f.Locals.Runs = []wasm.LocalRun{{Count: locals, Type: wasm.I32}}
			m.Code = []wasm.Func{f, ordinary.Code[0], f, ordinary.Code[0]}
			typ := m.FuncTypes[0]
			m.FuncTypes = []wasm.TypeIdx{typ, typ, typ, typ}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Workers: workers})
			if err != nil {
				t.Fatal(err)
			}
			for i := range m.Code {
				want := i%2 == 0 && locals == 2
				if got := directPreparedMarked(cm.DirectPreparedBounded, i); got != want {
					t.Fatalf("workers=%d locals=%d function=%d bounded=%v want=%v", workers, locals, i, got, want)
				}
				if i%2 == 0 && directPreparedMarked(cm.DirectPrepared, i) {
					t.Fatal("GC function admitted direct register entry")
				}
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
		}
	}
}

// A custom recipe can change an imported call's effects; a whole-body proof
// must decline that compilation context before treating source effects as fixed.
func TestGCStructReductionRejectsCustomInstructionContext(t *testing.T) {
	m, err := wasm.DecodeModule(gcReductionModuleBytes(false, false))
	if err != nil {
		t.Fatal(err)
	}
	f := fn{m: m, ft: &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, nParams: 1, localType: []machineType{mtI32, mtI32, mtI32}, gcStructHelpers: true}
	if _, ok := f.gcStructReduction(m.Code[0].BodyBytes); !ok {
		t.Fatal("positive control did not prove reduction")
	}
	f.customInstructions = map[uint32]railcore.CustomInstruction{0: {}}
	if _, ok := f.gcStructReduction(m.Code[0].BodyBytes); ok {
		t.Fatal("custom compilation context admitted reduction")
	}
}
