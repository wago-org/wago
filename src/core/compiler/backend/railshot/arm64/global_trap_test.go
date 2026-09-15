//go:build arm64

package arm64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestTrapExitStoresDirtyValuePin(t *testing.T) {
	for _, reg := range []Reg{X9, X10, X11} {
		for _, shared := range []bool{false, true} {
			f := &fn{
				a: &encoder.Asm{}, sc: &scratch{},
				m:         &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}},
				globalReg: []Reg{reg | globalRegDirty},
			}
			f.trapAlways(trapUnreachable)
			start := f.a.Len()
			if shared {
				f.emitSharedTrapStubs()
			} else {
				f.emitTrapStubs()
			}
			expected := &encoder.Asm{}
			expected.Store64(reg, X16, 0)
			if !bytes.Contains(f.a.B[start:], expected.B) {
				t.Fatalf("shared=%v: trap exit has no store from value pin %v using X16", shared, reg)
			}
			// Shared metadata must not overwrite X10/X11 before their stores.
			if shared && (reg == X10 || reg == X11) {
				metadata := &encoder.Asm{}
				if reg == X10 {
					metadata.MovImm64(X10, 1)
				} else {
					metadata.MovImm64(X11, uint64(trapUnreachable))
				}
				storeAt := bytes.Index(f.a.B[start:], expected.B)
				metadataAt := bytes.Index(f.a.B[start:], metadata.B)
				if metadataAt >= 0 && storeAt >= metadataAt {
					t.Fatalf("pin %v: store at %d, metadata overwrite at %d", reg, storeAt, metadataAt)
				}
			}
		}
	}
}

func TestEntryTrapInitializesValuePinBeforeExit(t *testing.T) {
	f := &fn{
		a: &encoder.Asm{}, sc: &scratch{},
		m:         &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}},
		globalReg: []Reg{X9 | globalRegDirty},
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
