package plugin

import (
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestTypedServices(t *testing.T) {
	key := NewServiceKey[int]("counter")
	if key.Name() != "counter" {
		t.Fatalf("key name = %q", key.Name())
	}
	reg := &wago.Registry{}
	if err := Provide(reg, key, 42); err != nil {
		t.Fatalf("Provide: %v", err)
	}
	ref, err := Require(reg, key)
	if err != nil {
		t.Fatalf("Require: %v", err)
	}
	if _, err := ref.Get(); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("Get before activation error = %v", err)
	}
	if _, err := (*Ref[int])(nil).Get(); err == nil || !strings.Contains(err.Error(), "nil typed") {
		t.Fatalf("nil Get error = %v", err)
	}
	missing, err := Require(reg, NewServiceKey[int]("missing"))
	if err != nil {
		t.Fatalf("Require missing service: %v", err)
	}
	if _, err := missing.Get(); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("missing Get error = %v", err)
	}
	if _, err := (&Ref[int]{}).Get(); err == nil || !strings.Contains(err.Error(), "nil typed") {
		t.Fatalf("empty Get error = %v", err)
	}
	if err := Provide[int](nil, key, 1); err == nil {
		t.Fatal("Provide accepted nil registry")
	}
	if _, err := Require[int](nil, key); err == nil {
		t.Fatal("Require accepted nil registry")
	}
}
