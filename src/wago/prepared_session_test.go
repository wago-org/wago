package wago

import (
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sessionImportMemoryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
}

func TestPreparedSessionDirectReservation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !s.state.fast {
		t.Fatal("isolated direct function missed fast reservation")
	}
	copy := *s
	for i := uint64(0); i < 10; i++ {
		got, err := copy.Invoke(i)
		if err != nil || len(got) != 1 || got[0] != i+1 {
			t.Fatalf("invoke(%d) = %v, %v", i, got, err)
		}
	}
	shared := make(chan struct{})
	go func() { in.markNativeControlShared(); close(shared) }()
	select {
	case <-shared:
		t.Fatal("sharing completed while fast session held")
	case <-time.After(20 * time.Millisecond):
	}
	s.Close()
	select {
	case <-shared:
	case <-time.After(time.Second):
		t.Fatal("sharing stayed blocked after close")
	}
	copy.Close()
	if _, err := copy.Invoke(1); err == nil {
		t.Fatal("closed copy invoked")
	}
}

func TestPreparedSessionGeneralReservation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	in.markNativeControlShared()
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.state.fast {
		t.Fatal("shared instance acquired fast reservation")
	}
	got, err := s.Invoke1(41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("general invoke = %v, %v", got, err)
	}
}

func TestPreparedSessionTypedHostCallback(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 { return v + 1 })
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !s.state.host {
		t.Fatal("typed callback missed reserved host path")
	}
	for i := 0; i < 10; i++ {
		got, err := s.Invoke1(I32(41))
		if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("host invoke = %v, %v", got, err)
		}
	}
	s.Close()
	if _, err := s.Invoke1(1); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed invoke error = %v", err)
	}
}

func TestPreparedSessionCallbackCloseAndReentry(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	var s *PreparedSession
	var reentryErr error
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 {
		calls++
		if calls == 1 {
			_, reentryErr = s.Invoke1(I32(v))
		} else {
			s.Close()
		}
		return v + 1
	})
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	s, err = fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := int32(40); i <= 41; i++ {
		got, err := s.Invoke1(I32(i))
		if err != nil || len(got) != 1 || AsI32(got[0]) != i+1 {
			t.Fatalf("invoke = %v, %v", got, err)
		}
	}
	if reentryErr == nil || !strings.Contains(reentryErr.Error(), "already active") {
		t.Fatalf("reentry error = %v", reentryErr)
	}
	if _, err := s.Invoke1(1); err == nil {
		t.Fatal("callback-closed session invoked")
	}
}

func TestPreparedSessionHostSharingRevokesLease(t *testing.T) {
	c := MustCompile(sessionImportMemoryModule())
	defer c.Close()
	var in *Instance
	imports := NewImports()
	imports.HostFunc("env", "f", func(v int32) int32 {
		if _, err := in.ExportedMemory("memory"); err != nil {
			panic(HostTrap{Err: err})
		}
		return v + 1
	})
	var err error
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.state.host {
		t.Fatal("missed reserved host path")
	}
	for i := 0; i < 2; i++ {
		got, err := s.Invoke1(I32(41))
		if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
			t.Fatalf("invoke after sharing = %v, %v", got, err)
		}
		if s.state.host {
			t.Fatal("retained private lease after sharing")
		}
	}
}
