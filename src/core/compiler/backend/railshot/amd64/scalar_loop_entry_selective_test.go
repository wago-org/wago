//go:build amd64 && wago_experimental_scalar_loop_entry && wago_experimental_poll_padding

package amd64

import (
	"bytes"
	"encoding/binary"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestScalarEntrySelectiveBudgetAndTargets(t *testing.T) {
	old, padding := scalarLoopEntry, interruptPollPadding
	defer func() { scalarLoopEntry = old; interruptPollPadding = padding }()
	scalarLoopEntry = -2
	interruptPollPadding = 0
	if parseScalarLoopEntry("selective") != -2 {
		t.Fatal("selector")
	}
	for offset := 0; offset < 32; offset++ {
		a := &x64.Asm{}
		a.EmitBytes(bytes.Repeat([]byte{0x90}, offset))
		f := fn{a: a, sc: newScratch(), interruptible: true}
		selected := f.alignExperimentalScalarLoop(0, 0)
		want := offset >= 1 && offset <= 16
		if selected != want {
			t.Fatalf("offset%d selected%v want%v", offset, selected, want)
		}
		if !selected {
			if a.Len() != offset {
				t.Fatal("fallback emitted bytes")
			}
			continue
		}
		header := a.Len()
		if header&31 != 0 || f.extraBytes != 16 {
			t.Fatal("wrong selected header/budget")
		}
		f.emitInterruptCheck(RSI)
		if a.Len()-header != ordinaryScalarLoopPollBytes {
			t.Fatal("actual emitted poll differs from selection model")
		}
		back := a.JmpPlaceholder()
		a.PatchRel32(back, header)
		got := back + 4 + int(int32(binary.LittleEndian.Uint32(a.B[back:back+4])))
		if got != header {
			t.Fatal("backedge bypasses or repeats padding")
		}
		if a.B[header] != 0x48 {
			t.Fatal("header no longer original poll")
		}
	}
	a := &x64.Asm{}
	f := fn{a: a, interruptible: true}
	for i := 0; i < 5; i++ {
		a.EmitBytes(bytes.Repeat([]byte{0x90}, (1-a.Len())&31))
		before := a.Len()
		got := f.alignExperimentalScalarLoop(0, 0)
		if got != (i < 4) {
			t.Fatalf("selection %d=%v", i, got)
		}
		if i == 4 && a.Len() != before {
			t.Fatal("budget fallback changed code")
		}
	}
	if f.extraBytes != 64 {
		t.Fatal("incremental budget exceeded or miscounted")
	}
}

func TestScalarEntrySelectiveFallbacks(t *testing.T) {
	old, padding := scalarLoopEntry, interruptPollPadding
	defer func() { scalarLoopEntry = old; interruptPollPadding = padding }()
	scalarLoopEntry = -2
	interruptPollPadding = 0
	for _, tc := range []struct {
		params, results                    int
		interruptible, eh, compact, padded bool
	}{
		{params: 1, interruptible: true}, {results: 1, interruptible: true},
		{}, {interruptible: true, eh: true}, {interruptible: true, compact: true},
		{interruptible: true, padded: true},
	} {
		a := &x64.Asm{}
		a.EmitBytes([]byte{0x90})
		f := fn{a: a, interruptible: tc.interruptible, moduleEH: tc.eh, compactLoopAlign32: tc.compact}
		interruptPollPadding = 0
		if tc.padded {
			interruptPollPadding = 32
		}
		if f.alignExperimentalScalarLoop(tc.params, tc.results) || a.Len() != 1 || f.extraBytes != 0 {
			t.Fatal("fallback altered layout/budget", tc)
		}
	}
}
