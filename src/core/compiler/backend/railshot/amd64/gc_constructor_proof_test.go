//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcConstructorProofModule(t *testing.T, unknown, typed, nonnullable bool, variant string) *wasm.Module {
	t.Helper()
	field := []byte{0x6d} // nullable eqref
	if typed {
		field = []byte{0x63, 0}
	}
	if nonnullable {
		field = []byte{0x64, 0x6d}
	}
	carrier := append([]byte{0x5f, 2, 0x7f, 0}, field...)
	carrier = append(carrier, 0)
	types := wasmtest.Vec(
		[]byte{0x5e, 0x6d, 1}, // array of nullable eqref
		carrier,
		[]byte{0x60, 2, 0x64, 0, 0x7f, 1, 0x6d},
		[]byte{0x60, 2, 0x64, 0, 0x7f, 1, 0x64, 1},
	)
	producer := wasmtest.Code([]byte{
		0x20, 1, 0x20, 0, 0xfb, 15, 0x4f, // index >= array.len
		0x04, 0x6d, 0xd0, 0x6d, 0x05, // if eqref: null, else
		0x20, 0, 0x20, 1, 0xfb, 11, 0,
		0x0b, 0x0b,
	})
	if variant == "unknown-arm" {
		producer = bytes.Replace(producer, []byte{0xd0, 0x6d}, []byte{0x23, 0}, 1)
	}
	wrapper := []byte{1, 1, 0x6d, 0x20, 0, 0x20, 1, 0x10, 0, 0x21, 2,
		0x20, 0, 0xfb, 15, 0x1a, 0x20, 2, 0x0b}
	switch variant {
	case "saved-overwrite":
		wrapper = append(append(append([]byte(nil), wrapper[:len(wrapper)-3]...), 0x23, 0, 0x21, 2), wrapper[len(wrapper)-3:]...)
	case "unsupported-loop":
		wrapper = append(append(append([]byte(nil), wrapper[:len(wrapper)-3]...), 0x03, 0x40, 0x0b), wrapper[len(wrapper)-3:]...)
	}
	consumer := []byte{1, 1, 0x6d, 0x20, 0, 0x20, 1, 0x10, 1, 0x21, 2}
	if unknown {
		consumer = append(consumer, 0x23, 0, 0x21, 2)
	}
	if variant == "caller-join" {
		consumer = append(consumer, 0x02, 0x40, 0x0b)
	}
	consumer = append(consumer, 0x41, 1, 0x20, 2)
	if typed {
		consumer = append(consumer, 0xfb, 23, 0)
	}
	if nonnullable {
		consumer = append(consumer, 0xfb, 22, 0x6d)
	}
	consumer = append(consumer, 0xfb, 0, 1, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6d, 1, 0xd0, 0x6d, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(producer,
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
			append(wasmtest.ULEB(uint32(len(consumer))), consumer...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCConstructorStoredReturnProof(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := nativeGCStructDirectSlotsEnabled
	nativeGCStructDirectSlotsEnabled = true
	defer func() { nativeGCStructDirectSlotsEnabled = saved }()
	var metadata x64.Asm
	metadata.Load32(RAX, R10, gc.NativeHandleMetadataOffset)
	metadata.ShiftImm(5, RAX, 16, false)
	for _, tc := range []struct {
		name                                  string
		variant                               string
		unknown, typed, nonnullable, disabled bool
		want                                  int
	}{
		{name: "saved-return-chain", want: 1},
		{name: "unknown-overwrite", unknown: true},
		{name: "typed-field", typed: true},
		{name: "nonnullable-field", nonnullable: true},
		{name: "facts-disabled", disabled: true},
		{name: "unknown-return-arm", variant: "unknown-arm"},
		{name: "saved-result-overwrite", variant: "saved-overwrite"},
		{name: "unsupported-callee-loop", variant: "unsupported-loop"},
		{name: "caller-join", variant: "caller-join"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(gcConstructorProofModule(t, tc.unknown, tc.typed, tc.nonnullable, tc.variant), CompileOptions{
				GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats,
				Optimizations: map[string]bool{"inline": false, "dead-gc-new": false, "gc-native-alloc": true, "value-facts": !tc.disabled},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			fs := stats.Funcs[2]
			if got := fs.Peephole["gc-native-struct-initializer-proof"]; got != tc.want {
				t.Fatalf("proved initializer checks removed=%d, want %d", got, tc.want)
			}
			body := cm.Code[cm.Entry[2] : cm.Entry[2]+fs.CodeBytes]
			if got := bytes.Contains(body, metadata.B); got != (tc.want == 0) {
				t.Fatalf("constructor initializer metadata load present=%t, want %t", got, tc.want == 0)
			}
		})
	}
}

func TestGCStoredReturnAnalysisFootprint(t *testing.T) {
	m := gcConstructorProofModule(t, false, false, false, "")
	f := fn{m: m}
	check := func() {
		budget := 1024
		if !f.proveStoredGCReturn(1, 0, &budget) {
			panic("saved return not proved")
		}
	}
	check()
	if n := testing.AllocsPerRun(100, check); n != 0 {
		t.Fatalf("proof Go allocations=%v, want0", n)
	}
	fact := gcReferenceTypeFact{flags: gcReferenceFactStored}
	if fact.valid() || fact.provesCast(m, 22, 0, false) {
		t.Fatal("stored-reference validity became an indexed-type proof")
	}
	var facts gcLocalTypeFacts
	facts.set(3, fact)
	got, ok := facts.get(3)
	if !ok || !got.stored() || got.valid() {
		t.Fatal("bounded map lost or widened validity-only proof")
	}
}
