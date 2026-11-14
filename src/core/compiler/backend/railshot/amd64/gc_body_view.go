//go:build amd64

package amd64

import (
	"crypto/sha256"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"sort"
)

var gcPrivateSinkDropEnabled = os.Getenv("WAGO_AMD64_NO_GC_SINK_STORE_ELISION") != "1"

func validateGCBodyViews(m *wasm.Module, views map[uint32]codegen.GCBodyView) error {
	for index, view := range views {
		local := int(index) - m.ImportedFuncCount()
		if local < 0 || local >= len(m.Code) {
			return fmt.Errorf("GC body view: function %d unavailable", index)
		}
		c := m.Code[local]
		if sha256.Sum256(c.BodyBytes) != view.BodySHA256 {
			return fmt.Errorf("GC body view: stale function %d hash", index)
		}
		if len(view.Constructors) == 0 || len(view.Constructors) > 32 || len(view.Origins) == 0 || view.Origins[0].NewPC != 0 {
			return fmt.Errorf("GC body view: invalid record bounds")
		}
		for i, o := range view.Origins {
			if int(o.NewPC) >= len(c.BodyBytes) || (i > 0 && view.Origins[i-1].NewPC >= o.NewPC) {
				return fmt.Errorf("GC body view: unordered source origins")
			}
		}
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		r := wasm.NewReader(c.BodyBytes)
		seen := 0
		sinks := map[uint32]bool{}
		for _, cert := range view.Constructors {
			sinks[cert.Sink] = true
		}
		for r.HasNext() {
			pc := uint32(r.Offset())
			op, e := r.Byte()
			if e != nil {
				return e
			}
			var im wasm.InstructionImmediate
			if e = classifier.ClassifyInto(r, op, &im); e != nil {
				return e
			}
			if (op == 0x20 || op == 0x22) && sinks[im.Index] {
				return fmt.Errorf("GC body view: sink local is read")
			}
			if cert, ok := view.Constructors[pc]; ok {
				if im.Kind != wasm.InstrStructNew || im.Index != cert.Type {
					return fmt.Errorf("GC body view: certificate is not its constructor")
				}
				next := *r
				set, e := next.Byte()
				if e != nil || set != 0x21 {
					return fmt.Errorf("GC body view: constructor has no sink")
				}
				slot, e := next.U32()
				if e != nil || slot != cert.Sink {
					return fmt.Errorf("GC body view: constructor sink mismatch")
				}
				seen++
			}
		}
		if seen != len(view.Constructors) {
			return fmt.Errorf("GC body view: certificate is not an instruction boundary")
		}
	}
	return nil
}

func (f *fn) certifiedDeadConstructor(typ uint32, r *wasm.Reader) bool {
	if len(f.gcBodyViews) == 0 || f.wasmPC < f.tracePCBase {
		return false
	}
	view, ok := f.gcBodyViews[f.traceFuncIdx]
	if !ok {
		return false
	}
	cert, ok := view.Constructors[f.wasmPC-f.tracePCBase]
	if !ok || cert.Type != typ {
		return false
	}
	next := *r
	op, e := next.Byte()
	if e != nil || op != 0x21 {
		return false
	}
	slot, e := next.U32()
	if e != nil || slot != cert.Sink {
		return false
	}
	f.stats.peep("gc-private-result-charge")
	return true
}

func (f *fn) gcBodySourcePC() (uint32, uint32) {
	view, ok := f.gcBodyViews[f.traceFuncIdx]
	if !ok || f.wasmPC < f.tracePCBase {
		return f.traceFuncIdx, f.wasmPC
	}
	pc := f.wasmPC - f.tracePCBase
	i := sort.Search(len(view.Origins), func(i int) bool { return view.Origins[i].NewPC > pc }) - 1
	if i < 0 {
		return f.traceFuncIdx, f.wasmPC
	}
	o := view.Origins[i]
	old := o.OldPC
	if o.Linear {
		old += pc - o.NewPC
	}
	return o.OldFunction, old
}

// certifiedUnreadGCSink is valid only after validateGCBodyViews has proved that
// no local.get or local.tee reads this sink anywhere in the exact body hash.
// The constructor and its trapping operands already ran; dropping the result
// avoids a store that cannot contribute to any later source value or GC root.
func (f *fn) certifiedUnreadGCSink(local int) bool {
	if len(f.gcBodyViews) == 0 || local < f.localBase {
		return false
	}
	view, ok := f.gcBodyViews[f.traceFuncIdx]
	if !ok {
		return false
	}
	sourceLocal := uint32(local - f.localBase)
	for _, cert := range view.Constructors {
		if cert.Sink == sourceLocal {
			return true
		}
	}
	return false
}
