//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"sync"
	"testing"
)

func TestClosedInstanceMemoryAccessCannotReachReusedJobMemory(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod := mustCompileWat(rt, t, `(module (memory 1 1))`)
	c := mod.Compiled()
	a, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	if !a.WriteUint64Le(0, 0x1122334455667788) {
		t.Fatal("seed A memory")
	}
	reused := a.jm
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.jm != nil {
		t.Fatal("closed instance retained stale JobMemory pointer")
	}
	b, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.jm != reused {
		t.Fatalf("JobMemory cache did not deterministically reuse object: A=%p B=%p", reused, b.jm)
	}
	if !b.WriteUint64Le(0, 0xaabbccddeeff0011) || !b.Write(16, []byte("unchanged")) {
		t.Fatal("seed B memory")
	}

	if _, ok := a.ReadUint8(0); ok {
		t.Fatal("closed ReadUint8 succeeded")
	}
	if _, ok := a.ReadUint16Le(0); ok {
		t.Fatal("closed ReadUint16Le succeeded")
	}
	if _, ok := a.ReadUint32Le(0); ok {
		t.Fatal("closed ReadUint32Le succeeded")
	}
	if _, ok := a.ReadUint64Le(0); ok {
		t.Fatal("closed ReadUint64Le succeeded")
	}
	if _, ok := a.ReadFloat32Le(0); ok {
		t.Fatal("closed ReadFloat32Le succeeded")
	}
	if _, ok := a.ReadFloat64Le(0); ok {
		t.Fatal("closed ReadFloat64Le succeeded")
	}
	if _, ok := a.Read(0, 8); ok {
		t.Fatal("closed Read succeeded")
	}
	if a.WriteUint8(0, 1) || a.WriteUint16Le(0, 2) || a.WriteUint32Le(0, 3) || a.WriteUint64Le(0, 4) ||
		a.WriteFloat32Le(0, 5) || a.WriteFloat64Le(0, 6) || a.Write(16, []byte("corrupt")) {
		t.Fatal("closed memory write succeeded")
	}
	if got, ok := b.ReadUint64Le(0); !ok || got != 0xaabbccddeeff0011 {
		t.Fatalf("B scalar after closed A access = %#x, %v", got, ok)
	}
	if got, ok := b.Read(16, uint32(len("unchanged"))); !ok || string(got) != "unchanged" {
		t.Fatalf("B bytes after closed A access = %q, %v", got, ok)
	}
}

func staleGlobalModule(rt *Runtime, t *testing.T) *Module {
	t.Helper()
	return mustCompileWat(rt, t, `(module
		(global $scalar (mut i64) (i64.const 1))
		(global $vector (mut v128) (v128.const i32x4 1 2 3 4))
		(global $ref (mut funcref) (ref.null func))
		(export "scalar" (global $scalar))
		(export "vector" (global $vector))
		(export "ref" (global $ref)))`)
}

func TestClosedInstanceGlobalAccessCannotReachReusedArena(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod := staleGlobalModule(rt, t)
	a, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	reused := a.ar
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if a.ar != nil || a.globalCells != nil || a.globals != nil {
		t.Fatal("closed instance retained stale arena/global state")
	}
	b, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.ar != reused {
		t.Fatalf("Arena cache did not deterministically reuse object: A=%p B=%p", reused, b.ar)
	}
	vec := V128{0xaa, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 0xbb}
	if err := b.SetGlobal("scalar", I64(99)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetGlobalV128("vector", vec); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Global("scalar"); err == nil {
		t.Fatal("closed Global succeeded")
	}
	if _, err := a.GlobalV128("vector"); err == nil {
		t.Fatal("closed GlobalV128 succeeded")
	}
	if _, err := a.GlobalValue("ref"); err == nil {
		t.Fatal("closed GlobalValue succeeded")
	}
	if err := a.SetGlobal("scalar", I64(7)); err == nil {
		t.Fatal("closed SetGlobal succeeded")
	}
	if err := a.SetGlobalV128("vector", V128{1}); err == nil {
		t.Fatal("closed SetGlobalV128 succeeded")
	}
	if err := a.SetGlobalValue("ref", ValueFuncRef(NullFuncRef())); err == nil {
		t.Fatal("closed SetGlobalValue succeeded")
	}
	if got, err := b.Global("scalar"); err != nil || AsI64(got) != 99 {
		t.Fatalf("B scalar after closed A access = %d, %v", AsI64(got), err)
	}
	if got, err := b.GlobalV128("vector"); err != nil || got != vec {
		t.Fatalf("B vector after closed A access = %x, %v", got, err)
	}
}

func TestDirectAccessorsRaceInstanceClose(t *testing.T) {
	for i := 0; i < 20; i++ {
		rt := NewRuntime()
		mod := staleGlobalModule(rt, t)
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				in.WriteUint64Le(0, uint64(j))
				_, _ = in.Read(0, 8)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				_ = in.SetGlobal("scalar", uint64(j))
				_, _ = in.GlobalV128("vector")
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = in.Close()
		}()
		close(start)
		wg.Wait()
		_ = rt.Close()
	}
}
