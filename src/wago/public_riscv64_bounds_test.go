//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicForcedHostModeForOwnedScalarModule(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	m, err := rt.Compile(multiValueControlCallModule())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), m, WithSynchronousHostCalls())
	if err != nil || in == nil {
		t.Fatal("supported scalar synchronous host mode", err)
	}
	got, err := in.Invoke("pair")
	if err != nil || !slices.Equal(got, []uint64{18, 19}) {
		t.Fatal(got, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	in, err = rt.Instantiate(context.Background(), m)
	if err != nil {
		t.Fatal("later ordinary instantiation", err)
	}
	defer in.Close()
	got, err = in.Invoke("pair")
	if err != nil || !slices.Equal(got, []uint64{18, 19}) {
		t.Fatal(got, err)
	}
	t.Run("caller-resolver", func(t *testing.T) {
		other := NewRuntime()
		defer other.Close()
		module, err := other.Compile(multiValueControlCallModule())
		if err != nil {
			t.Fatal(err)
		}
		resolver := &CallerResolver{}
		resolver.activate(other)
		defer resolver.close()
		instance, err := other.Instantiate(context.Background(), module)
		if err != nil || instance == nil {
			t.Fatal("caller-resolver forced host mode", err)
		}
		defer instance.Close()
		got, err := instance.Invoke("pair")
		if err != nil || !slices.Equal(got, []uint64{18, 19}) {
			t.Fatal(got, err)
		}
	})
}

func TestRV64PublicArtifactBoundaries(t *testing.T) {
	c, err := Compile(nil, growMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	check := func(name string, mutate func(*Compiled), accepted bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			view := *base
			mutate(&view)
			err := railshotValidateCompiled(&view)
			if accepted && err != nil || !accepted && !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("artifact boundary", accepted, err)
			}
		})
	}
	for _, count := range []int{256, 257} {
		check("unused-type-arity", func(v *Compiled) {
			unused := DefinedTypeDescriptor{RecGroup: uint32(len(v.Types)), Kind: CompositeTypeFunction, Final: true}
			unused.Params = make([]ValueTypeDescriptor, count)
			unused.Results = make([]ValueTypeDescriptor, count)
			for i := range unused.Params {
				unused.Params[i].Kind = ValueTypeI32
				unused.Results[i].Kind = ValueTypeI64
			}
			v.Types = append(slices.Clone(v.Types), unused)
		}, count == 256)
		check("active-descriptors", func(v *Compiled) { v.Data = make([]DataInit, count) }, count == 256)
		check("passive-descriptors", func(v *Compiled) { v.PassiveData = make([]PassiveDataInit, count) }, count == 256)
	}
	check("inert-GC-count", func(v *Compiled) {
		v.GCTypeDescs = make([]gc.TypeDesc, railshot.MaxFunctions+1)
	}, false)
	for _, extra := range []int{0, 1} {
		check("combined-payload", func(v *Compiled) {
			v.Data = []DataInit{{Bytes: make([]byte, railshot.MaxModuleBytes/2)}}
			v.PassiveData = []PassiveDataInit{{Bytes: make([]byte, railshot.MaxModuleBytes/2+extra)}}
		}, extra == 0)
	}
}

func TestRV64PublicGlobalsAndActiveData(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(6, []byte{1, 0x7f, 1, 0x41, 7, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("read", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x10, 1, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x41, 3, 0x0b, 3, 'a', 'b', 'c'})),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("read"); err != nil || !slices.Equal(got, []uint64{7}) {
		t.Fatal("literal global initializer", got, err)
	}
	if string(in.Memory().UnsafeBytes()[3:6]) != "abc" {
		t.Fatal("active data initializer")
	}
	if got, err := in.Invoke("set", 0xdecafbad80000001); err != nil || !slices.Equal(got, []uint64{0x80000001}) {
		t.Fatal("production global directory/direct call", got, err)
	}
	args := []uint64{0xdecafbad80000001}
	var callErr error
	allocs := testing.AllocsPerRun(100, func() { _, callErr = in.Invoke("set", args...) })
	if callErr != nil || allocs != 0 {
		t.Fatal("warm ordinary public call allocations", allocs, callErr)
	}
}

func TestRV64PublicConfiguredLimits(t *testing.T) {
	c, err := NewRuntimeConfig().WithMemoryLimitPages(2).Compile(growMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tc := range []struct{ delta, want uint64 }{{1, 1}, {1, 0xffffffff}, {0xfeedface00000000, 2}, {0xffffffff, 0xffffffff}} {
		got, err := in.Invoke("grow", tc.delta)
		if err != nil || !slices.Equal(got, []uint64{tc.want}) {
			t.Fatal("configured page quota", tc, got, err)
		}
	}
	if compiled, err := NewRuntimeConfig().WithMaxNativeCodeBytes(1).Compile(multiValueControlCallModule()); err == nil || compiled != nil {
		t.Fatal("native code limit", compiled, err)
	}
}
