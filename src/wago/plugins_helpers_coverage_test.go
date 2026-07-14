package wago

import (
	"slices"
	"testing"
)

func TestHostEnvironmentGuestArgsHelpers(t *testing.T) {
	before := GuestArgs()
	t.Cleanup(func() { SetGuestArgs(before) })
	SetGuestArgs([]string{"module.wasm", "one", "two"})
	if got := (&HostEnvironment{}).GuestArgs(); !slices.Equal(got, []string{"module.wasm", "one", "two"}) {
		t.Fatalf("HostEnvironment.GuestArgs = %v", got)
	}
}
