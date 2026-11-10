//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAdjacentSetGetTeeFold(t *testing.T) {
	saved := setGetTeeFoldEnabled
	setGetTeeFoldEnabled = true
	defer func() { setGetTeeFoldEnabled = saved }()

	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		body []byte
		arg  uint64
		want uint64
		hits int
	}{
		{"i32", wasm.I32, []byte{0, 0x20, 0, 0x21, 0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}, 42, 43, 1},
		{"i64", wasm.I64, []byte{0, 0x20, 0, 0x21, 0, 0x20, 0, 0x42, 1, 0x7c, 0x0b}, 42, 43, 0},
		{"different-local", wasm.I32, []byte{0x01, 0x01, 0x7f, 0x20, 0, 0x21, 0, 0x20, 1, 0x0b}, 42, 0, 0},
		{"intervening-op", wasm.I32, []byte{0, 0x20, 0, 0x21, 0, 0x41, 1, 0x1a, 0x20, 0, 0x0b}, 42, 42, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}, tc.body)
			if got := runAmd64u(t, m, tc.arg); got != tc.want {
				t.Fatalf("result = %d, want %d", got, tc.want)
			}
			stats := compileWithStats(t, m, false)
			if got := stats.Funcs[0].Peephole["local-set-get-tee"]; got != tc.hits {
				t.Fatalf("folds = %d, want %d", got, tc.hits)
			}
		})
	}
	setGetTeeFoldEnabled = false
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0, 0x20, 0, 0x21, 0, 0x20, 0, 0x0b})
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["local-set-get-tee"]; got != 0 {
		t.Fatalf("disabled fold count = %d, want 0", got)
	}
}

func TestAdjacentSetGetTeeFoldAcrossInlineLocalBase(t *testing.T) {
	saved := setGetTeeFoldEnabled
	setGetTeeFoldEnabled = true
	defer func() { setGetTeeFoldEnabled = saved }()

	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x10, 1, 0x0b}},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x21, 0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}},
	)
	if got := runAmd64u(t, m, 42); got != 43 {
		t.Fatalf("result = %d, want 43", got)
	}
	stats := compileWithStats(t, m, false)
	if stats.Funcs[0].Peephole["all-calls-inlined"] != 1 ||
		stats.Funcs[0].Peephole["local-set-get-tee"] != 1 {
		t.Fatalf("inlined caller did not fold set/get: %v", stats.Funcs[0].Peephole)
	}
}
