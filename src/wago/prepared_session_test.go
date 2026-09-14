package wago

import (
	"errors"
	"testing"
	"time"
)

func TestPreparedSessionDirectReservation(t *testing.T) {
	in, fn := narrowPreparedFixture(t)
	s, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !s.fast {
		t.Fatal("isolated direct function did not acquire fast session reservation")
	}
	for i := uint64(0); i < 10; i++ {
		got, err := s.Invoke(i)
		if err != nil || len(got) != 1 || got[0] != i+1 {
			t.Fatalf("invoke(%d) = %v, %v", i, got, err)
		}
	}

	shared := make(chan struct{})
	go func() {
		in.markNativeControlShared()
		close(shared)
	}()
	select {
	case <-shared:
		t.Fatal("resource sharing completed while a fast session was reserved")
	case <-time.After(20 * time.Millisecond):
	}
	s.Close()
	select {
	case <-shared:
	case <-time.After(time.Second):
		t.Fatal("resource sharing did not resume after session close")
	}
	s.Close()
	if _, err := s.Invoke(1); err == nil {
		t.Fatal("closed session invocation succeeded")
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
	if s.fast {
		t.Fatal("shared instance acquired fast session reservation")
	}
	got, err := s.Invoke(41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("general session invoke = %v, %v", got, err)
	}
}

func TestPreparedSessionNativeCloseReleasesGlobalLease(t *testing.T) {
	for range 2 {
		in, fn := narrowPreparedFixture(t)
		// Force the reservation-held native path rather than the isolated direct
		// path. A previous bug classified native-direct sessions as isolated at
		// Close and leaked nativeExecutionMu across successive instances.
		fn.directIntFast = false
		fn.directIsolated = false
		s, err := fn.OpenSession()
		if err != nil {
			t.Fatal(err)
		}
		if !s.native {
			t.Fatal("prepared function did not acquire native session")
		}
		if got, err := s.Invoke1(41); err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("native session invoke = %v, %v", got, err)
		}
		s.Close()
		in.Close()
	}
}

func TestPreparedSessionTypedHostTrapAndNestedEntry(t *testing.T) {
	otherCompiled := MustCompile(benchAddOneModule())
	defer otherCompiled.Close()
	other, err := Instantiate(otherCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherFn, err := other.PrepareI32ToI32("f")
	if err != nil {
		t.Fatal(err)
	}

	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			calls++
			if calls == 1 {
				panic(HostTrap{Err: errors.New("expected session host trap")})
			}
			got, callErr := otherFn.Call(value)
			if callErr != nil {
				panic(callErr)
			}
			return got
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("g")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !session.host {
		t.Fatal("typed host function did not acquire a host-capable session")
	}
	if _, err := session.Invoke1(I32(41)); err == nil || err.Error() != "expected session host trap" {
		t.Fatalf("first session call error = %v", err)
	}
	if got, err := session.Invoke1(I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("session after host trap and nested entry = %v, %v; want 42", got, err)
	}
	session.Close()
}
