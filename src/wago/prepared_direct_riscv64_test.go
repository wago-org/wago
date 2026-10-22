//go:build linux && riscv64 && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"slices"
	"testing"
)

func TestRV64DirectIntegerTrapAndReuse(t *testing.T) {
	m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x20, 0, 0x6d, 0x0b}))))
	c := MustCompile(m)
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("f", 1); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("invoke=%v/%v trap=%x", got, err, in.trap)
	}
	if _, err := in.Invoke("f", 0); err == nil {
		t.Fatal("missing divide trap")
	}
	if got, err := in.Invoke("f", 1); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("reuse=%v/%v", got, err)
	}
}

func TestRV64DirectIntegerZeroArgumentPaths(t *testing.T) {
	m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))))
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded%v", bounded), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithOptimization("prepared-bounded-entry", bounded), m)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatalf("Invoke[%d]=%v/%v", i, got, err)
				}
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.directIntFast || fn.directIntBounded != bounded {
				t.Fatalf("direct/bounded=%v/%v", fn.directIntFast, fn.directIntBounded)
			}
			for i := 0; i < 3; i++ {
				got, err := fn.Invoke()
				if err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatalf("prepared[%d]=%v/%v", i, got, err)
				}
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				got, err := session.Invoke0()
				if err != nil || len(got) != 1 || got[0] != 7 {
					session.Close()
					t.Fatalf("session[%d]=%v/%v", i, got, err)
				}
			}
			if alloc := testing.AllocsPerRun(100, func() {
				got, err := session.Invoke0()
				if err != nil || len(got) != 1 || got[0] != 7 {
					panic("session result")
				}
			}); alloc != 0 {
				t.Errorf("warm session allocs=%v", alloc)
			}
			session.Close()
			if alloc := testing.AllocsPerRun(100, func() {
				got, err := fn.Invoke()
				if err != nil || len(got) != 1 || got[0] != 7 {
					panic("prepared result")
				}
			}); alloc != 0 {
				t.Errorf("warm prepared allocs=%v", alloc)
			}
		})
	}
}

func TestRV64DirectIntegerArtifactAndPolicy(t *testing.T) {
	m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x7c, 0x20, 2, 0x7c, 0x20, 3, 0x7c, 0x0b}))))
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("register%v", enabled), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithOptimization("reg-abi", enabled), m)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.directPreparedAt(0) != enabled {
				t.Fatalf("source direct=%v", c.directPreparedAt(0))
			}
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var loaded Compiled
			if err := loaded.UnmarshalBinary(blob); err != nil {
				t.Fatal(err)
			}
			defer loaded.Close()
			if loaded.directPreparedAt(0) {
				t.Fatal("artifact retained compile-only direct selection")
			}
			for _, image := range []*Compiled{c, &loaded} {
				in, err := Instantiate(image)
				if err != nil {
					t.Fatal(err)
				}
				fn, err := in.WasmFunc("sum")
				if err != nil {
					in.Close()
					t.Fatal(err)
				}
				for _, args := range [][4]uint64{{1, 2, 3, 4}, {^uint64(0), 1, 2, 3}, {1 << 63, 1 << 63, 5, 7}} {
					want := args[0] + args[1] + args[2] + args[3]
					got, err := in.Invoke("sum", args[:]...)
					if err != nil || len(got) != 1 || got[0] != want {
						in.Close()
						t.Fatalf("Invoke=%v/%v want%x", got, err, want)
					}
					got, err = fn.Invoke(args[:]...)
					if err != nil || len(got) != 1 || got[0] != want {
						in.Close()
						t.Fatalf("prepared=%v/%v want%x", got, err, want)
					}
				}
				in.Close()
			}
		})
	}
}

func BenchmarkRV64PreparedIntegerEntry(b *testing.B) {
	for _, tc := range []struct {
		name            string
		direct, bounded bool
	}{{"serial", false, false}, {"register-syscall", true, false}, {"register-bounded", true, true}} {
		b.Run(tc.name, func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithOptimization("reg-abi", tc.direct).WithOptimization("prepared-bounded-entry", tc.bounded), benchAddOneModule())
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("f")
			if err != nil {
				b.Fatal(err)
			}
			if _, err := fn.Invoke(41); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := fn.Invoke(41)
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("result=%v/%v", got, err)
				}
			}
			b.ReportMetric(float64(len(c.code)), "code-bytes")
		})
	}
}

func TestRV64DirectIntegerOnlyExported(t *testing.T) {
	m := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 3, 0x0b}), wasmtest.Code([]byte{0x41, 7, 0x0b}))))
	c := MustCompile(m)
	defer c.Close()
	if c.directPreparedAt(0) || !c.directPreparedAt(1) {
		t.Fatal("adapter is not limited to public prepared functions")
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f")
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("invoke=%v/%v", got, err)
	}
}

func TestRV64DirectIntegerRejectsFramedSelectionMarkers(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithOptimization("reg-abi", false), benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := c.executionView()
	for _, mark := range []func(int) int{markDirectPreparedEntry, markDirectPreparedBoundedEntry, markDirectPreparedLightEntry} {
		changed := *view
		changed.InternalEntry = slices.Clone(view.InternalEntry)
		changed.InternalEntry[0] = mark(changed.InternalEntry[0])
		if err := railshotValidateCompiled(&changed); err == nil {
			t.Fatal("framed entry accepted a private selection marker")
		}
	}
}
