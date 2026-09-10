package wago

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

func assertExpiredHostToken(t *testing.T, h instanceHostModule) {
	t.Helper()
	if h.valid() || h.Memory() != nil {
		t.Fatal("expired memory capability accepted")
	}
	if _, err := h.in.InvokeFromHost(context.Background(), h, "g", 1); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired re-entry: %v", err)
	}
	if err := h.CollectGC(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired collection: %v", err)
	}
	if _, err := h.NewExternRef(1); err == nil {
		t.Fatalf("expired externref: %v", err)
	}
	if _, ok := h.ExternRefValue(ExternRef{}); ok {
		t.Fatal("expired externref read")
	}
	if h.ReleaseExternRef(ExternRef{}) {
		t.Fatal("expired externref release")
	}
	if err := h.WithGuestStorage(func(GuestStorage) error { t.Fatal("expired storage callback ran"); return nil }); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired storage: %v", err)
	}
	if _, err := h.NewGCArrayResult(0, 1, nil); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired array result: %v", err)
	}
}

func TestRetainedHostTokenCannotGainLaterGeneration(t *testing.T) {
	c := MustCompile(benchReturningImportModule())
	defer c.Close()
	var retained instanceHostModule
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(mod HostModule, p, r []uint64) {
		calls++
		if calls == 1 {
			retained = mod.(instanceHostModule)
		} else {
			assertExpiredHostToken(t, retained)
		}
		r[0] = p[0] + 1
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 3; i++ {
		if _, err := in.Invoke("g", 1); err != nil {
			t.Fatal(err)
		}
		assertExpiredHostToken(t, retained)
	}
}

func TestHostScopeGenerationMonotonicAndExhaustion(t *testing.T) {
	var in Instance
	var scope hostCallScope
	a := scope.beginReservedWithID(&in, 1, nil)
	b := scope.beginReservedWithID(&in, 1, nil)
	if a.valid() || !b.valid() || b.generation <= a.generation {
		t.Fatal("invalid nested generation")
	}
	scope.end(b.generation, b.parentGeneration)
	c := scope.beginReservedWithID(&in, 1, nil)
	if b.valid() || c.generation <= b.generation {
		t.Fatal("generation rolled back")
	}
	scope.end(c.generation, c.parentGeneration)
	if !a.valid() {
		t.Fatal("outer callback not restored")
	}
	scope.sequence.Store(^uint64(0))
	for i := 0; i < 2; i++ {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("generation exhaustion did not fail")
				}
			}()
			scope.beginReservedWithID(&in, 1, nil)
		}()
		if !a.valid() || scope.sequence.Load() != ^uint64(0) {
			t.Fatal("exhaustion changed authority")
		}
	}
	scope.end(a.generation, a.parentGeneration)
	if a.valid() || b.valid() || c.valid() {
		t.Fatal("expired callback valid")
	}
}

func TestHostTokenSize(t *testing.T) {
	if got := unsafe.Sizeof(instanceHostModule{}); got > 64 {
		t.Fatalf("callback token = %d bytes, want <=64", got)
	}
}
