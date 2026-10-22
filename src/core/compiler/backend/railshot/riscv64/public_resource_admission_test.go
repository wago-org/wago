//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPublicTableCountsAndAdvertisedMaximum(t *testing.T) {
	for _, count := range []int{3, MaxFunctions, MaxFunctions + 1} {
		m := &wasm.Module{Tables: make([]wasm.Table, count)}
		for i := range m.Tables {
			m.Tables[i].Type = wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Max: 0xffffffff, HasMax: true}}
		}
		want := count <= MaxFunctions
		if err := AdmitPublicInitializers(m); (err == nil) != want {
			t.Fatalf("initializer table count %d: %v", count, err)
		}
		if err := ownedTableBounds(m, true); (err == nil) != want {
			t.Fatalf("public table count %d: %v", count, err)
		}
		if err := ownedTableBounds(m, false); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private table count or capacity widened", err)
		}
		if want {
			if n := testing.AllocsPerRun(100, func() {
				if err := ownedTableBounds(m, true); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatalf("table admission allocations=%g", n)
			}
		}
	}
	for _, count := range []int{3, MaxFunctions + 1} {
		tables := make([][]byte, count)
		for i := range tables {
			tables[i] = []byte{0x70, 0, 0}
		}
		source := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(tables...)))
		if err := AdmitProductionSource(source); (err == nil) != (count <= MaxFunctions) {
			t.Fatalf("wire table count %d: %v", count, err)
		}
	}
}

func TestPublicNullableExternWireTypes(t *testing.T) {
	for _, typ := range [][]byte{{0x6f}, {0x63, 0x6f}} {
		section := append([]byte{1, 0x60, 1}, typ...)
		section = append(section, 0)
		source := wasmtest.Module(wasmtest.Section(1, section))
		if err := AdmitProductionSource(source); err != nil {
			t.Fatal("public nullable extern type", err)
		}
		if err := admitWire(source); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private extern type admitted", err)
		}
	}
}

func publicLargeLocalsModule(t testing.TB, count uint32) *wasm.Module {
	t.Helper()
	locals := append([]byte{1}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x7e)
	body := append([]byte{0x20}, wasmtest.ULEB(count-1)...)
	body = append(body, 0x0b)
	m, err := wasm.DecodeModule(binaryModule(fixtureFunc{result: 0x7e, locals: locals, body: body}))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPublicLargeLocalsConfiguredAndPhysicalBounds(t *testing.T) {
	m := publicLargeLocalsModule(t, 1056)
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, MaxFunctionLocals: 1055}
	if image, err := CompileModuleWith(m, opts); image != nil || err == nil {
		t.Fatal("configured local count admitted", err)
	}
	opts.MaxFunctionLocals = 1056
	image, err := CompileModuleWith(m, opts)
	if err != nil || image == nil {
		t.Fatal("large public local count", err)
	}
	if image.CodeImage != nil {
		defer image.CodeImage.Close()
	}
	t.Logf("1056-local emitted code: %d bytes", len(image.Code))
	if _, err := integerLocalCount(&m.Code[0], &m.Types[0].SubTypes[0].Comp, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("private local bound widened", err)
	}
	m.Code[0].Locals.Runs[0].Count = ^uint32(0)
	if _, err := integerLocalCount(&m.Code[0], &m.Types[0].SubTypes[0].Comp, true); !errors.Is(err, ErrUnsupported) {
		t.Fatal("physical local bound admitted", err)
	}
}

func TestPublicLocalAdmissionAllocationCosts(t *testing.T) {
	for _, count := range []uint32{1, 1056} {
		m := publicLargeLocalsModule(t, count)
		ft, _ := m.LocalFuncType(0)
		state := moduleState{production: true, foreignTableCalls: true}
		n := testing.AllocsPerRun(100, func() {
			if _, err := walkInteger(&m.Code[0], ft, &state, nil); err != nil {
				panic(err)
			}
		})
		// The unchanged walker uses three allocations for its result/control
		// typing. A larger local vector adds only one contiguous carrier buffer.
		want := float64(3)
		if count > 256 {
			want++
		}
		if n > want {
			t.Fatalf("%d-local admission allocations=%g, want %g", count, n, want)
		}
		t.Logf("%d-local admission: %g allocations/run", count, n)
	}
}

func BenchmarkPublicLargeLocalCompile(b *testing.B) {
	m := publicLargeLocalsModule(b, 1056)
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, MaxFunctionLocals: 1056}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		image, err := CompileModuleWith(m, opts)
		if err != nil {
			b.Fatal(err)
		}
		if image.CodeImage != nil {
			image.CodeImage.Close()
		}
	}
}

func BenchmarkPublicLocalAdmission(b *testing.B) {
	for _, test := range []struct {
		name  string
		count uint32
	}{{"small", 1}, {"large", 1056}} {
		b.Run(test.name, func(b *testing.B) {
			m := publicLargeLocalsModule(b, test.count)
			ft, _ := m.LocalFuncType(0)
			state := moduleState{production: true, foreignTableCalls: true}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := walkInteger(&m.Code[0], ft, &state, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
