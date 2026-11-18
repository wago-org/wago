//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScopedBoundsControlAndTrapOrder(t *testing.T) {
	// Parameters: address, iteration count, branch selector, replacement address.
	// The independent oracle checks complete memory, including stores before traps.
	load := []byte{0x20, 0, 0x29, 0, 0, 0x1a}
	store := func(off byte) []byte { return []byte{0x20, 0, 0x20, 1, 0x36, 0, off} }
	marker := []byte{0x41, 32, 0x41, 27, 0x36, 0, 0}
	loop := func(body []byte) []byte {
		b := []byte{0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x45, 0x0d, 1}
		b = append(b, body...)
		return append(b, 0x20, 1, 0x41, 1, 0x6b, 0x21, 1, 0x0c, 0, 0x0b, 0x0b)
	}
	type state struct {
		mem                     []byte
		ptr, count, cond, other uint32
		trap                    bool
	}
	check := func(s *state, off, size uint32) bool {
		if uint64(s.ptr)+uint64(off)+uint64(size) > 65536 {
			s.trap = true
			return false
		}
		return true
	}
	write := func(s *state, off uint32) {
		if !s.trap && check(s, off, 4) {
			binary.LittleEndian.PutUint32(s.mem[s.ptr+off:], s.count)
		}
	}
	prove := func(s *state) {
		if !s.trap {
			check(s, 0, 8)
		}
	}
	mark := func(s *state) {
		if !s.trap {
			binary.LittleEndian.PutUint32(s.mem[32:], 27)
		}
	}
	repeat := func(s *state, update bool) {
		for s.count != 0 && !s.trap {
			write(s, 4)
			if update {
				s.ptr = s.other
			}
			s.count--
		}
	}
	joined := append([]byte{0x20, 2, 0x04, 0x40}, store(4)...)
	joined = append(joined, 0x05)
	joined = append(joined, store(0)...)
	joined = append(joined, 0x0b)
	conditionalProof := append([]byte{0x20, 2, 0x04, 0x40}, load...)
	conditionalProof = append(conditionalProof, 0x0b)
	changed := []byte{0x20, 2, 0x04, 0x40, 0x20, 3, 0x21, 0, 0x0b}
	cases := []struct {
		name  string
		code  []byte
		model func(*state)
		hit   bool
	}{
		{"invariant loop", append(append(append([]byte{}, load...), marker...), loop(store(4))...), func(s *state) { prove(s); mark(s); repeat(s, false) }, true},
		{"both arms", append(append(append([]byte{}, load...), marker...), joined...), func(s *state) {
			prove(s)
			mark(s)
			if s.cond != 0 {
				write(s, 4)
			} else {
				write(s, 0)
			}
		}, true},
		{"conditional proof does not dominate", append(append(append([]byte{}, conditionalProof...), marker...), loop(store(4))...), func(s *state) {
			if s.cond != 0 {
				prove(s)
			}
			mark(s)
			repeat(s, false)
		}, false},
		{"conditional local write", append(append(append([]byte{}, load...), marker...), loop(append(append([]byte{}, changed...), store(4)...))...), func(s *state) {
			prove(s)
			mark(s)
			for s.count != 0 && !s.trap {
				if s.cond != 0 {
					s.ptr = s.other
				}
				write(s, 4)
				s.count--
			}
		}, false},
		{"backedge local write", append(append(append([]byte{}, load...), marker...), loop(append(store(4), 0x20, 3, 0x21, 0))...), func(s *state) { prove(s); mark(s); repeat(s, true) }, false},
	}
	partial := append(append(store(0), store(4)...), store(8)...)
	cases = append(cases, struct {
		name  string
		code  []byte
		model func(*state)
		hit   bool
	}{
		"extent stops at proven end", append(append(append([]byte{}, load...), marker...), loop(partial)...),
		func(s *state) {
			prove(s)
			mark(s)
			for s.count != 0 && !s.trap {
				write(s, 0)
				write(s, 4)
				write(s, 8)
				s.count--
			}
		}, true,
	})
	conditionalNested := append([]byte{0x20, 2, 0x04, 0x40}, load...)
	conditionalNested = append(conditionalNested, 0x02, 0x40, 0x01, 0x0b, 0x0b)
	conditionalNested = append(conditionalNested, marker...)
	conditionalNested = append(conditionalNested, loop(store(4))...)
	cases = append(cases, struct {
		name  string
		code  []byte
		model func(*state)
		hit   bool
	}{
		"conditional nested proof expires", conditionalNested,
		func(s *state) {
			if s.cond != 0 {
				prove(s)
			}
			mark(s)
			repeat(s, false)
		}, false,
	})
	// Nested scopes must preserve the entry proof; a write after their exit must
	// invalidate it before a sibling scope, even when it reuses the same depth.
	nested := append([]byte{0x02, 0x40}, loop(store(4))...)
	nested = append(nested, 0x0b, 0x20, 3, 0x21, 0, 0x02, 0x40)
	nested = append(nested, store(0)...)
	nested = append(nested, 0x0b)
	cases = append(cases, struct {
		name  string
		code  []byte
		model func(*state)
		hit   bool
	}{"nested then sibling", append(append(append([]byte{}, load...), marker...), nested...), func(s *state) { prove(s); mark(s); repeat(s, false); s.ptr = s.other; write(s, 0) }, true})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0}, tc.code...)
			body = append(body, 0x41, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, facts := range []bool{false, true} {
				opts := CompileOptions{Optimizations: map[string]bool{"bounds-facts": facts, "memory-min-bounds": false, "scalar-loop-unroll": false}}
				for _, ptr := range []uint64{0, 17, 65524, 65528, 65532, 65535, 0xffffffff, 0x8877665500000011} {
					for _, count := range []uint64{0, 1, 3} {
						for _, cond := range []uint64{0, 1} {
							for _, other := range []uint64{41, 65532, 0xffffffff} {
								s := state{mem: make([]byte, 65536), ptr: uint32(ptr), count: uint32(count), cond: uint32(cond), other: uint32(other)}
								tc.model(&s)
								got, err := runScalarUnrollMemory(t, m, opts, ptr, count, cond, other)
								if (err != nil) != s.trap || !bytes.Equal(got, s.mem) {
									t.Fatalf("facts%v ptr%x count%d cond%d other%x trap%v err%v equal%v", facts, ptr, count, cond, other, s.trap, err, bytes.Equal(got, s.mem))
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestScopedBoundsScanRejectsCallsAndWideWrites(t *testing.T) {
	// A failed scan must not lend any proof to a later control scope.
	for _, body := range [][]byte{{0x10, 0, 0x0b}, {0x20, 0, 0x21, 0x80, 0x80, 4, 0x0b}, {0xfc}} {
		r := wasm.ReaderFrom(body)
		sets, call := scanLoopSetLocals(&r, wasm.NewModuleInstructionClassifier(&wasm.Module{}, true), nil, nil)
		if sets != nil && !call {
			t.Fatalf("accepted %x", body)
		}
		if r.Offset() != 0 {
			t.Fatal("scan moved the compiler reader")
		}
	}
}
