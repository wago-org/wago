//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcLoopTestFunction(t *testing.T) fn {
	t.Helper()
	m, e := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 2, 0x7f, 1, 0x7f, 1}))))
	if e != nil {
		t.Fatal(e)
	}
	metadata, e := frontend.BuildGCTypeMetadata(m)
	if e != nil {
		t.Fatal(e)
	}
	return fn{gcTypeLayouts: metadata.Layouts, m: m, classifier: wasm.NewModuleInstructionClassifier(m, true), gcStructHelpers: true, gcArrayHelpers: true}
}

func TestGCResolvedLoopProof(t *testing.T) {
	read := []byte{0x20, 0, 0xfb, 2, 0, 0, 0x1a}
	for _, tc := range []struct {
		name          string
		before, after []byte
		want          int
	}{
		{"read", nil, nil, 1},
		{"merge", []byte{0x41, 0, 0x04, 0x7f, 0x41, 0, 0x05}, []byte{0x41, 0, 0x0b, 0x1a}, 1},
		{"write-before", []byte{0x20, 1, 0x21, 0}, nil, 0},
		{"write-after", nil, []byte{0x20, 1, 0x22, 0, 0x1a}, 0},
		{"other-write", nil, []byte{0x41, 0, 0x21, 2}, 1},
		{"call", nil, []byte{0x10, 0}, 0},
		{"indirect", nil, []byte{0x11, 0, 0}, 0},
		{"host-global", nil, []byte{0x23, 0, 0x1a}, 0},
		{"allocation", nil, []byte{0xfb, 1, 0, 0x1a}, 0},
		{"nested-loop", nil, []byte{3, 0x40, 0x0b}, 0},
		{"unknown", nil, []byte{0xff}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := gcLoopTestFunction(t)
			code := append(append(append([]byte{}, tc.before...), read...), tc.after...)
			code = append(code, 0xb)
			r := wasm.ReaderFrom(code)
			p := f.inspectGCResolvedLoop(&r)
			if p.n != tc.want {
				t.Fatalf("n=%d want=%d", p.n, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("lookahead consumed source")
			}
		})
	}
	for _, mode := range []string{"interrupt", "EH", "custom", "inline", "budget"} {
		f := gcLoopTestFunction(t)
		switch mode {
		case "interrupt":
			f.interruptible = true
		case "EH":
			f.moduleEH = true
		case "custom":
			f.customInstructions = map[uint32]CustomInstruction{0: {}}
		case "inline":
			f.inlineDepth = 1
		case "budget":
			f.gcLoopScanBytes = gcLoopScanBudget
		}
		r := wasm.ReaderFrom(append(append([]byte{}, read...), 0xb))
		if f.inspectGCResolvedLoop(&r).n != 0 {
			t.Fatal(mode)
		}
	}
	for _, n := range []int{gcLoopScanOps - 8, gcLoopScanOps, gcLoopScanLimit} {
		f := gcLoopTestFunction(t)
		code := append(append(append([]byte{}, read...), bytes.Repeat([]byte{1}, n)...), 0xb)
		r := wasm.ReaderFrom(code)
		p := f.inspectGCResolvedLoop(&r)
		if (p.n != 0) != (n == gcLoopScanOps-8) {
			t.Fatalf("operation limit n=%d admitted=%d", n, p.n)
		}
		if f.gcLoopScanBytes > gcLoopScanBudget {
			t.Fatal("budget exceeded")
		}
	}
}

func TestGCResolvedCertificateSet(t *testing.T) {
	var f fn
	for i, reg := range []Reg{RBP, R12, R13, R14} {
		c := f.freeGCResolvedObject()
		if c == nil {
			t.Fatal("less than four certificates")
		}
		*c = gcResolvedObject{valid: true, local: i, typeIndex: 1, requiredBytes: 24, reg: reg}
		f.pinned = f.pinned.add(reg)
	}
	if f.freeGCResolvedObject() != nil {
		t.Fatal("more than four certificates")
	}
	f.invalidateGCLoadFactsForLocal(1)
	if f.findGCResolvedObject(1, 1) != nil || f.findGCResolvedObject(0, 1) == nil || f.pinned.has(R12) {
		t.Fatal("source write did not invalidate exactly its certificate")
	}
	f.prepareGCResolvedObject(0x10)
	if f.hasGCResolvedObject() || f.pinned != 0 {
		t.Fatal("call retained raw address")
	}
	for _, sub := range []uint32{0, 1, 6, 7, 8, 9, 10, 16, 17} {
		f.gcResolved = gcResolvedObject{valid: true, reg: RBP}
		f.pinned = f.pinned.add(RBP)
		f.prepareGCResolvedFB(sub)
		if f.hasGCResolvedObject() || f.pinned != 0 {
			t.Fatalf("effect sub=%d retained raw address", sub)
		}
	}
}

func TestGCResolvedLoopScanAllocations(t *testing.T) {
	f := gcLoopTestFunction(t)
	code := []byte{0x20, 0, 0xfb, 2, 0, 0, 0x1a, 0x0b}
	allocs := testing.AllocsPerRun(100, func() {
		f.gcLoopScanBytes = 0
		r := wasm.ReaderFrom(code)
		if f.inspectGCResolvedLoop(&r).n != 1 {
			panic("proof rejected")
		}
	})
	if allocs != 0 {
		t.Fatalf("loop scan allocated %g objects", allocs)
	}
	t.Logf("loop scan allocations=%g", allocs)
}

func TestGCResolvedLoopByteBound(t *testing.T) {
	f := gcLoopTestFunction(t)
	// 12 bytes and two operations per constant/drop, so bytes are limiting.
	wide := []byte{0x42, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0, 0x1a}
	code := []byte{0x20, 0, 0xfb, 2, 0, 0, 0x1a}
	code = append(code, bytes.Repeat(wide, gcLoopScanLimit/12+1)...)
	code = append(code, 0xb)
	r := wasm.ReaderFrom(code)
	if f.inspectGCResolvedLoop(&r).n != 0 {
		t.Fatal("admitted loop above byte bound")
	}
	if r.Offset() != 0 || f.gcLoopScanBytes > gcLoopScanLimit {
		t.Fatal("lookahead escaped byte bound")
	}
}
