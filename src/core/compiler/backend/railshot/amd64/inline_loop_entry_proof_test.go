//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestInlineLoopEntryProofRejectsUnprovedDefinitions(t *testing.T) {
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}})
	types := []machineType{mtI32, mtI32, mtI32, mtI32}
	args := []byte{0x20, 0, 0x41, 17, 0x41, 1}
	bindings := []byte{0x03, 0, 0x21, 2, 0x21, 1, 0x21, 0}
	join := func(a, b []byte) []byte { return append(append([]byte{}, a...), b...) }
	for _, tc := range []struct {
		name string
		body []byte
		want uint64
	}{
		{"loop-args", join(args, bindings), 6},
		{"early-A", join([]byte{0x20, 0, 0x20, 1, 0x41, 1}, bindings), 4},
		{"early-B", join([]byte{0x20, 0, 0x20, 2, 0x41, 1}, bindings), 2},
		{"write-before-read", join([]byte{0x41, 7, 0x21, 1, 0x20, 0, 0x20, 1, 0x41, 1}, bindings), 6},
		{"copy-before-read", join([]byte{0x20, 0, 0x21, 1, 0x20, 0, 0x20, 1, 0x41, 1}, bindings), 6},
		{"duplicate-destination", join(args, []byte{0x03, 0, 0x21, 1, 0x21, 1, 0x21, 0}), 2},
		{"missing-argument", join(args[2:], bindings), 0},
		{"unknown-type", join(args, []byte{0x03, 1, 0x21, 2, 0x21, 1, 0x21, 0}), 0},
		{"parameterless-loop", join(args, []byte{0x03, 0x40}), 0},
		{"incomplete-bindings", join(args, bindings[:6]), 0},
		{"tee-binding", join(args, []byte{0x03, 0, 0x22, 2, 0x21, 1, 0x21, 0}), 0},
		{"block-first", join([]byte{0x02, 0x40}, join(args, bindings)), 0},
		{"call-first", join([]byte{0x10, 0}, join(args, bindings)), 0},
		{"truncated-get", []byte{0x20, 0x80}, 0},
		{"no-loop", []byte{0x41, 7, 0x21, 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inlineLoopEntryInitializedLocals(m, tc.body, 1, types); got != tc.want {
				t.Fatalf("got=%x want=%x", got, tc.want)
			}
		})
	}
	if got := inlineLoopEntryInitializedLocals(m, join(args, bindings), 1, []machineType{mtI32, mtI64, mtI32, mtI32}); got != 0 {
		t.Fatal("accepted mismatched binding width")
	}
	if got := inlineLoopEntryInitializedLocals(nil, join(args, bindings), 1, types); got != 0 {
		t.Fatal("accepted absent module signature")
	}
	mixed := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64, wasm.I32}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}})
	if got := inlineLoopEntryInitializedLocals(mixed, join([]byte{0x20, 0, 0x42, 0x7f, 0x41, 1}, bindings), 1, []machineType{mtI32, mtI64, mtI32, mtI64}); got != 6 {
		t.Fatalf("mixed widths got=%x want=6", got)
	}
}
