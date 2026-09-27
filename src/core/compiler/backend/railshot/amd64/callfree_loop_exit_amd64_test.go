//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func callFreeLoopExitModuleAMD64(t testing.TB) *wasm.Module {
	t.Helper()
	i32 := []wasm.ValType{wasm.I32}
	return modFuncs(t,
		funcDef{i32, i32, []byte{
			0x01, 0x01, 0x7f,
			0x20, 0x00, 0x21, 0x01,
			0x02, 0x40,
			0x03, 0x40,
			0x20, 0x01, 0x45, 0x0d, 0x01,
			0x20, 0x01, 0x41, 0x01, 0x6b, 0x21, 0x01,
			0x0c, 0x00,
			0x0b, 0x0b,
			0x20, 0x00, 0x10, 0x01,
			0x0b,
		}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func TestCallFreeLoopExitReconciliationIsColdAMD64(t *testing.T) {
	m := callFreeLoopExitModuleAMD64(t)
	compile := func(enabled bool) *ModuleStats {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Stats: &stats,
			Optimizations: map[string]bool{
				"inline":                  false,
				"callfree-loop-cold-exit": enabled,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		return &stats
	}
	on, off := compile(true), compile(false)
	if got := on.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 1 {
		t.Fatalf("cold loop exits = %d, want 1 (all: %v)", got, on.Funcs[0].Peephole)
	}
	if got := off.Funcs[0].Peephole["callfree-loop-exit-cold"]; got != 0 {
		t.Fatalf("disabled cold loop exits = %d, want 0", got)
	}
	for _, n := range []uint64{0, 1, 2, 17, 255} {
		cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		got := runCompiledAmd64u(t, cm, n)
		cm.CodeImage.Close()
		if got != n {
			t.Fatalf("f(%d) = %d, want %d", n, got, n)
		}
	}
}

func TestCallFreeLoopKeepsPinnedLocalsOnBackedgeAMD64(t *testing.T) {
	m := callFreeLoopExitModuleAMD64(t)
	compile := func(enabled bool) (*ModuleStats, *encoderamd64.CompiledModule) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Stats: &stats,
			Optimizations: map[string]bool{
				"inline":                  false,
				"callfree-loop-reg-state": enabled,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return &stats, cm
	}
	_, offCode := compile(false)
	defer offCode.CodeImage.Close()
	on, onCode := compile(true)
	defer onCode.CodeImage.Close()
	if got := on.Funcs[0].Peephole["callfree-loop-reg-state"]; got != 1 {
		t.Fatalf("register loop state = %d, want 1", got)
	}
	for _, n := range []uint64{0, 1, 2, 17, 255} {
		for _, tc := range []struct {
			name string
			code *encoderamd64.CompiledModule
		}{{"off", offCode}, {"on", onCode}} {
			if got := runCompiledAmd64u(t, tc.code, n); got != n {
				t.Fatalf("%s f(%d) = %d, want %d", tc.name, n, got, n)
			}
		}
	}
}

func TestScanCallFreeLoopTracksOnlyWrittenPinsAMD64(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(nil, false)
	pins := []int{0, 2, 5}
	for _, tc := range []struct {
		name   string
		body   []byte
		ok     bool
		writes uint64
	}{
		{"read only", []byte{0x20, 0x02, 0x0b}, true, 0},
		{"set and tee", []byte{0x21, 0x05, 0x22, 0x02, 0x0b}, true, 0b110},
		{"nested loop", []byte{0x03, 0x40, 0x21, 0x00, 0x0b, 0x0b}, true, 0},
		{"branch table", []byte{0x21, 0x02, 0x0e, 0x01, 0x00, 0x00, 0x0b}, true, 0},
		{"call", []byte{0x21, 0x02, 0x10, 0x00, 0x0b}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, writes := scanLoopCallFree(wasm.NewReader(tc.body), classifier, false, pins)
			if ok != tc.ok || writes != tc.writes {
				t.Fatalf("got callFree=%v writes=%b, want %v %b", ok, writes, tc.ok, tc.writes)
			}
		})
	}
}

func TestCallFreeLoopRegisterStateAfterConditionalWriteAMD64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{
			0x01, 0x01, 0x7f,
			0x41, 0x01, 0x21, 0x01,
			0x02, 0x40, 0x03, 0x40,
			0x20, 0x00, 0x45, 0x0d, 0x01,
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00,
			0x20, 0x00, 0x41, 0x02, 0x46, 0x04, 0x40,
			0x41, 0x2a, 0x21, 0x01, 0x0b,
			0x20, 0x01, 0x1a, 0x0c, 0x00,
			0x0b, 0x0b,
			0x20, 0x01, 0x10, 0x01, 0x0b,
		}},
		funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}},
	)
	for _, enabled := range []bool{false, true} {
		cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{
			"inline": false, "callfree-loop-reg-state": enabled,
		}})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ n, want uint64 }{{0, 1}, {1, 1}, {2, 1}, {3, 42}, {4, 42}, {9, 42}} {
			if got := runCompiledAmd64u(t, cm, tc.n); got != tc.want {
				t.Fatalf("enabled=%v f(%d)=%d, want %d", enabled, tc.n, got, tc.want)
			}
		}
		cm.CodeImage.Close()
	}
}
