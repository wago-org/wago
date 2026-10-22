//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARPairEmissionAndInvalidInputs(t *testing.T) {
	pair := swarPair{enc.X5, enc.X6}
	for _, kind := range []wasm.InstrKind{wasm.InstrV128Not, wasm.InstrV128And, wasm.InstrV128Andnot, wasm.InstrV128Or, wasm.InstrV128Xor, wasm.InstrV128Bitselect} {
		f := target{}
		f.swarBits(kind, pair, swarPair{enc.X6, enc.X5}, swarPair{enc.X7, enc.X9}, swarPair{enc.X10, enc.X11})
		if f.err != nil || f.a.Err() != nil || len(f.a.B) < 16 || len(f.a.B) > 32 {
			t.Fatal(kind, f.err, f.a.Err(), len(f.a.B))
		}
	}
	for _, test := range []struct {
		name string
		emit func(*integerEmitter)
	}{
		{"reserved-low", func(f *integerEmitter) { f.swarMove(swarPair{enc.SP, enc.X6}, pair) }},
		{"scratch-high", func(f *integerEmitter) { f.swarMove(pair, swarPair{enc.X5, enc.Scratch1}) }},
		{"alias-halves", func(f *integerEmitter) { f.swarLoad(16, swarPair{enc.X5, enc.X5}) }},
		{"invalid-register", func(f *integerEmitter) { f.swarStore(16, swarPair{32, enc.X6}) }},
		{"negative-offset", func(f *integerEmitter) { f.swarLoad(-8, pair) }},
		{"header-offset", func(f *integerEmitter) { f.swarStore(8, pair) }},
		{"unaligned-offset", func(f *integerEmitter) { f.swarLoad(20, pair) }},
		{"overflow-offset", func(f *integerEmitter) { f.swarStore(math.MaxInt32-7, pair) }},
		{"unknown-op", func(f *integerEmitter) { f.swarBits(wasm.InstrI8x16Add, pair, pair, pair, pair) }},
		{"invalid-mask", func(f *integerEmitter) { f.swarBits(wasm.InstrV128Bitselect, pair, pair, pair, swarPair{}) }},
		{"wide-memory-offset", func(f *integerEmitter) { f.swarMemory(1<<32, true) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := integerEmitter{}
			f.a.Addi(enc.X5, enc.Zero, 7)
			before := bytes.Clone(f.a.B)
			test.emit(&f)
			if f.err == nil || !bytes.Equal(before, f.a.B) {
				t.Fatal("invalid SWAR emission", f.err)
			}
			f.swarMove(pair, pair)
			f.swarMemory(0, false)
			if !bytes.Equal(before, f.a.B) {
				t.Fatal("sticky SWAR error emitted")
			}
		})
	}
	// The pair prerequisite must not open source or public production admission.
	data := binaryModule(fixtureFunc{body: []byte{0xfd, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, 0x0b}})
	if image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: true}); image != nil || err == nil {
		t.Fatal("SWAR prerequisite admitted V128", image, err)
	}
}
