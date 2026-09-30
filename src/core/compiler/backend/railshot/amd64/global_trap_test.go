//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTrapExitStoresDirtyValuePin(t *testing.T) {
	for _, shared := range []bool{false, true} {
		f := &fn{
			a: &encoder.Asm{}, s: newStack(), sc: &scratch{},
			m:         &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}},
			globalReg: []Reg{R12 | globalRegDirty},
		}
		f.trapAlways(trapUnreachable)
		start := f.a.Len()
		if shared {
			f.emitSharedTrapStubs(1)
		} else {
			f.emitTrapStubs()
		}
		expected := &encoder.Asm{}
		expected.Store64(RSI, 0, R12)
		if !bytes.Contains(f.a.B[start:], expected.B) {
			t.Fatalf("shared=%v: trap exit has no store from value pin R12 using RSI", shared)
		}
	}
}

func TestEntryTrapInitializesValuePinBeforeExit(t *testing.T) {
	f := &fn{
		a: &encoder.Asm{}, s: newStack(), sc: &scratch{},
		m:         &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}},
		globalReg: []Reg{R12 | globalRegDirty},
	}
	f.trapAlways(trapInterrupted)
	f.entryTrapEnd = f.a.Len()
	entry := f.sc.trapSites[trapInterrupted][0].branch
	f.trapAlways(trapInterrupted)
	body := f.sc.trapSites[trapInterrupted][1].branch
	start := f.a.Len()
	f.prepareEntryTrapPins()
	sites := f.sc.trapSites[trapInterrupted]
	if sites[0].branch == entry || sites[1].branch != body || f.a.Len() <= start {
		t.Fatalf("entry/body trap branches = %v; original %d/%d", sites, entry, body)
	}
}
