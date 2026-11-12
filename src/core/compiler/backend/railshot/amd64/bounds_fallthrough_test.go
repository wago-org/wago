//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBoundsFallthrough(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		body := []byte{0x00,
			0x02, 0x40, 0x03, 0x40, // block; loop
			0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // load p+4
		}
		if overwrite {
			body = append(body, 0x20, 0x02, 0x21, 0x00) // p = replacement
		}
		body = append(body,
			0x20, 0x01, 0x41, 0x00, 0x47, 0x0d, 0x01, // if cond != 0, exit block
			0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // load p
			0x0b, 0x0b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, body)
		for _, tc := range []struct {
			p, cond, replacement uint64
			trap                 bool
		}{
			{65528, 0, 0, false},
			{65529, 0, 0, true},
			{65528, 1, 65536, false},
			{0, 0, 65536, overwrite},
		} {
			_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, tc.p, tc.cond, tc.replacement)
			if (err != nil) != tc.trap {
				t.Fatalf("overwrite=%v p=%d cond=%d replacement=%d: trap=%v, err=%v", overwrite, tc.p, tc.cond, tc.replacement, tc.trap, err)
			}
		}
	}
}

func TestBoundsFallthroughCode(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0x00, 0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x20, 0x01, 0x41, 0x00, 0x47, 0x0d, 0x01,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, 0x0b, 0x0b, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
	s := compileWithStats(t, m, false).Funcs[0]
	if s.BoundsChecks != 1 || s.BoundsChecksElidable != 1 {
		t.Fatalf("checks=%d elided=%d, want 1/1", s.BoundsChecks, s.BoundsChecksElidable)
	}
}

func TestBoundsFallthroughJoinAndTrapPrefix(t *testing.T) {
	body := []byte{0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a, // checked initial pointer
		0x20, 0x02, 0x21, 0x00, // replace it
		0x20, 0x01, 0x41, 0x00, 0x47, 0x0d, 0x01,
		0x41, 0x00, 0x41, 0x07, 0x36, 0x02, 0x00, // store before possible trap
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a,
		0x0b, 0x0b, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, body)
	_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 8, 0, 65536)
	if err == nil || mem[0] != 7 {
		t.Fatalf("expected trap after store: err=%v memory[0]=%d", err, mem[0])
	}

	join := []byte{0x00, 0x02, 0x40,
		0x20, 0x01, 0x0d, 0x00, // skip the first check
		0x20, 0x00, 0x28, 0x02, 0x04, 0x1a,
		0x0b,
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, 0x0b}
	m = modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, join)
	_, _, err = runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65533, 1)
	if err == nil {
		t.Fatal("expected out-of-bounds trap after join")
	}
}
