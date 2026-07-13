//go:build arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// mod1 builds a single-function module (func 0 exported "f").
func mod1(t *testing.T, params, results []wasm.ValType, funcBody []byte) *wasm.Module {
	t.Helper()
	entry := append(wasmtest.ULEB(uint32(len(funcBody))), funcBody...)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// TestCompileSmoke exercises the ported arm64 backend's full compile path (hint
// scan → prologue → condense engine → register allocator → control flow →
// epilogue → module layout) under qemu, asserting it produces non-empty AArch64
// code without erroring or panicking. It does not execute the code (that needs
// the real enterNative arm64 trampoline); it verifies the code *generator* runs.
func TestCompileSmoke(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	cases := []struct {
		name string
		in   []wasm.ValType
		out  []wasm.ValType
		body []byte
	}{
		{"add", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}},
		{"mul", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}},
		{"const_add", i32, i32, []byte{0x00, 0x41, 0xe8, 0x07, 0x20, 0x00, 0x6a, 0x0b}},
		{"lt_s", i32x2, i32, []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x48, 0x0b}},
		{"eqz", i32, i32, []byte{0x00, 0x20, 0x00, 0x45, 0x0b}},
		{
			// if (x!=0) 10 else 20
			"if_else", i32, i32,
			[]byte{
				0x01, 0x01, 0x7f, 0x20, 0x00, 0x45,
				0x04, 0x7f, 0x41, 0x14, 0x05, 0x41, 0x0a, 0x0b, 0x0b,
			},
		},
		{
			// iterative fib(n)
			"fib", i32, i32,
			[]byte{
				0x01, 0x03, 0x7f,
				0x41, 0x00, 0x21, 0x01, 0x41, 0x01, 0x21, 0x02, 0x41, 0x00, 0x21, 0x03,
				0x02, 0x40, 0x03, 0x40,
				0x20, 0x03, 0x20, 0x00, 0x4e, 0x0d, 0x01,
				0x20, 0x01, 0x20, 0x02, 0x6a, 0x20, 0x02, 0x21, 0x01, 0x21, 0x02,
				0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03,
				0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.in, tc.out, tc.body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("CompileModule: %v", err)
			}
			if len(cm.Code) == 0 {
				t.Fatal("empty code")
			}
			if len(cm.Code)%4 != 0 {
				t.Errorf("code length %d not a multiple of 4 (A64 words)", len(cm.Code))
			}
		})
	}
}

func TestCompileReleaseBodiesIsOptIn(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x0b})
	if _, err := CompileModuleWith(m, CompileOptions{}); err != nil {
		t.Fatalf("CompileModuleWith default: %v", err)
	}
	if len(m.Code[0].BodyBytes) == 0 {
		t.Fatal("default backend compile released caller-owned body")
	}
	if _, err := CompileModuleWith(m, CompileOptions{ReleaseBodies: true}); err != nil {
		t.Fatalf("CompileModuleWith ReleaseBodies: %v", err)
	}
	if m.Code[0].BodyBytes != nil {
		t.Fatalf("released body = %x, want nil", m.Code[0].BodyBytes)
	}
}

func TestCompileModuleCodeLimitStopsAssembly(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x0b})
	_, err := CompileModuleWith(m, CompileOptions{MaxCodeBytes: 0, HasCodeLimit: true})
	var limitErr *codegen.LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("CompileModuleWith error = %v, want codegen.LimitError", err)
	}
	if limitErr.Resource != "native code" || limitErr.Limit != 0 || limitErr.Used == 0 {
		t.Fatalf("LimitError = %+v", limitErr)
	}
}

func TestModuleCodeCapacityHintBoundsSpeculativeReservation(t *testing.T) {
	m := &wasm.Module{Code: make([]wasm.Func, 131073)}
	if got, want := moduleCodeCapacityHint(m), 8<<20; got != want {
		t.Fatalf("moduleCodeCapacityHint = %d, want capped %d", got, want)
	}
}

func TestTopPinCandidateSelectionKeepsSortOrder(t *testing.T) {
	var store [4]gpPinCandidate
	got := store[:0]
	for _, c := range []gpPinCandidate{
		{idx: 7, score: 1},
		{global: true, idx: 0, score: 9},
		{idx: 4, score: 9},
		{idx: 2, score: 9},
		{idx: 1, score: 2},
	} {
		got = insertTopGPPin(got, c, 3)
	}
	want := []gpPinCandidate{{idx: 2, score: 9}, {idx: 4, score: 9}, {global: true, idx: 0, score: 9}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	scores := []int64{2, 7, 7, 1}
	var floats [2]int
	fs := floats[:0]
	for _, i := range []int{3, 2, 0, 1} {
		fs = insertTopFloatPin(fs, i, scores, 2)
	}
	if got, want := fs, []int{1, 2}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("float candidates = %v, want %v", got, want)
	}
}
