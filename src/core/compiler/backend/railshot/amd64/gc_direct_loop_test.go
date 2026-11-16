//go:build amd64 && (wago_gc_direct_trusted_loop || wago_gc_direct_weighted_sum || wago_gc_direct_weighted_sse2)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
	"unsafe"
)

func TestDirectLoopProof(t *testing.T) {
	body := []byte{0x20, 0, 0x20, 1, 0xfb, 11, 0, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x41, 32, 0x49, 0x0d, 0, 0x0b}
	for _, tc := range []struct {
		name   string
		prefix []byte
		want   bool
	}{
		{"read", nil, true}, {"store", []byte{0x20, 0, 0x41, 0, 0x41, 3, 0xfb, 14, 0}, true},
		{"receiver-write", []byte{0x20, 0, 0x21, 0}, false}, {"call", []byte{0x10, 0}, false},
		{"division", []byte{0x41, 1, 0x41, 0, 0x6e, 0x1a}, false}, {"allocate", []byte{0x41, 1, 0xfb, 7, 0, 0x1a}, false},
		{"second-receiver", []byte{0x20, 2, 0x41, 0, 0xfb, 11, 0, 0x1a}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixedArrayRootEmitter(wasm.I32, 0)
			f.m.Types[0].SubTypes[0].Final = true
			f.localType = []machineType{mtI64, mtI32, mtI64}
			r := wasm.ReaderFrom(append(append([]byte{}, tc.prefix...), body...))
			_, ok := f.planGCLoopResolverMode(&r, true)
			if ok != tc.want {
				t.Fatalf("admit=%v want=%v", ok, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("lookahead advanced reader")
			}
		})
	}
	typ := reflect.TypeOf(fn{})
	fields := make([]reflect.StructField, 0, typ.NumField()-1)
	for i := 0; i < typ.NumField(); i++ {
		if fld := typ.Field(i); fld.Name != "directLoop" {
			fld.Anonymous = false
			fields = append(fields, fld)
		}
	}
	baseline := reflect.StructOf(fields).Size()
	if typ.Size()-baseline != 16 {
		t.Fatalf("compiler state growth: %d -> %d", baseline, typ.Size())
	}
	t.Logf("layout without lease=%d", baseline)
	t.Logf("fn bytes=%d; direct lease bytes=%d; offset=%d", unsafe.Sizeof(fn{}), unsafe.Sizeof(directLoopLease{}), unsafe.Offsetof(fn{}.directLoop))
	if unsafe.Sizeof(directLoopLease{}) != 16 {
		t.Fatal("lease grew")
	}
}

func TestDirectLoopLowering(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0, 0x03, 0x40,
		0x20, 3, 0x20, 0, 0x20, 2, 0xfb, 11, 0, 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x20, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x0b, 0x20, 3, 0x0b}
	m, e := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...)))))
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
	if c.GCTrustedDirectVersion != 6 {
		t.Fatalf("receipt %d", c.GCTrustedDirectVersion)
	}
	s := stats.Funcs[0]
	if s.Peephole["gc-direct-loop-hoist"] != 1 || s.Peephole["gc-direct-loop-reuse"] != 1 {
		t.Fatalf("missing loop lease: %+v", s.Peephole)
	}
}
