//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func TestGCLoopResolverProof(t *testing.T) {
	body := []byte{0x20, 0, 0x20, 1, 0xfb, 11, 0, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x41, 32, 0x49, 0x0d, 0, 0x0b}
	fixture := func() *fn {
		f := fixedArrayRootEmitter(wasm.I32, 0)
		f.m.Types[0].SubTypes[0].Final = true
		f.localType = []machineType{mtI64, mtI32, mtI64}
		f.tracePCBase = 10
		return f
	}
	for _, tc := range []struct {
		name   string
		prefix []byte
		want   bool
	}{
		{"plain", nil, true},
		{"local-arithmetic", []byte{0x20, 1, 0x41, 2, 0x6c, 0x21, 1}, true},
		{"source-assignment", []byte{0x20, 0, 0x21, 0}, false},
		{"nested", []byte{0x02, 0x40, 0x0b}, false},
		{"early-branch", []byte{0x41, 0, 0x0d, 0}, false},
		{"call", []byte{0x10, 0}, false},
		{"division", []byte{0x41, 1, 0x41, 0, 0x6e, 0x1a}, false},
		{"allocation", []byte{0x41, 0, 0xfb, 7, 0, 0x1a}, false},
		{"write", []byte{0x20, 0, 0x41, 0, 0x41, 1, 0xfb, 14, 0}, false},
		{"other-object", []byte{0x20, 2, 0x41, 0, 0xfb, 11, 0, 0x1a}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			code := append(append([]byte{}, tc.prefix...), body...)
			r := wasm.ReaderFrom(code)
			p, ok := f.planGCLoopResolver(&r)
			if ok != tc.want {
				t.Fatalf("admitted=%v want=%v", ok, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("lookahead advanced reader")
			}
			if ok && (p.local != 0 || p.typeIndex != 0 || p.endPC != uint32(10+len(code)-1)) {
				t.Fatalf("bad plan %+v", p)
			}
		})
	}
	for _, mode := range []string{"interruptible", "inline", "eh", "custom", "budget", "vector"} {
		f := fixture()
		switch mode {
		case "interruptible":
			f.interruptible = true
		case "inline":
			f.inlineDepth = 1
		case "eh":
			f.moduleEH = true
		case "custom":
			f.customInstructions = map[uint32]CustomInstruction{0: {}}
		case "budget":
			f.gcPureIfSites = 16
		case "vector":
			f.vectorRegion.enabled = true
		}
		r := wasm.ReaderFrom(body)
		if _, ok := f.planGCLoopResolver(&r); ok {
			t.Fatal(mode)
		}
	}
	for _, n := range []int{64, 128, 256} {
		f := fixture()
		code := []byte{}
		for i := 0; i < n; i++ {
			code = append(code, 0x41, 0, 0x1a)
		}
		code = append(code, body...)
		r := wasm.ReaderFrom(code)
		if _, ok := f.planGCLoopResolver(&r); ok {
			t.Fatalf("unbounded scan %d", n)
		}
	}
	var cached gcResolvedObject
	if unsafe.Sizeof(cached) != 32 {
		t.Fatalf("certificate grew: %d", unsafe.Sizeof(cached))
	}
}

func TestGCLoopResolverLease(t *testing.T) {
	f := fn{wasmPC: 15, gcResolved: gcResolvedObject{valid: true, loopEndPC: 20, reg: RBP}, pinned: maskOf(RBP)}
	f.prepareGCResolvedObject(0x0d)
	if !f.gcResolved.valid {
		t.Fatal("backedge lost lease")
	}
	f.wasmPC = 20
	f.prepareGCResolvedObject(0x0b)
	if f.gcResolved.valid || f.pinned.has(RBP) {
		t.Fatal("lease outlived loop")
	}
}

func TestGCLoopResolverLowering(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 3, 0x20, 0, 0x20, 2, 0xfb, 11, 0, 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x0b}
	m, e := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	))
	if e != nil {
		t.Fatal(e)
	}
	if e = wasm.ValidateModule(m); e != nil {
		t.Fatal(e)
	}
	var stats ModuleStats
	c, e := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CodeImage.Close()
	s := stats.Funcs[0]
	if s.Peephole["gc-loop-resolve-hoist"] != 1 || s.GCHandleResolutions != 1 || s.GCHandleResolutionReuse != 1 {
		t.Fatalf("missing single hoist/reuse: %+v", s)
	}
}
